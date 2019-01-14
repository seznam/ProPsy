#!/bin/bash

source hack/test/tools.sh

kubectl get pps >/dev/null 2>&1 || : # first time may fail before swagger updates
echo "PPS: "
kubectl get pps --all-namespaces -o wide
echo "-----"
kubectl apply -f hack/test/stage1/000-service.yaml

call_grpc envoy.api.v2.ClusterDiscoveryService/FetchClusters
test_value "Cluster name" resources[0].loadAssignment.clusterName default-test
test_value "Timeout" resources[0].connectTimeout "5s"
exit 0