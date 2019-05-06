#!/bin/bash

source hack/test/tools.sh

kubectl get pps >/dev/null 2>&1 || : # first time may fail before swagger updates
echo "PPS: "
kubectl get pps --all-namespaces -o wide
echo "-----"
kubectl apply -f hack/test/stage1/000-service.yaml
kubectl apply -f hack/test/stage1/000-endpoints.yaml
sleep 1 # sometimes takes a second to process and re-fetch endpoints

call_grpc envoy.api.v2.ClusterDiscoveryService/FetchClusters
test_value "cluster names" resources[].name 0-6444_0_-*1-default-test-e2e-canary
test_value "EDS Clusters" resources[].edsClusterConfig.edsConfig.apiConfigSource.grpcServices[0].envoyGrpc.clusterName xds_cluster*xds_cluster
test_value "EDS Cluster Names" resources[].edsClusterConfig.serviceName 0-6444_0_-*1-default-test-e2e-canary
test_value "Timeout" resources[0].connectTimeout 5s

call_grpc envoy.api.v2.ListenerDiscoveryService/FetchListeners
test_value "Listen port" resources[0].address.socketAddress.portValue 6444
test_value "Total weight" resources[0].filterChains[0].filters[0].config.route_config.virtual_hosts[0].routes[0].route.weighted_clusters.total_weight 104

kubectl apply -f hack/test/stage1/000-service-updated.yaml
sleep 1 # sometimes takes a second to process and re-fetch endpoints
call_grpc envoy.api.v2.ClusterDiscoveryService/FetchClusters
test_value "EDS Cluster Names" resources[].edsClusterConfig.serviceName 0-6448_0_-*1-default-testt
test_value "Timeout" resources[0].connectTimeout "6s"

call_grpc envoy.api.v2.ListenerDiscoveryService/FetchListeners
test_value "Listen port" resources[0].address.socketAddress.portValue 6448
test_value "Total weight" resources[0].filterChains[0].filters[0].config.route_config.virtual_hosts[0].routes[0].route.weighted_clusters.total_weight 60

# test rollback to the original values to check service re-registration
kubectl apply -f hack/test/stage1/000-service.yaml
call_grpc envoy.api.v2.ClusterDiscoveryService/FetchClusters
test_value "cluster names" resources[].name 0-6444_0_-*1-default-test-e2e-canary
test_value "EDS Clusters" resources[].edsClusterConfig.edsConfig.apiConfigSource.grpcServices[0].envoyGrpc.clusterName xds_cluster*xds_cluster
test_value "EDS Cluster Names" resources[].edsClusterConfig.serviceName 0-6444_0_-*1-default-test-e2e-canary
test_value "Timeout" resources[0].connectTimeout "5s"

call_grpc envoy.api.v2.EndpointDiscoveryService/FetchEndpoints
test_value "EDS discovered port" resources[].endpoints[].lbEndpoints[]?.endpoint.address.socketAddress.portValue 6443*6443*6443*6443
test_value "EDS discovered addresses" resources[].endpoints[].lbEndpoints[]?.endpoint.address.socketAddress.address 10.1.2.3*10.4.5.6*10.7.8.9*192.168.1.2

kubectl apply -f hack/test/stage1/000-endpoints-updated.yaml
call_grpc envoy.api.v2.EndpointDiscoveryService/FetchEndpoints
test_value "EDS discovered port" resources[].endpoints[].lbEndpoints[]?.endpoint.address.socketAddress.portValue 9999*9999*9999*9999
test_value "EDS discovered addresses" resources[].endpoints[].lbEndpoints[]?.endpoint.address.socketAddress.address 10.10.20.30*10.40.50.60*10.70.80.90*192.168.10.20
exit 0
