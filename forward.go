package main

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/dialer"
	M "github.com/xjasonlyu/tun2socks/v2/metadata"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	"github.com/xjasonlyu/tun2socks/v2/proxy/proto"
)

var _ proxy.Proxy = (*Direct)(nil)

type Direct struct {
	*Base
}

func NewDirect() *Direct {
	return &Direct{
		Base: &Base{
			addr:  "direct",
			proto: proto.Direct,
		},
	}
}

func (d *Direct) DialContext(ctx context.Context, metadata *M.Metadata) (net.Conn, error) {
	log.Printf("[Direct] DialContext: %s", metadata.DestinationAddress())
	c, err := dialer.DialContext(ctx, "tcp", "localhost:8000")
	if err != nil {
		return nil, err
	}
	setKeepAlive(c)
	return c, nil
}

func (d *Direct) DialUDP(*M.Metadata) (net.PacketConn, error) {
	log.Printf("[Direct] DialUDP")
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
	log.Printf("[Direct] WriteTo: %s", addr.String())
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
			log.Printf("[Direct] failed to set keepalive: %v", err)
		}
		err = tcp.SetKeepAlivePeriod(tcpKeepAlivePeriod)
		if err != nil {
			log.Printf("[Direct] failed to set keepalive period: %v", err)
		}

	}
}

// safeConnClose closes tcp connection safely.
func safeConnClose(c net.Conn, err error) {
	if c != nil && err != nil {
		_ = c.Close()
	}
}
