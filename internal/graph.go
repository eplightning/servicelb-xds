package internal

import (
	"context"
	"fmt"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tcp_proxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	udp_proxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/udp_proxy/v3"
	proxy_protocolv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	raw_bufferv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	cachetypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/net"
)

type ServicePort struct {
	Port     int32
	Protocol net.Protocol
}

type ServicePortData struct {
	Endpoints        []*endpointv3.LocalityLbEndpoints
	UseProxyProtocol bool
}

type ServiceData struct {
	Ports map[ServicePort]ServicePortData
}

type ServiceGraph struct {
	sc       cache.SnapshotCache
	services map[types.NamespacedName]*ServiceData
	version  int64
}

type constHash struct{}

func (h constHash) ID(node *corev3.Node) string {
	return "node"
}

func (port ServicePort) String() string {
	return fmt.Sprintf("%v %v", port.Protocol, port.Port)
}

func NewServiceData() *ServiceData {
	return &ServiceData{
		Ports: make(map[ServicePort]ServicePortData),
	}
}

func NewServiceGraph() *ServiceGraph {
	return &ServiceGraph{
		sc:       cache.NewSnapshotCache(false, constHash{}, nil),
		services: make(map[types.NamespacedName]*ServiceData),
	}
}

func (g *ServiceGraph) GetCache() cache.SnapshotCache {
	return g.sc
}

func (g *ServiceGraph) RemoveService(name types.NamespacedName) {
	if _, ok := g.services[name]; ok {
		delete(g.services, name)
		g.newSnapshot()
	}
}

func (g *ServiceGraph) UpdateService(name types.NamespacedName, data *ServiceData) {
	g.services[name] = data
	g.newSnapshot()
}

func (g *ServiceGraph) Conflicts(name types.NamespacedName, port ServicePort) bool {
	for svc, data := range g.services {
		if name.Name == svc.Name && name.Namespace == svc.Namespace {
			continue
		}

		for svcPort, _ := range data.Ports {
			if svcPort.Port == port.Port && svcPort.Protocol == port.Protocol {
				return true
			}
		}
	}

	return false
}

func (g *ServiceGraph) newSnapshot() {
	g.version++

	clusters := g.makeClusters()
	listeners := g.makeListeners()
	clustersRaw := make([]cachetypes.Resource, len(clusters))
	listenersRaw := make([]cachetypes.Resource, len(listeners))

	for res := range clusters {
		clustersRaw[res] = clusters[res]
	}
	for res := range listeners {
		listenersRaw[res] = listeners[res]
	}

	snapshot, err := cache.NewSnapshot(fmt.Sprintf("%v", g.version),
		map[resource.Type][]cachetypes.Resource{
			resource.ClusterType:  clustersRaw,
			resource.ListenerType: listenersRaw,
		},
	)
	if err != nil {
		// TODO: proper handling
		panic("cannot create snapshot")
	}

	err = g.sc.SetSnapshot(context.Background(), "node", snapshot)
	if err != nil {
		// TODO: proper handling
	}
}

func (g *ServiceGraph) makeListeners() []*listenerv3.Listener {
	var listeners []*listenerv3.Listener

	for svcName, svcData := range g.services {
		for svcPort := range svcData.Ports {
			listenerName := fmt.Sprintf("frontend_%v_%v", svcPort.Protocol, svcPort.Port)
			clusterName := g.clusterName(svcName, svcPort)

			protocol := corev3.SocketAddress_TCP
			if svcPort.Protocol == net.UDP {
				protocol = corev3.SocketAddress_UDP
			}

			listener := &listenerv3.Listener{
				Name: listenerName,
				Address: &corev3.Address{
					Address: &corev3.Address_SocketAddress{
						SocketAddress: &corev3.SocketAddress{
							Address: "::",
							PortSpecifier: &corev3.SocketAddress_PortValue{
								PortValue: uint32(svcPort.Port),
							},
							Protocol: protocol,
						},
					},
				},
			}

			if svcPort.Protocol == net.TCP {
				listener.FilterChains = []*listenerv3.FilterChain{
					{
						Filters: []*listenerv3.Filter{
							{
								Name: "envoy.tcp_proxy",
								ConfigType: &listenerv3.Filter_TypedConfig{
									TypedConfig: MustMarshalAny(tcpProxyConfig(listenerName, clusterName)),
								},
							},
						},
					},
				}

				listener.SocketOptions = tcpSocketOptions()
			} else {
				listener.ListenerFilters = []*listenerv3.ListenerFilter{
					{
						Name: "envoy.filters.udp_listener.udp_proxy",
						ConfigType: &listenerv3.ListenerFilter_TypedConfig{
							TypedConfig: MustMarshalAny(udpProxyConfig(listenerName, clusterName)),
						},
					},
				}
			}

			listeners = append(listeners, listener)
		}
	}

	return listeners
}

func (g *ServiceGraph) makeClusters() []*clusterv3.Cluster {
	var clusters []*clusterv3.Cluster

	for svcName, svcData := range g.services {
		for svcPort, svcPortData := range svcData.Ports {
			clusterName := g.clusterName(svcName, svcPort)

			cluster := &clusterv3.Cluster{
				Name: clusterName,
				ClusterDiscoveryType: &clusterv3.Cluster_Type{
					Type: clusterv3.Cluster_STATIC,
				},
				LoadAssignment: g.makeEndpoint(clusterName, svcPortData),
			}

			if svcPortData.UseProxyProtocol {
				rawBuffer := &raw_bufferv3.RawBuffer{}

				proxyProtocol := &proxy_protocolv3.ProxyProtocolUpstreamTransport{
					Config: &corev3.ProxyProtocolConfig{
						Version: corev3.ProxyProtocolConfig_V2,
					},
					TransportSocket: &corev3.TransportSocket{
						Name: "envoy.transport_sockets.raw_buffer",
						ConfigType: &corev3.TransportSocket_TypedConfig{
							TypedConfig: MustMarshalAny(rawBuffer),
						},
					},
				}

				cluster.TransportSocket = &corev3.TransportSocket{
					Name: "envoy.transport_sockets.upstream_proxy_protocol",
					ConfigType: &corev3.TransportSocket_TypedConfig{
						TypedConfig: MustMarshalAny(proxyProtocol),
					},
				}
			}

			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

func (g *ServiceGraph) makeEndpoint(clusterName string, svcPortData ServicePortData) *endpointv3.ClusterLoadAssignment {
	return &endpointv3.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints:   svcPortData.Endpoints,
	}
}

func (g *ServiceGraph) clusterName(svcName types.NamespacedName, svcPort ServicePort) string {
	return fmt.Sprintf("%v_%v_%v_%v", svcName.Namespace, svcName.Name, svcPort.Protocol, svcPort.Port)
}

func tcpSocketOptions() []*corev3.SocketOption {
	// https://github.com/projectcontour/contour/blob/4a22d0c629727e67253348809eb475f1d8346dbc/internal/envoy/v3/socket_options.go#L29
	return []*corev3.SocketOption{
		{
			Description: "Enable TCP keep-alive",
			Level:       1,
			Name:        9,
			Value: &corev3.SocketOption_IntValue{
				IntValue: 1,
			},
			State: corev3.SocketOption_STATE_LISTENING,
		},
		{
			Description: "TCP keep-alive initial idle time",
			Level:       6,
			Name:        4,
			Value: &corev3.SocketOption_IntValue{
				IntValue: 45,
			},
			State: corev3.SocketOption_STATE_LISTENING,
		},
		{
			Description: "TCP keep-alive time between probes",
			Level:       6,
			Name:        5,
			Value: &corev3.SocketOption_IntValue{
				IntValue: 5,
			},
			State: corev3.SocketOption_STATE_LISTENING,
		},
		{
			Description: "TCP keep-alive probe count",
			Level:       6,
			Name:        6,
			Value: &corev3.SocketOption_IntValue{
				IntValue: 9,
			},
			State: corev3.SocketOption_STATE_LISTENING,
		},
	}
}

func tcpProxyConfig(statPrefix, clusterName string) *tcp_proxyv3.TcpProxy {
	return &tcp_proxyv3.TcpProxy{
		StatPrefix: statPrefix,
		ClusterSpecifier: &tcp_proxyv3.TcpProxy_Cluster{
			Cluster: clusterName,
		},
	}
}

func udpProxyConfig(statPrefix, clusterName string) *udp_proxyv3.UdpProxyConfig {
	return &udp_proxyv3.UdpProxyConfig{
		StatPrefix: statPrefix,
		RouteSpecifier: &udp_proxyv3.UdpProxyConfig_Cluster{
			Cluster: clusterName,
		},
	}
}
