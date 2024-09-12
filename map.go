package main

import (
	"net"
	"strconv"
	"strings"
	"sync"
)

type fromAddr string

func (f fromAddr) parse() (proto, addr string) {
	from := string(f)

	// Split the string by "://"
	parts := strings.Split(from, "://")
	if len(parts) != 2 {
		return "", ""
	}

	if parts[0] == "" {
		return "", ""
	}

	// Return the first and second parts
	return parts[0], parts[1]
}

func (f fromAddr) build(proto, addr string) string {
	return proto + "://" + addr
}

type used map[string]bool

func (u used) add(s string) {
	u[s] = true
}

func (u used) has(s string) bool {
	return u[s]
}

type fwdMap struct {
	mu    sync.RWMutex
	data  map[fromAddr]net.Addr
	ports used
}

func newFwdMap() *fwdMap {
	return &fwdMap{
		data:  make(map[fromAddr]net.Addr),
		ports: make(used),
	}
}

func (m *fwdMap) add(from fromAddr, to net.Addr) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[from] = to
}

func (m *fwdMap) get(from fromAddr) (net.Addr, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	to, exists := m.data[from]
	return to, exists
}

func (m *fwdMap) addPort(port string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ports.add(port)
}

func (m *fwdMap) hasPort(port string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ports.has(port)
}

func (m *fwdMap) findFreePort() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	for port := 30000; port < 40000; port++ {
		portStr := strconv.Itoa(port)

		if _, exists := m.ports[portStr]; exists {
			continue
		}

		n, err := net.Listen("tcp", "localhost:"+portStr)
		if err == nil {
			_ = n.Close()
			m.ports.add(portStr)
			return portStr
		}
	}

	return ""
}
