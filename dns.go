package main

import (
	"context"
	"net"
	"strings"

	"github.com/miekg/dns"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

// User goes into browser and types;
// 1) 172.0.0.1
// - do rdns to get the hostname
// 2) service.namespace.svc.cluster.local
// - dns resolves ip 172.0.0.1
// - do rdns to get the hostname
// ----- And -----
// - split the hostname to get the type(pod,svc), port, protocol, endpoint, service, namespace, and zone
// - 1) if has endpoint is a pod
// - get service to find selector
// - get the pod by label app=service
// - make sure the pod ip is the ip
// - 2) if has no endpoint is a service
// - so, get the service
// - make sure the service cluster ip is the ip
// - get the pod by label app=service
// - return the pod
// ----- And -----
// - metadata plugin for coredns disabled by default
// - but with srv records we can get port
// - still, we need to get exact pod name to port forward

func findPodByIP(client kubernetes.Interface, ip string, zone string) (pod *v1.Pod, err error) {
	klog.Infof("Finding pod by IP: %s", ip)
	name, err := rdns(&net.TCPAddr{
		IP:   net.ParseIP("localhost"),
		Port: 5300,
	}, ip)
	if err != nil {
		klog.Errorf("failed to do rdns: %v", err)
		return nil, err
	}
	if name == "" {
		klog.Errorf("failed to find name")
		return nil, nil
	}

	_, _, _, service, namespace, endpoint := split(name, zone)

	svc, err := client.CoreV1().Services(namespace).Get(context.TODO(), service, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("failed to get service: %v", err)
		return nil, err
	}

	if endpoint == "" {
		if svc.Spec.ClusterIP != ip {
			klog.Errorf("service cluster ip does not match: %s != %s", svc.Spec.ClusterIP, ip)
			return nil, nil
		}
	}

	selector := []string{}
	for key, value := range svc.Spec.Selector {
		selector = append(selector, key+"="+value)
	}
	pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: strings.Join(selector, ","),
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		klog.Errorf("failed to list pods: %v", err)
		return nil, err
	}

	if endpoint != "" {
		for _, pod := range pods.Items {
			if pod.Status.PodIP == ip {
				return &pod, nil
			}
		}
	}

	return nil, nil
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
		port, protocol, service, namespace = parts[0], parts[1], parts[2], parts[3]
	case 3: // endpoint.service.namespace.pod|svc.zone
		endpoint, service, namespace = parts[0], parts[1], parts[2]
	case 2: // service.namespace.pod|svc.zone
		service, namespace = parts[0], parts[1]
	default:
		return "", "", "", "", "", ""
	}

	return _type, port, protocol, service, namespace, endpoint
}

func findZone(ip string) string {
	full, err := rdns(&net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 5300,
	}, ip)
	if err != nil {
		return "error"
	}
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

func rdns(dnsAddr net.Addr, ip string) (full string, err error) {
	// 3 Possible cases:
	// 1. _port._protocol.service.namespace.pod|svc.zone
	// 2. (endpoint): endpoint.service.namespace.pod|svc.zone
	// 3. (service): service.namespace.pod|svc.zone

	//dig SRV  +vc -p 5300 @127.0.0.1 -x 172.0.0.1)
	reverse, err := dns.ReverseAddr(ip)
	if err != nil {
		return "", err
	}

	c := new(dns.Client)
	c.Net = dnsAddr.Network() // "+vc" in dig forces TCP

	m := new(dns.Msg)
	m.SetQuestion(reverse, dns.TypePTR)

	r, _, err := c.Exchange(m, dnsAddr.String())
	if err != nil {
		return "", err
	}

	if len(r.Answer) == 0 {
		return "", nil
	}

	ans := r.Answer[0].(*dns.PTR).Ptr
	if ans[len(ans)-1] == '.' {
		ans = ans[:len(ans)-1]
	}

	return ans, nil
}
