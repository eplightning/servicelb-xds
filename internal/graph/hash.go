package graph

import corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

type constHash struct{}

func (h constHash) ID(node *corev3.Node) string {
	return "node"
}
