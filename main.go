package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/go-units"
	"github.com/google/shlex"
	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/tun"
	"github.com/xjasonlyu/tun2socks/v2/core/option"
	"github.com/xjasonlyu/tun2socks/v2/dialer"
	"github.com/xjasonlyu/tun2socks/v2/log"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"
	"go.uber.org/automaxprocs/maxprocs"
	"gopkg.in/yaml.v3"

	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// Key holds the configuration parameters.
type Key struct {
	MTU                      int           `yaml:"mtu"`
	Mark                     int           `yaml:"fwmark"`
	Device                   string        `yaml:"device"`
	LogLevel                 string        `yaml:"loglevel"`
	Interface                string        `yaml:"interface"`
	TCPModerateReceiveBuffer bool          `yaml:"tcp-moderate-receive-buffer"`
	TCPSendBufferSize        string        `yaml:"tcp-send-buffer-size"`
	TCPReceiveBufferSize     string        `yaml:"tcp-receive-buffer-size"`
	TUNPreUp                 string        `yaml:"tun-pre-up"`
	TUNPostUp                string        `yaml:"tun-post-up"`
	UDPTimeout               time.Duration `yaml:"udp-timeout"`
}

var (
	_engineMu sync.Mutex

	// _defaultKey holds the default key for the engine.
	_defaultKey *Key

	// _defaultProxy holds the default proxy for the engine.
	_defaultProxy proxy.Proxy

	// _defaultDevice holds the default device for the engine.
	_defaultDevice device.Device

	// _defaultStack holds the default stack for the engine.
	_defaultStack *stack.Stack

	// Configuration flags.
	key = new(Key)

	configFile  string
	versionFlag bool
)

func init() {
	flag.StringVar(&configFile, "config", "", "YAML format configuration file")
	flag.StringVar(&key.Device, "device", "", "Use this device [driver://]name")
	flag.StringVar(&key.Interface, "interface", "", "Use network INTERFACE (Linux/MacOS only)")
	flag.StringVar(&key.LogLevel, "loglevel", "info", "Log level [debug|info|warn|error|silent]")
	flag.BoolVar(&versionFlag, "version", false, "Show version and then quit")
	flag.Parse()
}

func main() {
	maxprocs.Set(maxprocs.Logger(func(string, ...any) {}))

	if versionFlag {
		os.Exit(0)
	}

	if configFile != "" {
		data, err := os.ReadFile(configFile)
		if err != nil {
			log.Fatalf("Failed to read config file '%s': %v", configFile, err)
		}
		if err = yaml.Unmarshal(data, key); err != nil {
			log.Fatalf("Failed to unmarshal config file '%s': %v", configFile, err)
		}
	}

	Insert(key)

	Start()
	defer Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}

// Start starts the TUN/TAP engine.
func Start() {
	_engineMu.Lock()
	defer _engineMu.Unlock()

	if _defaultKey == nil {
		log.Fatalf("[ENGINE] failed to start: %v", errors.New("empty key"))
	}

	for _, f := range []func(*Key) error{
		general,
		netstack,
	} {
		if err := f(_defaultKey); err != nil {
			log.Fatalf("[ENGINE] failed to start: %v", err)
		}
	}
}

// Stop stops the TUN/TAP engine.
func Stop() {
	_engineMu.Lock()
	defer _engineMu.Unlock()

	if _defaultDevice != nil {
		_defaultDevice.Close()
	}
	if _defaultStack != nil {
		_defaultStack.Close()
		_defaultStack.Wait()
	}
}

// Insert inserts a new key into the engine.
func Insert(k *Key) {
	_engineMu.Lock()
	_defaultKey = k
	_engineMu.Unlock()
}

func execCommand(cmd string) error {
	parts, err := shlex.Split(cmd)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return errors.New("empty command")
	}
	_, err = exec.Command(parts[0], parts[1:]...).Output()
	return err
}

func general(k *Key) error {
	level, err := log.ParseLevel(k.LogLevel)
	if err != nil {
		return err
	}
	log.SetLogger(log.Must(log.NewLeveled(level)))

	if k.Interface != "" {
		iface, err := net.InterfaceByName(k.Interface)
		if err != nil {
			return err
		}
		dialer.DefaultInterfaceName.Store(iface.Name)
		dialer.DefaultInterfaceIndex.Store(int32(iface.Index))
		log.Infof("[DIALER] bind to interface: %s", k.Interface)
	}

	if k.Mark != 0 {
		dialer.DefaultRoutingMark.Store(int32(k.Mark))
		log.Infof("[DIALER] set fwmark: %#x", k.Mark)
	}

	if k.UDPTimeout > 0 {
		if k.UDPTimeout < time.Second {
			return errors.New("invalid udp timeout value")
		}
		tunnel.T().SetUDPTimeout(k.UDPTimeout)
	}
	return nil
}

func netstack(k *Key) (err error) {
	log.Infof("[NETSTACK] starting...")
	if k.Device == "" {
		return errors.New("empty device")
	}

	if k.TUNPreUp != "" {
		log.Infof("[TUN] pre-execute command: `%s`", k.TUNPreUp)
		if preUpErr := execCommand(k.TUNPreUp); preUpErr != nil {
			log.Errorf("[TUN] failed to pre-execute: %s: %v", k.TUNPreUp, preUpErr)
		}
	}

	defer func() {
		if k.TUNPostUp == "" || err != nil {
			return
		}
		log.Infof("[TUN] post-execute command: `%s`", k.TUNPostUp)
		if postUpErr := execCommand(k.TUNPostUp); postUpErr != nil {
			log.Errorf("[TUN] failed to post-execute: %s: %v", k.TUNPostUp, postUpErr)
		}
	}()

	_defaultProxy = NewDirect() // Use the Direct proxy
	tunnel.T().SetDialer(_defaultProxy)

	if _defaultDevice, err = parseDevice(k.Device, uint32(k.MTU)); err != nil {
		return
	}

	var multicastGroups []netip.Addr

	var opts []option.Option
	if k.TCPModerateReceiveBuffer {
		opts = append(opts, option.WithTCPModerateReceiveBuffer(true))
	}

	if k.TCPSendBufferSize != "" {
		size, err := units.RAMInBytes(k.TCPSendBufferSize)
		if err != nil {
			return err
		}
		opts = append(opts, option.WithTCPSendBufferSize(int(size)))
	}

	if k.TCPReceiveBufferSize != "" {
		size, err := units.RAMInBytes(k.TCPReceiveBufferSize)
		if err != nil {
			return err
		}
		opts = append(opts, option.WithTCPReceiveBufferSize(int(size)))
	}

	if _defaultStack, err = core.CreateStack(&core.Config{
		LinkEndpoint:     _defaultDevice,
		TransportHandler: tunnel.T(),
		MulticastGroups:  multicastGroups,
		Options:          opts,
	}); err != nil {
		return
	}

	log.Infof(
		"[STACK] %s://%s <-> %s://%s",
		_defaultDevice.Type(), _defaultDevice.Name(),
		_defaultProxy.Proto(), _defaultProxy.Addr(),
	)
	return nil
}

// parseDevice parses the device string and returns a device.Device.
func parseDevice(s string, mtu uint32) (device.Device, error) {
	if !strings.Contains(s, "://") {
		s = fmt.Sprintf("%s://%s", tun.Driver /* default driver */, s)
	}

	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}

	driver := strings.ToLower(u.Scheme)

	switch driver {
	case tun.Driver:
		return parseTUN(u, mtu)
	default:
		return nil, fmt.Errorf("unsupported driver: %s", driver)
	}
}

// parseTUN parses the TUN device URL and returns a device.Device.
func parseTUN(u *url.URL, mtu uint32) (device.Device, error) {
	return tun.Open(u.Host, mtu)
}
