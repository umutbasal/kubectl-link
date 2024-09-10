package main

import (
	"errors"
	"net"
	"net/netip"
	"os/exec"
	"sync"
	"time"

	"github.com/docker/go-units"
	"github.com/google/shlex"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/option"
	"github.com/xjasonlyu/tun2socks/v2/dialer"
	"github.com/xjasonlyu/tun2socks/v2/log"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"
)

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
)

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

	// if _defaultProxy, err = parseProxy("socks5://localhost:9050"); err != nil {
	// 	return
	// }
	_defaultProxy = NewDirect() // TODO: MAKE THIS WORK
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
