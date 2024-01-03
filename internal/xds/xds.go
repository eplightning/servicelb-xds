package xds

import (
	"context"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/go-logr/logr"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"net"
	"time"

	clusterservice "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	endpointservice "github.com/envoyproxy/go-control-plane/envoy/service/endpoint/v3"
	listenerservice "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
)

const (
	grpcKeepaliveTime        = 30 * time.Second
	grpcKeepaliveTimeout     = 5 * time.Second
	grpcKeepaliveMinTime     = 30 * time.Second
	grpcMaxConcurrentStreams = 1000000
)

type XDSOptions struct {
	Address string
}

type XDSServer struct {
	server        xds.Server
	grpcServer    *grpc.Server
	options       XDSOptions
	snapshotCache cache.SnapshotCache
	lis           net.Listener
	l             logr.Logger
}

func NewXDSServer(logger logr.Logger, snapshotCache cache.SnapshotCache, options XDSOptions) *XDSServer {
	return &XDSServer{
		snapshotCache: snapshotCache,
		options:       options,
		l:             logger,
	}
}

func (srv *XDSServer) Start(ctx context.Context) error {
	// gRPC golang library sets a very small upper bound for the number gRPC/h2
	// streams over a single TCP connection. If a proxy multiplexes requests over
	// a single connection to the management server, then it might lead to
	// availability problems. Keepalive timeouts based on connection_keepalive parameter https://www.envoyproxy.io/docs/envoy/latest/configuration/overview/examples#dynamic
	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions,
		grpc.MaxConcurrentStreams(grpcMaxConcurrentStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    grpcKeepaliveTime,
			Timeout: grpcKeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             grpcKeepaliveMinTime,
			PermitWithoutStream: true,
		}),
	)

	srv.server = xds.NewServer(ctx, srv.snapshotCache, nil)
	srv.grpcServer = grpc.NewServer(grpcOptions...)

	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(srv.grpcServer, srv.server)
	endpointservice.RegisterEndpointDiscoveryServiceServer(srv.grpcServer, srv.server)
	clusterservice.RegisterClusterDiscoveryServiceServer(srv.grpcServer, srv.server)
	listenerservice.RegisterListenerDiscoveryServiceServer(srv.grpcServer, srv.server)

	lis, err := net.Listen("tcp", srv.options.Address)
	if err != nil {
		return err
	}

	srv.lis = lis

	go func() {
		<-ctx.Done()

		srv.l.Info("XDS server stopping")
		srv.grpcServer.GracefulStop()
	}()

	srv.l.Info("XDS server listening", "addr", srv.options.Address)

	return srv.grpcServer.Serve(srv.lis)
}
