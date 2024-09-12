package main

import (
	"net"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input         fromAddr
		expectedProto string
		expectedAddr  string
	}{
		{fromAddr("tcp://localhost:1234"), "tcp", "localhost:1234"},
		{fromAddr("tcp://localhost"), "tcp", "localhost"},
		{fromAddr("tcp://"), "tcp", ""},
		{fromAddr("tcp"), "", ""},
	}

	for _, test := range tests {
		proto, addr := test.input.parse()
		if proto != test.expectedProto || addr != test.expectedAddr {
			t.Errorf("parse(%q) = %q, %q; want %q, %q", test.input, proto, addr, test.expectedProto, test.expectedAddr)
		}
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		input         fromAddr
		proto         string
		addr          string
		expectedValue string
	}{
		{fromAddr(""), "tcp", "", "tcp://"},
		{fromAddr(""), "tcp", "localhost", "tcp://localhost"},
		{fromAddr(""), "tcp", "localhost:1234", "tcp://localhost:1234"},
	}

	for _, test := range tests {
		value := test.input.build(test.proto, test.addr)
		if value != test.expectedValue {
			t.Errorf("build(%q, %q) = %q; want %q", test.proto, test.addr, value, test.expectedValue)
		}
	}
}

func TestAdd(t *testing.T) {
	m := newFwdMap()
	to := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
	m.add(fromAddr("tcp://localhost:1234"), to)

	got, ok := m.get(fromAddr("tcp://localhost:1234"))
	if !ok {
		t.Errorf("add() = %v; want %v", got, to)
	}

	if got.String() != to.String() {
		t.Errorf("add() = %v; want %v", got, to)
	}
}
