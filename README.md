# ProPsy
ProPsy is a very useful tool that distributes config to remote Envoy nodes. It does so by a feature of Envoy - gRPC streaming discovery. Each Envoy gets set a name and a path to a discovery cluster, from which it pulls all of its config - Listeners, Clusters, Routes and Endpoints. What ProPsy does is listen across **MULTIPLE** Kubernetes clusters for these ProPsy Service events and Endpoint events to generate these configs.

## How to use
First you need some Envoy. Start by installing recent `envoy` package (let's say 1.9+ as that has been tested and compiled protobufs against) and configure its node name to "my-proxy". Next you need to set up a CRD in your kubernetes cluster as defined in `deployment/kubernetes/crd-service.yaml`. Then you can start creating these "ProPsy Services":
```yaml
apiVersion: propsy.seznam.cz/v1
kind: ProPsyService
metadata:
  name: miniapps
  namespace: ftxt-hint
spec:
  disabled: false
  listen: 0:4205
  nodes:
  - my-proxy
  percent: 100
  service: miniapps
  servicePort: 4041
  timeout: 800
  type: HTTP
  path: /miniapps/
  tlsCertificateSecret: test-locality-tls
```
What this will create is:
- Listener on 0:4205 (any interface/IP, port 4205)
- Work within namespace ftxt-hint
- Set 100% of traffic to this service
- Forward traffic to service ftxt-hint/miniapps port 4041
- Set connect timeout to 800ms
- Proxy as HTTP traffic
- Set path prefix to /miniapps/
- Serve as HTTPS with cert from secret `test-locality-tls` from the same namespace
- distribute this config to nodes that are named `my-proxy` and nowhere else

Now it is time to start the ProPsy daemon itself (can be within k8s cluster or outside):
```
./propsy-bin -listen ":9999" -zone ko -debug -clientverifyca cert/client-ca.pem -servercert cert/server.pem -serverkey cert/server-key.pem -configcluster /home/ashley/kubeconfig-propsy-ko.yaml:ko -endpointcluster /home/ashley/kubeconfig-propsy-ng.yaml:ng -endpointcluster /home/ashley/kubeconfig-propsy-ko.yaml:ko
```
Flags:
- listen: obvious, on which IP/port to listen (default `:8888`)
- zone: local zone, preferred traffic goes to there (more useful than setting it on Envoy side as there is some logic not implemented and just missing)
- debug: allow debug output (not required)
- clientverifyca: Path to CA that will be used to verify incoming requests for valid clients
- servercert: Path to CERT file that will be used for the gRPC server
- serverkey: Path to KEY file that will be used for the gRPC server (note that all the 3 TLS options need to be set to allow any form of TLS!)
- configcluster: multiple pairs of `<path to kubeconfig>:<cluster name>` to gather PPS from. Please note, that at least one cluster name should match the zone as it will be considered as `local zone` for preferred traffic weights.
- endpointcluster: multiple pairs of `<path to kubeconfig>:<cluster name>` to gather endpoints from. This should match PPS and `local zone` for preferred traffic

Now you need to actually start your Envoy instance. There is, however, one requirement: the discovery cluster must be called `xds_cluster` as it is what the ProPsy distributes as upstream discovery cluster for endpoints.

Sample Envoy config:
```yaml
admin:
  access_log_path: /tmp/admin_access.log
  address:
    socket_address: { address: 0.0.0.0, port_value: 9901 }

dynamic_resources:
  lds_config:
    api_config_source:
      api_type: gRPC
      grpc_services:
        envoy_grpc:
          cluster_name: xds_cluster
  cds_config:
    api_config_source:
      api_type: gRPC
      grpc_services:
        envoy_grpc:
          cluster_name: xds_cluster

cluster_manager:
  local_cluster_name: xds_cluster

node:
  id: my_proxy
  cluster: ko
  locality:
    region: ko
    zone: ko
    sub_zone: admins5

static_resources:
  clusters:
  - name: xds_cluster
    connect_timeout: 0.25s
    type: STATIC
    tls_context:
      common_tls_context:
        tls_certificates:
        - certificate_chain: { "filename": "/config/cert/localhost.pem" }
          private_key: { "filename": "/config/cert/localhost-key.pem" }
        validation_context:
          trusted_ca: { "filename": "/config/cert/server-ca.pem" }
    lb_policy: ROUND_ROBIN
    http2_protocol_options: {}
    commonLbConfig:
      zone_aware_lb_config:
        min_cluster_size: 1
    load_assignment:
      cluster_name: xds_cluster
      endpoints:
      - locality:
          region: ko
          zone: ko
          sub_zone: admins5
        lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 127.0.0.1
                port_value: 8.8.8.8
```
And launch it as

```
envoy --config-path conf/envoy.yaml --service-cluster xds_cluster --service-zone ko --v2-config-only -l info
```

And you should be done! :)

## FAQ 
### Multi-clustering
ProPsy supports running across multiple clusters. How, do you ask? Try creating a PPS with the **same** name+namespace in the other locality. Suddenly you can control traffic. 

Note, that the main locality controls the service's type (HTTP, TCP), possible TLS certificate and % of the traffic. Setting 50% of traffic in the other locality has no effect, just as setting canary zone in the other locality.

### Weights Example
Cluster A: (local zone)
- service 30
- canary service 5

Cluster B: (foreign zone)
- service 30
- canary service 10

Weights go as follows from the point of cluster A:

30 requests go to local service, 5 go to canary service and foreign receives 70 requests out of 105. 

### Different services on one port
ProPsy fully supports running multiple services on one port with different paths. Just set them to
- the same node
- the same listen
- the same type
- different path

### Only foreign cluster service
Discovery works across all connected clusters. That means that you can run some PPS only in a foreign cluster.

### Debugging
If there's something odd happening, it is possible to view the actual data ProPsy is distributing by using `grpcurl` (get it somewhere online or from a 1st stage in our gitlab pipeline builds) and fetching all the protobufs:
```
grpcurl -d '{"node": {"id": "my-proxy"}}' -import-path proto/data-plane-api/ -proto envoy/api/v2/lds.proto -plaintext localhost:8888 envoy.api.v2.ListenerDiscoveryService/FetchListeners | jq .
grpcurl -d '{"node": {"id": "my-proxy"}}' -import-path proto/data-plane-api/ -proto envoy/api/v2/cds.proto -plaintext localhost:8888 envoy.api.v2.ClusterDiscoveryService/FetchClusters | jq .
grpcurl -d '{"node": {"id": "my-proxy"}}' -import-path proto/data-plane-api/ -proto envoy/api/v2/eds.proto -plaintext localhost:8888 envoy.api.v2.EndpointDiscoveryService/FetchEndpoints | jq .
```

(Routes are distributed within Listener discovery)

### Bug reports
Please report bugs to us (admins5) IMMEDIATILY so we can fix them ASAP. Either by creating a gitlab issue or just sending us MR with a fix :)
