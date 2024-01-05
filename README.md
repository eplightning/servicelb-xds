# xds-servicelb

Implementation of Kubernetes LoadBalancer services using xDS protocol.

## Description

This controller allows you to provide load-balancing services to any Kubernetes cluster via external xDS load-balancers (such as Envoy).
Suitable for situations when MetalLB is not viable due to environment's limitations etc.

## Features
 
 - Exposes Kubernetes Type=LoadBalancer services via an xDS (LDS, CDS and EDS) endpoint
 - Returned endpoint addresses can be either pod or node IP (via nodePort)
 - Listeners are dual-stack while endpoints are single-stack (IPv4 or IPv6)
 - TCP and UDP services supported
 - Client IP preservation via PROXY protocol
 - Customizable idle timeout for downstream connections
 - IP filtering supported via loadBalancerSourceRanges field
## Installation

If you're okay with using NodePort for exposing xDS, use `default-nodeport` variant:

```
kubectl apply -f manifests/default-nodeport.yaml
```

Otherwise you can use `default` variant and will need to expose xDS service via different means:

```
kubectl apply -f manifests/default.yaml
```

## Configuration

Given the nature of this controller its very likely you will need to customize flags to fit your network environment. Here's a list of options you can override:

- `--xds-cluster-name=xds-servicelb` - xDS cluster name that will be used for CDS clusters, should match whatever you set in envoy's boostrap config
- `--load-balancer-class=class` - You can restrict the controller to only manage services with specified `loadBalancerClass` spec field
- `--use-ipv6-endpoints=true` - Use IPv6 endpoints instead of IPv4
- `--ingress-status=ip1,ip2,host` - IPs/hostnames to use for load balancer ingress status field
- `--address-source=endpoint|node` - What to use as a source for endpoint addresses (node or endpoint)
- `--node-address-type=ExternalIP` - Which node addresses to use (only applicable for address-source=node)

## Envoy bootstrap example

To connect to the xDS server from Envoy you'll need a bootstrap configuration file. An example of basic one is provided below, make sure to fill in IP and port (for nodeport its 32051 by default).

```yaml
node:
  id: node
  cluster: loadbalancer

admin:
  address:
    pipe:
      path: /run/envoy/envoy.sock
      mode: 0777

dynamic_resources:
  cds_config:
    resource_api_version: V3
    api_config_source:
      api_type: GRPC
      transport_api_version: V3
      grpc_services:
      - envoy_grpc:
          cluster_name: xds-servicelb
  lds_config:
    resource_api_version: V3
    api_config_source:
      api_type: GRPC
      transport_api_version: V3
      grpc_services:
      - envoy_grpc:
          cluster_name: xds-servicelb

static_resources:
  clusters:
  - name: xds-servicelb
    type: STATIC
    typed_extension_protocol_options:
      envoy.extensions.upstreams.http.v3.HttpProtocolOptions:
        "@type": type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions
        explicit_http_config:
          http2_protocol_options:
            connection_keepalive:
              interval: 60s
              timeout: 10s
    load_assignment:
      cluster_name: xds-servicelb
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                protocol: TCP
                address: __XDS_SERVICELB_IP__
                port_value: __XDS_SERVICELB_PORT__

```

