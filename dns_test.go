package main

import (
	"testing"
)

func TestParseZone(t *testing.T) {
	tests := []struct {
		full     string
		expected string
	}{
		{"_port._protocol.service.namespace.pod.zone", "zone"},
		{"endpoint.service.namespace.pod.zone", "zone"},
		{"service.namespace.pod.zone", "zone"},
		{"_port._protocol.service.namespace.svc.zone", "zone"},
		{"endpoint.service.namespace.svc.zone.a.a.a.a.a.a", "zone.a.a.a.a.a.a"},
		{"service.namespace.svc.zone", "zone"},
		{"asdasd", "cluster.local"},
	}

	for _, test := range tests {
		result := parseZone(test.full)
		if result != test.expected {
			t.Errorf("parseZone(%q) = %q; want %q", test.full, result, test.expected)
		}
	}
}
func TestSplit(t *testing.T) {
	tests := []struct {
		name     string
		zone     string
		expected [6]string
	}{
		{"_port._protocol.service.namespace.pod.zone", "zone", [6]string{"pod", "_port", "_protocol", "service", "namespace", ""}},
		{"endpoint.service.namespace.pod.zone", "zone", [6]string{"pod", "", "", "service", "namespace", "endpoint"}},
		{"service.namespace.pod.zone", "zone", [6]string{"pod", "", "", "service", "namespace", ""}},
		{"_port._protocol.service.namespace.svc.zone", "zone", [6]string{"svc", "_port", "_protocol", "service", "namespace", ""}},
		{"endpoint.service.namespace.svc.zone", "zone", [6]string{"svc", "", "", "service", "namespace", "endpoint"}},
		{"service.namespace.svc.zone", "zone", [6]string{"svc", "", "", "service", "namespace", ""}},
		{"invalid.name", "zone", [6]string{"", "", "", "", "", ""}},
	}

	for _, test := range tests {
		_type, port, protocol, service, namespace, endpoint := split(test.name, test.zone)
		result := [6]string{_type, port, protocol, service, namespace, endpoint}
		if result != test.expected {
			t.Errorf("split(%q, %q) = %q; want %q", test.name, test.zone, result, test.expected)
		}
	}
}
