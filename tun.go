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

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/tun"
	"github.com/xjasonlyu/tun2socks/v2/core/option"
	"github.com/xjasonlyu/tun2socks/v2/dialer"
	"github.com/xjasonlyu/tun2socks/v2/log"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"
)

func configure(k *Key) error {
	level, err := log.ParseLevel(k.Tun2SocksLogLevel)
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
	return nil
}

func bootNetstack(k *Key) (err error) {
	log.Infof("[NETSTACK] starting...")
	if k.Device == "" {
		return errors.New("empty device")
	}

	_defaultProxy = NewDirect() // Use the Direct proxy
	tunnel.T().SetDialer(_defaultProxy)

	if _defaultDevice, err = parseDevice(k.Device, uint32(k.MTU)); err != nil {
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

// Start starts the TUN/TAP engine.
func Start() {
	_engineMu.Lock()
	defer _engineMu.Unlock()

	if _defaultKey == nil {
		log.Fatalf("[ENGINE] failed to start: %v", errors.New("empty key"))
	}

	for _, f := range []func(*Key) error{
		configure,
		bootNetstack,
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
