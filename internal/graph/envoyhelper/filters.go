package envoyhelper

import (
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rbacv3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	networkrbacv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	tcpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	udpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/udp_proxy/v3"
	"google.golang.org/protobuf/proto"
	"net/netip"
	"time"
)

const (
	ListenerFilter_UDPProxy = "envoy.filters.udp_listener.udp_proxy"

	NetworkFilter_TCPProxy = "envoy.tcp_proxy"
	NetworkFilter_RBAC     = "envoy.filters.network.rbac"

	TransportSocket_RawBuffer             = "envoy.transport_sockets.raw_buffer"
	TransportSocket_UpstreamProxyProtocol = "envoy.transport_sockets.upstream_proxy_protocol"
)

func IPFilterConfig(statPrefix string, allowedIPs []netip.Prefix) *networkrbacv3.RBAC {
	return &networkrbacv3.RBAC{
		StatPrefix: statPrefix,
		Rules: &rbacv3.RBAC{
			Action: rbacv3.RBAC_ALLOW,
			Policies: map[string]*rbacv3.Policy{
				"source-ranges": AnyRBACPolicy(allowedIPs),
			},
		},
	}
}

func TCPProxyConfig(statPrefix, clusterName string, idleTimeout *time.Duration) *tcpproxyv3.TcpProxy {
	return &tcpproxyv3.TcpProxy{
		StatPrefix: statPrefix,
		ClusterSpecifier: &tcpproxyv3.TcpProxy_Cluster{
			Cluster: clusterName,
		},
		IdleTimeout: DurationToProto(idleTimeout),
	}
}

func UDPProxyConfig(statPrefix, clusterName string, idleTimeout *time.Duration) *udpproxyv3.UdpProxyConfig {
	return &udpproxyv3.UdpProxyConfig{
		StatPrefix: statPrefix,
		RouteSpecifier: &udpproxyv3.UdpProxyConfig_Cluster{
			Cluster: clusterName,
		},
		IdleTimeout: DurationToProto(idleTimeout),
	}
}

func NetworkFilter(name string, typedConfig proto.Message) *listenerv3.Filter {
	return &listenerv3.Filter{
		Name: name,
		ConfigType: &listenerv3.Filter_TypedConfig{
			TypedConfig: MustMarshalAny(typedConfig),
		},
	}
}

func ListenerFilter(name string, typedConfig proto.Message) *listenerv3.ListenerFilter {
	return &listenerv3.ListenerFilter{
		Name: name,
		ConfigType: &listenerv3.ListenerFilter_TypedConfig{
			TypedConfig: MustMarshalAny(typedConfig),
		},
	}
}
