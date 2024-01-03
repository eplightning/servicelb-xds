package graph

import (
	"fmt"
	"k8s.io/utils/net"
	"net/netip"
	"time"
)

type ServiceEndpoint struct {
	AddrPort netip.AddrPort
	Protocol net.Protocol
}

type ServicePort struct {
	Port     int32
	Protocol net.Protocol
}

type ServicePortData struct {
	Endpoints        []ServiceEndpoint
	UseProxyProtocol bool
	IdleTimeout      *time.Duration
	AllowedIPRanges  []netip.Prefix
}

type ServiceData struct {
	Ports map[ServicePort]ServicePortData
}

func (port ServicePort) String() string {
	return fmt.Sprintf("%v:%v", port.Protocol, port.Port)
}

func NewServiceData() *ServiceData {
	return &ServiceData{
		Ports: make(map[ServicePort]ServicePortData),
	}
}
