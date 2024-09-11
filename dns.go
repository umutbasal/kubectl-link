package main

import (
	"fmt"
	"math/rand"
	"strings"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func findObjByIP(ip string, zone string) *v1.Object {
	name := mockrdns(ip)
	if name == "" {
		return nil
	}
	return findObjByName(name, zone)
}

func findObjByName(name, zone string) *v1.Object {
	_type, port, protocol, service, namespace, endpoint := split(name, zone)

	fmt.Println(port, protocol, service, namespace, endpoint)
	fmt.Println(_type)

	return nil
}

func split(name string, zone string) (_type, port, protocol, service, namespace, endpoint string) {
	// Strip the zone from the name if it exists
	if strings.Contains(name, zone) {
		name = strings.Split(name, zone)[0]
	}

	// Determine if the name is for a pod or a service
	switch {
	case strings.Contains(name, ".pod."):
		_type = "pod"
		name = strings.Split(name, ".pod.")[0]
	case strings.Contains(name, ".svc."):
		_type = "svc"
		name = strings.Split(name, ".svc.")[0]
	default:
		return "", "", "", "", "", ""
	}

	// Split the name by periods and process based on the number of parts
	parts := strings.Split(name, ".")
	switch len(parts) {
	case 4: // _port._protocol.service.namespace.pod|svc.zone
		port = parts[0]
		protocol = parts[1]
		service = parts[2]
		namespace = parts[3]
	case 3: // endpoint.service.namespace.pod|svc.zone
		endpoint = parts[0]
		service = parts[1]
		namespace = parts[2]
	case 2: // service.namespace.pod|svc.zone
		service = parts[0]
		namespace = parts[1]
	default:
		return "", "", "", "", "", ""
	}

	return _type, port, protocol, service, namespace, endpoint
}

func findZone(ip string) string {
	full := rdns(ip)
	return parseZone(full)
}

func parseZone(full string) string {
	if strings.Contains(full, ".pod.") {
		return strings.Split(full, ".pod.")[1]
	}
	if strings.Contains(full, ".svc.") {
		return strings.Split(full, ".svc.")[1]
	}
	return "cluster.local"
}

func rdns(ip string) string {
	// 3 Possible cases:
	// 1. _port._protocol.service.namespace.pod|svc.zone
	// 2. (endpoint): endpoint.service.namespace.pod|svc.zone
	// 3. (service): service.namespace.pod|svc.zone
	_ = ip
	return ""
}

func mockrdns(ip string) string {
	cases := []string{
		"_port._protocol.service.namespace.pod.zone",
		"endpoint.service.namespace.pod.zone",
		"service.namespace.pod.zone",
	}
	choose := cases[rand.Intn(len(cases))]
	_ = ip
	return choose
}
