package internal

import (
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func MustMarshalAny(pb proto.Message) *anypb.Any {
	a, err := anypb.New(pb)
	if err != nil {
		panic(err.Error())
	}

	return a
}

// envoyLBEndpoint creates a new LbEndpoint.
func envoyLBEndpoint(addr *corev3.Address) *endpointv3.LbEndpoint {
	return &endpointv3.LbEndpoint{
		HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
			Endpoint: &endpointv3.Endpoint{
				Address: addr,
			},
		},
	}
}

// EnvoyEndpoints returns a slice of LocalityLbEndpoints.
// The slice contains one entry, with one LbEndpoint per
// *envoy_core_v3.Address supplied.
func EnvoyEndpoints(addrs ...*corev3.Address) []*endpointv3.LocalityLbEndpoints {
	lbendpoints := make([]*endpointv3.LbEndpoint, 0, len(addrs))
	for _, addr := range addrs {
		lbendpoints = append(lbendpoints, envoyLBEndpoint(addr))
	}
	return []*endpointv3.LocalityLbEndpoints{{
		LbEndpoints: lbendpoints,
	}}
}
