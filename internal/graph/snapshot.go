package graph

import (
	"fmt"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	proxy_protocolv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/proxy_protocol/v3"
	raw_bufferv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/raw_buffer/v3"
	cachetypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/eplightning/xds-servicelb/internal"
	"github.com/eplightning/xds-servicelb/internal/graph/envoyhelper"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/net"
	"strings"
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
			protocol := envoyhelper.SocketAddrProtocol(svcPort.Protocol)
			port := uint32(svcPort.Port)

			listener := &listenerv3.Listener{
				Name:    listenerName,
				Address: envoyhelper.IPAddress("::", port, protocol),
				AdditionalAddresses: []*listenerv3.AdditionalAddress{
					{
						Address:       envoyhelper.IPAddress("0.0.0.0", port, protocol),
						SocketOptions: &corev3.SocketOptionsOverride{},
					},
				},
			}

			var filters []*listenerv3.Filter

			if len(svcPortData.AllowedIPRanges) > 0 {
				filters = append(filters, envoyhelper.NetworkFilter(
					envoyhelper.NetworkFilter_RBAC,
					envoyhelper.IPFilterConfig("rbac_"+listenerName, svcPortData.AllowedIPRanges),
				))
			}

			if svcPort.Protocol == net.TCP {
				filters = append(filters, envoyhelper.NetworkFilter(
					envoyhelper.NetworkFilter_TCPProxy,
					envoyhelper.TCPProxyConfig(listenerName, clusterName, svcPortData.IdleTimeout),
				))

				listener.SocketOptions = tcpKeepAliveSocketOptions()
				listener.AdditionalAddresses[0].SocketOptions.SocketOptions = tcpKeepAliveSocketOptions()
			} else {
				listener.ListenerFilters = []*listenerv3.ListenerFilter{
					envoyhelper.ListenerFilter(
						envoyhelper.ListenerFilter_UDPProxy,
						envoyhelper.UDPProxyConfig(listenerName, clusterName, svcPortData.IdleTimeout),
					),
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
				EdsClusterConfig: envoyhelper.EDSClusterConfig(config.XDSClusterName),
			}

			if svcPortData.UseProxyProtocol {
				cluster.TransportSocket = envoyhelper.TransportSocket(
					envoyhelper.TransportSocket_UpstreamProxyProtocol,
					&proxy_protocolv3.ProxyProtocolUpstreamTransport{
						Config: &corev3.ProxyProtocolConfig{
							Version: corev3.ProxyProtocolConfig_V2,
						},
						TransportSocket: envoyhelper.TransportSocket(envoyhelper.TransportSocket_RawBuffer, &raw_bufferv3.RawBuffer{}),
					},
				)
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
		addresses = append(addresses, envoyhelper.IPAddress(
			ep.AddrPort.Addr().String(),
			uint32(ep.AddrPort.Port()),
			envoyhelper.SocketAddrProtocol(ep.Protocol),
		))
	}

	return envoyhelper.LocalityLBEndpoints(addresses...)
}

func buildClusterName(svcName types.NamespacedName, svcPort ServicePort) string {
	return fmt.Sprintf("%v_%v_%v_%v", svcName.Namespace, svcName.Name, strings.ToLower(string(svcPort.Protocol)), svcPort.Port)
}

func buildListenerName(svcPort ServicePort) string {
	return fmt.Sprintf("frontend_%v_%v", strings.ToLower(string(svcPort.Protocol)), svcPort.Port)
}

func tcpKeepAliveSocketOptions() []*corev3.SocketOption {
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
