package envoyhelper

import (
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	rbacv3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/utils/net"
	"net/netip"
	"time"
)

func DurationToProto(d *time.Duration) *durationpb.Duration {
	if d != nil {
		return durationpb.New(*d)
	}

	return nil
}

func MustMarshalAny(pb proto.Message) *anypb.Any {
	a, err := anypb.New(pb)
	if err != nil {
		panic(err.Error())
	}

	return a
}

func lbEndpoint(addr *corev3.Address) *endpointv3.LbEndpoint {
	return &endpointv3.LbEndpoint{
		HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
			Endpoint: &endpointv3.Endpoint{
				Address: addr,
			},
		},
	}
}

func LocalityLBEndpoints(addrs ...*corev3.Address) []*endpointv3.LocalityLbEndpoints {
	lbe := make([]*endpointv3.LbEndpoint, 0, len(addrs))
	for _, addr := range addrs {
		lbe = append(lbe, lbEndpoint(addr))
	}
	return []*endpointv3.LocalityLbEndpoints{{
		LbEndpoints: lbe,
	}}
}

func AnyRBACPolicy(ips []netip.Prefix) *rbacv3.Policy {
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

func IPAddress(ip string, port uint32, protocol corev3.SocketAddress_Protocol) *corev3.Address {
	return &corev3.Address{
		Address: &corev3.Address_SocketAddress{
			SocketAddress: &corev3.SocketAddress{
				Address: ip,
				PortSpecifier: &corev3.SocketAddress_PortValue{
					PortValue: port,
				},
				Protocol: protocol,
			},
		},
	}
}

func TransportSocket(name string, typedConfig proto.Message) *corev3.TransportSocket {
	return &corev3.TransportSocket{
		Name: name,
		ConfigType: &corev3.TransportSocket_TypedConfig{
			TypedConfig: MustMarshalAny(typedConfig),
		},
	}
}

func SocketAddrProtocol(proto net.Protocol) corev3.SocketAddress_Protocol {
	if proto == net.UDP {
		return corev3.SocketAddress_UDP
	}

	return corev3.SocketAddress_TCP
}

func EDSClusterConfig(xdsCluster string) *clusterv3.Cluster_EdsClusterConfig {
	return &clusterv3.Cluster_EdsClusterConfig{
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
									ClusterName: xdsCluster,
								},
							},
						},
					},
				},
			},
		},
	}
}
