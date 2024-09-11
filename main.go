package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"

	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"

	"k8s.io/client-go/transport/spdy"
	"k8s.io/klog"
)

var (
	linkExample = `
	# setup and run tun device
	%[1]s
`

	errNoContext = fmt.Errorf("no context is currently set, use %q to select a new one", "kubectl config use-context <context>")
)

func main() {
	flags := pflag.NewFlagSet("kubectl-link", pflag.ExitOnError)
	pflag.CommandLine = flags

	klogFlags := flag.NewFlagSet("ignored", flag.ExitOnError)
	klog.InitFlags(klogFlags)
	flags.AddGoFlagSet(klogFlags)

	configFlags := genericclioptions.NewConfigFlags(false)
	configFlags.AddFlags(flags)

	flags.Parse(os.Args[1:])

	rawConfig, err := configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		klog.Fatalf("failed to load kubeconfig: %v", err)
	}

	if rawConfig.CurrentContext == "" {
		klog.Fatalf("failed to find current context: %v", errNoContext)
	}

	klog.Infof("current context: %s", rawConfig.CurrentContext)

	clientCfg, err := configFlags.ToRESTConfig()
	if err != nil {
		klog.Fatalf("failed to create REST config: %v", err)
	}

	client := kubernetes.NewForConfigOrDie(clientCfg)

	// Find dns pods filter healthy one
	pods, err := client.CoreV1().Pods("kube-system").List(context.TODO(), metav1.ListOptions{
		LabelSelector: "k8s-app=kube-dns",
		FieldSelector: "status.phase=Running",
	})

	if err != nil {
		klog.Fatalf("failed to list pods: %v", err)
	}

	if len(pods.Items) == 0 {
		klog.Fatalf("no dns pods found")
	}

	var dnsPod *v1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		for _, container := range pod.Spec.Containers {
			if container.Ports == nil {
				continue
			}
			for _, port := range container.Ports {
				if port.ContainerPort == 53 && port.Protocol == "TCP" { // TODO: Check if portforward is able to forward by udp
					dnsPod = pod
					break
				}
			}
		}
	}

	if dnsPod == nil {
		klog.Fatalf("no running dns pods found")
	}

	// Forward port kubectl port-forward -n kube-system pod/coredns-0-a 5300:53
	err = PodPortForward(clientCfg, dnsPod, []string{"5300:53"})
	if err != nil {
		klog.Fatalf("failed to forward port: %v", err)
	}
}

func PodPortForward(clientCfg *rest.Config, pod *v1.Pod, ports []string) error {
	targetURL, err := url.Parse(clientCfg.Host)
	if err != nil {
		return fmt.Errorf("failed to parse target URL: %w", err)
	}

	targetURL.Path = path.Join(
		"/api/v1/namespaces", pod.Namespace, "pods", pod.Name, "portforward",
	)

	transport, upgrader, err := spdy.RoundTripperFor(clientCfg)
	if err != nil {
		return fmt.Errorf("failed to create round tripper: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, targetURL)

	forwarder, err := portforward.New(dialer, ports, context.Background().Done(), make(chan struct{}), os.Stdout, os.Stderr)
	if err != nil {
		return fmt.Errorf("failed to create port forwarder: %w", err)
	}

	if err = forwarder.ForwardPorts(); err != nil {
		return fmt.Errorf("failed to forward ports: %w", err)
	}

	return nil
}
