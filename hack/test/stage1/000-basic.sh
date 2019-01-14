#!/bin/bash

source hack/test/tools.sh

kubectl get pps >/dev/null 2>&1 || : # first time may fail before swagger updates
echo "PPS: "
kubectl get pps --all-namespaces -o wide
echo "-----"
kubectl apply -f hack/test/stage1/000-service.yaml

call_grpc envoy.api.v2.ClusterDiscoveryService/FetchClusters
test_value "Cluster name" resources[0].loadAssignment.clusterName ko-default-kubernetes
test_value "Endpoint port" resources[0].loadAssignment.endpoints[0].lbEndpoints[0].endpoint.address.socketAddress.portValue 6443
test_value "Timeout" resources[0].connectTimeout "5s"

call_grpc envoy.api.v2.ListenerDiscoveryService/FetchListeners
test_value "Listen port" resources[0].address.socketAddress.portValue 6444
test_value "Total weight" resources[0].filterChains[0].filters[0].config.route_config.virtual_hosts[0].routes[0].route.weighted_clusters.total_weight 99

kubectl apply -f hack/test/stage1/000-service-updated.yaml
call_grpc envoy.api.v2.ClusterDiscoveryService/FetchClusters
test_value "Timeout" resources[0].connectTimeout "6s"

call_grpc envoy.api.v2.ListenerDiscoveryService/FetchListeners
test_value "Listen port" resources[0].address.socketAddress.portValue 6448
test_value "Total weight" resources[0].filterChains[0].filters[0].config.route_config.virtual_hosts[0].routes[0].route.weighted_clusters.total_weight 50

# test rollback to the original values to check service re-registration
kubectl apply -f hack/test/stage1/000-service.yaml
call_grpc envoy.api.v2.ClusterDiscoveryService/FetchClusters
test_value "Cluster name" resources[0].loadAssignment.clusterName ko-default-kubernetes
test_value "Endpoint port" resources[0].loadAssignment.endpoints[0].lbEndpoints[0].endpoint.address.socketAddress.portValue 6443
test_value "Timeout" resources[0].connectTimeout "5s"

exit 0