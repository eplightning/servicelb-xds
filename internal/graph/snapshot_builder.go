package graph

import (
	"fmt"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rbacv3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	network_rbacv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	tcp_proxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	udp_proxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/udp_proxy/v3"
	proxy_protocolv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	raw_bufferv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	cachetypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/eplightning/servicelb-xds/internal"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/net"
	"net/netip"
	"strings"
	"time"
)

func buildSnapshotData(config *internal.Config, data *graphData) map[resource.Type][]cachetypes.Resource {
	clusters := makeClusters(config, data)
	listeners := makeListeners(data)
	endpoints := makeLoadAssignments(data)
	clustersRaw := make([]cachetypes.Resource, len(clusters))
	listenersRaw := make([]cachetypes.Resource, len(listeners))
	endpointsRaw := make([]cachetypes.Resource, len(endpoints))

	for res := range clusters {
		clustersRaw[res] = clusters[res]
	}
	for res := range listeners {
		listenersRaw[res] = listeners[res]
	}
	for res := range endpoints {
		endpointsRaw[res] = endpoints[res]
	}

	return map[resource.Type][]cachetypes.Resource{
		resource.ClusterType:  clustersRaw,
		resource.ListenerType: listenersRaw,
		resource.EndpointType: endpointsRaw,
	}
}

func makeListeners(data *graphData) []*listenerv3.Listener {
	var listeners []*listenerv3.Listener

	for svcName, svcData := range data.services {
		for svcPort, svcPortData := range svcData.Ports {
			listenerName := buildListenerName(svcPort)
			clusterName := buildClusterName(svcName, svcPort)

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
				AdditionalAddresses: []*listenerv3.AdditionalAddress{
					{
						Address: &corev3.Address{
							Address: &corev3.Address_SocketAddress{
								SocketAddress: &corev3.SocketAddress{
									Address: "0.0.0.0",
									PortSpecifier: &corev3.SocketAddress_PortValue{
										PortValue: uint32(svcPort.Port),
									},
									Protocol: protocol,
								},
							},
						},
						SocketOptions: &corev3.SocketOptionsOverride{},
					},
				},
			}

			var filters []*listenerv3.Filter

			if len(svcPortData.AllowedIPRanges) > 0 {
				filters = append(filters, ipFilter(listenerName, svcPortData.AllowedIPRanges))
			}

			if svcPort.Protocol == net.TCP {
				filters = append(filters, &listenerv3.Filter{
					Name: "envoy.tcp_proxy",
					ConfigType: &listenerv3.Filter_TypedConfig{
						TypedConfig: MustMarshalAny(tcpProxyConfig(listenerName, clusterName, svcPortData.IdleTimeout)),
					},
				})

				listener.SocketOptions = tcpSocketOptions()
				listener.AdditionalAddresses[0].SocketOptions.SocketOptions = tcpSocketOptions()
			} else {
				listener.ListenerFilters = []*listenerv3.ListenerFilter{
					{
						Name: "envoy.filters.udp_listener.udp_proxy",
						ConfigType: &listenerv3.ListenerFilter_TypedConfig{
							TypedConfig: MustMarshalAny(udpProxyConfig(listenerName, clusterName, svcPortData.IdleTimeout)),
						},
					},
				}
			}

			if len(filters) > 0 {
				listener.FilterChains = []*listenerv3.FilterChain{
					{
						Filters: filters,
					},
				}
			}

			listeners = append(listeners, listener)
		}
	}

	return listeners
}

func makeClusters(config *internal.Config, data *graphData) []*clusterv3.Cluster {
	var clusters []*clusterv3.Cluster

	for svcName, svcData := range data.services {
		for svcPort, svcPortData := range svcData.Ports {
			clusterName := buildClusterName(svcName, svcPort)

			cluster := &clusterv3.Cluster{
				Name: clusterName,
				ClusterDiscoveryType: &clusterv3.Cluster_Type{
					Type: clusterv3.Cluster_EDS,
				},
				EdsClusterConfig: &clusterv3.Cluster_EdsClusterConfig{
					EdsConfig: &corev3.ConfigSource{
						ResourceApiVersion: corev3.ApiVersion_V3,
						ConfigSourceSpecifier: &corev3.ConfigSource_ApiConfigSource{
							ApiConfigSource: &corev3.ApiConfigSource{
								ApiType:             corev3.ApiConfigSource_GRPC,
								TransportApiVersion: corev3.ApiVersion_V3,
								GrpcServices: []*corev3.GrpcService{
									{
										TargetSpecifier: &corev3.GrpcService_EnvoyGrpc_{
											EnvoyGrpc: &corev3.GrpcService_EnvoyGrpc{
												ClusterName: config.XDSClusterName,
											},
										},
									},
								},
							},
						},
					},
				},
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

func makeLoadAssignments(data *graphData) []*endpointv3.ClusterLoadAssignment {
	var cla []*endpointv3.ClusterLoadAssignment

	for svcName, svcData := range data.services {
		for svcPort, svcPortData := range svcData.Ports {
			clusterName := buildClusterName(svcName, svcPort)

			cla = append(cla, makeLoadAssignment(clusterName, svcPortData))
		}
	}

	return cla
}

func makeLoadAssignment(clusterName string, svcPortData ServicePortData) *endpointv3.ClusterLoadAssignment {
	return &endpointv3.ClusterLoadAssignment{
		ClusterName: clusterName,
		Endpoints:   makeEndpoints(svcPortData.Endpoints),
	}
}

func makeEndpoints(endpoints []ServiceEndpoint) []*endpointv3.LocalityLbEndpoints {
	var addresses []*corev3.Address

	for _, ep := range endpoints {
		protocol := corev3.SocketAddress_TCP
		if ep.Protocol == net.UDP {
			protocol = corev3.SocketAddress_UDP
		}

		addresses = append(addresses, &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Address: ep.AddrPort.Addr().String(),
					PortSpecifier: &corev3.SocketAddress_PortValue{
						PortValue: uint32(ep.AddrPort.Port()),
					},
					Protocol: protocol,
				},
			},
		})
	}

	return envoyEndpoints(addresses...)
}

func buildClusterName(svcName types.NamespacedName, svcPort ServicePort) string {
	return fmt.Sprintf("%v_%v_%v_%v", svcName.Namespace, svcName.Name, strings.ToLower(string(svcPort.Protocol)), svcPort.Port)
}

func buildListenerName(svcPort ServicePort) string {
	return fmt.Sprintf("frontend_%v_%v", strings.ToLower(string(svcPort.Protocol)), svcPort.Port)
}

func durationToProto(d *time.Duration) *durationpb.Duration {
	if d != nil {
		return durationpb.New(*d)
	}

	return nil
}

func ipFilter(statPrefix string, ips []netip.Prefix) *listenerv3.Filter {
	return &listenerv3.Filter{
		Name: "envoy.filters.network.rbac",
		ConfigType: &listenerv3.Filter_TypedConfig{
			TypedConfig: MustMarshalAny(ipFilterConfig("rbac_"+statPrefix, ips)),
		},
	}
}

func ipFilterConfig(statPrefix string, ips []netip.Prefix) *network_rbacv3.RBAC {
	return &network_rbacv3.RBAC{
		StatPrefix: statPrefix,
		Rules: &rbacv3.RBAC{
			Action: rbacv3.RBAC_ALLOW,
			Policies: map[string]*rbacv3.Policy{
				"source-ranges": rbacPolicy(ips),
			},
		},
	}
}

func rbacPolicy(ips []netip.Prefix) *rbacv3.Policy {
	var principals []*rbacv3.Principal

	for _, ip := range ips {
		principals = append(principals, &rbacv3.Principal{
			Identifier: &rbacv3.Principal_RemoteIp{
				RemoteIp: &corev3.CidrRange{
					AddressPrefix: ip.Addr().String(),
					PrefixLen:     wrapperspb.UInt32(uint32(ip.Bits())),
				},
			},
		})
	}

	return &rbacv3.Policy{
		Permissions: []*rbacv3.Permission{
			{
				Rule: &rbacv3.Permission_Any{Any: true},
			},
		},
		Principals: principals,
	}
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

func tcpProxyConfig(statPrefix, clusterName string, idleTimeout *time.Duration) *tcp_proxyv3.TcpProxy {
	return &tcp_proxyv3.TcpProxy{
		StatPrefix: statPrefix,
		ClusterSpecifier: &tcp_proxyv3.TcpProxy_Cluster{
			Cluster: clusterName,
		},
		IdleTimeout: durationToProto(idleTimeout),
	}
}

func udpProxyConfig(statPrefix, clusterName string, idleTimeout *time.Duration) *udp_proxyv3.UdpProxyConfig {
	return &udp_proxyv3.UdpProxyConfig{
		StatPrefix: statPrefix,
		RouteSpecifier: &udp_proxyv3.UdpProxyConfig_Cluster{
			Cluster: clusterName,
		},
		IdleTimeout: durationToProto(idleTimeout),
	}
}
