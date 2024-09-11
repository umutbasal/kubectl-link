package main

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/dialer"
	"github.com/xjasonlyu/tun2socks/v2/log"
	M "github.com/xjasonlyu/tun2socks/v2/metadata"
	"github.com/xjasonlyu/tun2socks/v2/proxy/proto"
)

// Base is the base proxy type.
type Base struct {
	addr  string
	proto proto.Proto
}

// Addr returns the address of the proxy.
func (b *Base) Addr() string {
	return b.addr
}

// Proto returns the protocol of the proxy.
func (b *Base) Proto() proto.Proto {
	return b.proto
}

// DialContext dials a connection to the proxy.
func (b *Base) DialContext(context.Context, *M.Metadata) (net.Conn, error) {
	return nil, errors.ErrUnsupported
}

// DialUDP dials a UDP connection to the proxy.
func (b *Base) DialUDP(*M.Metadata) (net.PacketConn, error) {
	return nil, errors.ErrUnsupported
}

// Direct is a direct proxy.
type Direct struct {
	*Base
}

// NewDirect creates a new Direct proxy.
func NewDirect() *Direct {
	return &Direct{
		Base: &Base{
			addr:  "direct",
			proto: proto.Direct,
		},
	}
}

// DialContext dials a connection to the proxy.
func (d *Direct) DialContext(ctx context.Context, metadata *M.Metadata) (net.Conn, error) {
	c, err := dialer.DialContext(ctx, "tcp", "localhost:8080")
	if err != nil {
		return nil, err
	}
	setKeepAlive(c)
	return c, nil
}

// DialUDP dials a UDP connection to the proxy.
func (d *Direct) DialUDP(*M.Metadata) (net.PacketConn, error) {
	pc, err := dialer.ListenPacket("udp", "")
	if err != nil {
		return nil, err
	}
	return &directPacketConn{PacketConn: pc}, nil
}

type directPacketConn struct {
	net.PacketConn
}

func (pc *directPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		return pc.PacketConn.WriteTo(b, udpAddr)
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr.String())
	if err != nil {
		return 0, err
	}
	return pc.PacketConn.WriteTo(b, udpAddr)
}

const (
	tcpKeepAlivePeriod = 30 * time.Second
)

// setKeepAlive sets tcp keepalive option for tcp connection.
func setKeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		err := tcp.SetKeepAlive(true)

		if err != nil {
			log.Infof("[Direct] failed to set keepalive: %v", err)
		}
		err = tcp.SetKeepAlivePeriod(tcpKeepAlivePeriod)
		if err != nil {
			log.Infof("[Direct] failed to set keepalive period: %v", err)
		}

	}
}
