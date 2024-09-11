package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"sync"
	"syscall"

	"github.com/spf13/pflag"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/proxy"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
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

type Opts struct {
	Device            string `yaml:"device"`
	Tun2SocksLogLevel string `yaml:"tun2socks_log_level"`
	Interface         string `yaml:"interface"`
	DNSPod            string `yaml:"dns_pod"`
}

var (
	_engineMu      sync.Mutex
	_defaultOpt    *Opts
	_defaultProxy  proxy.Proxy
	_defaultDevice device.Device
	_defaultStack  *stack.Stack
	dnsPod         *v1.Pod
	opt            = new(Opts)
)

func pluginFlags(flags *pflag.FlagSet) {
	flags.StringVar(&opt.Device, "device", "", "Use this device [driver://]name")
	flags.StringVar(&opt.Interface, "interface", "", "Use network INTERFACE (Linux/MacOS only)")
	flags.StringVar(&opt.Tun2SocksLogLevel, "tun2socks-log-level", "info", "Log level [debug|info|warn|error|silent]")
	flags.StringVar(&opt.Tun2SocksLogLevel, "l", "info", "Log level [debug|info|warn|error|silent]")
	flags.StringVar(&opt.DNSPod, "dns-pod", "", "DNS pod name")
}

func main() {
	flags := pflag.NewFlagSet("kubectl-link", pflag.ExitOnError)
	pflag.CommandLine = flags

	pluginFlags(flags)

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

	if opt.DNSPod != "" {
		dnsPod, err = getDNSPodByName(client, "kube-system", opt.DNSPod)
		if err != nil {
			klog.Fatalf("Error: %v", err)
		}
		if dnsPod == nil {
			klog.Fatalf("Specified DNS pod not found")
		}
	} else {
		dnsPod, err = findHealthyDNSPod(client, "kube-system")
		if err != nil {
			klog.Fatalf("Error: %v", err)
		}
	}

	if dnsPod == nil {
		klog.Fatalf("no running dns pods found")
	}

	// // Forward port kubectl port-forward -n kube-system pod/coredns-0-a 5300:53
	// err = PodPortForward(clientCfg, dnsPod, []string{"5300:53"})
	// if err != nil {
	// 	klog.Fatalf("failed to forward port: %v", err)
	// }
	Insert(opt)

	Start()
	defer Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}

func hasPort(pod *v1.Pod, containerPort int32, protocol v1.Protocol) bool {
	for _, container := range pod.Spec.Containers {
		if container.Ports != nil {
			for _, port := range container.Ports {
				if port.ContainerPort == containerPort && port.Protocol == protocol {
					return true
				}
			}
		}
	}
	return false
}

func getDNSPodByName(client kubernetes.Interface, namespace, name string) (*v1.Pod, error) {
	pod, err := client.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get dns pod: %v", err)
	}
	if pod.Status.Phase != v1.PodRunning || !hasPort(pod, 53, "TCP") {
		return nil, nil
	}
	return pod, nil
}

func findHealthyDNSPod(client kubernetes.Interface, namespace string) (*v1.Pod, error) {
	pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "k8s-app=kube-dns",
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no dns pods found")
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		if hasPort(pod, 53, "TCP") {
			return pod, nil
		}
	}
	return nil, fmt.Errorf("no healthy dns pod found")
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
