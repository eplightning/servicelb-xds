package internal

import (
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"net"
)

type IngressStatusAddress struct {
	IP       net.IP
	Hostname string
}

type Config struct {
	LBClass          string
	AddressType      discoveryv1.AddressType
	UseNodeAddresses bool
	NodeAddressType  corev1.NodeAddressType
	IngressStatus    []IngressStatusAddress
}
