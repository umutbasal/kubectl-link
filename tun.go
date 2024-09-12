package main

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os/exec"
	"strings"

	"github.com/google/shlex"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/tun"
	"github.com/xjasonlyu/tun2socks/v2/core/option"
	"github.com/xjasonlyu/tun2socks/v2/dialer"
	"github.com/xjasonlyu/tun2socks/v2/log"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"
)

// Custom WriteSyncer to redirect zap logs to klog
type klogWriter struct{}

func (kw *klogWriter) Write(p []byte) (n int, err error) {
	klog.InfoDepth(1, string(p))
	return len(p), nil
}

func (kw *klogWriter) Sync() error {
	return nil
}

func configure(opt *Opts) error {
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig()), // Use console encoder
		zapcore.AddSync(&klogWriter{}),                               // Redirect output to klog
		zap.InfoLevel,                                                // Set logging level
	)

	// Create a zap logger using the custom core
	logger := zap.New(core)
	defer logger.Sync()

	log.SetLogger(logger)

	if opt.Interface != "" {
		iface, err := net.InterfaceByName(opt.Interface)
		if err != nil {
			return err
		}
		dialer.DefaultInterfaceName.Store(iface.Name)
		dialer.DefaultInterfaceIndex.Store(int32(iface.Index))
		log.Infof("[DIALER] bind to interface: %s", opt.Interface)
	}

	return nil
}

func bootNetstack(opt *Opts) (err error) {
	log.Infof("[NETSTACK] starting...")
	if opt.Device == "" {
		return errors.New("empty device")
	}

	_defaultProxy = NewDirect() // Use the Direct proxy
	tunnel.T().SetDialer(_defaultProxy)

	if _defaultDevice, err = parseDevice(opt.Device, uint32(0)); err != nil {
		return
	}

	var multicastGroups []netip.Addr

	var opts []option.Option

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

// StartTun starts the TUN/TAP engine.
func StartTun() {
	_engineMu.Lock()
	defer _engineMu.Unlock()

	if _defaultOpt == nil {
		log.Fatalf("[ENGINE] failed to start: %v", errors.New("empty Opts"))
	}

	for _, f := range []func(*Opts) error{
		configure,
		bootNetstack,
	} {
		if err := f(_defaultOpt); err != nil {
			log.Fatalf("[ENGINE] failed to start: %v", err)
		}
	}
}

// StopTun stops the TUN/TAP engine.
func StopTun() {
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

// InsertOptsTun inserts a new Opts into the engine.
func InsertOptsTun(opt *Opts) {
	_engineMu.Lock()
	_defaultOpt = opt
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
