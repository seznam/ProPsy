#!/bin/bash
call_grpc() {
	type=$1
 
	dist/grpcurl -d '{"node": {"id": "e2e-test"}}' -import-path proto/data-plane-api -proto envoy/api/v2/cds.proto -plaintext localhost:8888 $type | tee /tmp/test.log
}
kubectl get pps >/dev/null 2>&1 || : # first time may fail before swagger updates
echo "PPS: "
kubectl get pps --all-namespaces -o wide
echo "-----"
kubectl apply -f hack/test/stage1/000-service.yaml

echo "--------------------"
echo "Checking clusterName"
call_grpc envoy.api.v2.ClusterDiscoveryService/FetchClusters
CN=`cat /tmp/test.log | jq .resources[0].loadAssignment.clusterName -r`
if [ x"$CN" != "xdefault-test" ]; then
  echo "Wrong cluster name! Failing the test!"
  exit 1
else
  echo "ok"
fi
echo "-------------------"
exit 0
