package internal

import (
	"flag"
	corev1 "k8s.io/api/core/v1"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"strings"
)

const (
	AddressSourceNode     AddressSource = "node"
	AddressSourceEndpoint AddressSource = "endpoint"
)

type AddressSource string

type IngressStatusAddress struct {
	IP       net.IP
	Hostname string
}

type Config struct {
	MetricsAddr string
	HealthAddr  string
	XDSAddr     string

	XDSClusterName string

	EnableLeaderElection bool
	LeaderElectionID     string

	LoadBalancerClass string

	UseIPv6Endpoints bool
	IngressStatus    []IngressStatusAddress
	AddressSource    AddressSource
	NodeAddressType  corev1.NodeAddressType
}

func ParseConfig() (*Config, zap.Options) {
	var devel bool
	var ingressStatus string
	var addressSource string
	var nodeAddressType string

	config := &Config{}

	flag.BoolVar(&devel, "development", false, "Enable logger development mode")

	flag.StringVar(&config.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&config.HealthAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&config.XDSAddr, "xds-bind-address", ":50051", "The address the xDS endpoint binds to.")

	flag.StringVar(&config.XDSClusterName, "xds-cluster-name", "xds-servicelb", "XDS cluster name to use for EDS")

	flag.BoolVar(&config.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(&config.LoadBalancerClass, "load-balancer-class", "", "Load balancer class of services to manage")

	flag.BoolVar(&config.UseIPv6Endpoints, "use-ipv6-endpoints", false, "Use IPv6 endpoints instead of IPv4")
	flag.StringVar(&ingressStatus, "ingress-status", "", "Addresses (IPv4, IPv6, hostname) to use for service status, separated by commas")
	flag.StringVar(&addressSource, "address-source", "endpoint", "What to use as a source for endpoint addresses (node or endpoint)")
	flag.StringVar(&nodeAddressType, "node-address-type", "ExternalIP", "Which node address type to use for EDS")

	opts := zap.Options{
		Development: devel,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	for _, addr := range strings.Split(ingressStatus, ",") {
		addrTrim := strings.TrimSpace(addr)

		if addrTrim != "" {
			ip := net.ParseIP(addrTrim)
			if ip != nil {
				config.IngressStatus = append(config.IngressStatus, IngressStatusAddress{
					IP: ip,
				})
			} else {
				config.IngressStatus = append(config.IngressStatus, IngressStatusAddress{
					Hostname: addrTrim,
				})
			}
		}
	}

	config.AddressSource = AddressSource(addressSource)
	config.NodeAddressType = corev1.NodeAddressType(nodeAddressType)

	if config.LoadBalancerClass != "" {
		config.LeaderElectionID = "062fbd79.eplight.org/" + config.LoadBalancerClass
	} else {
		config.LeaderElectionID = "062fbd79.eplight.org"
	}

	return config, opts
}
