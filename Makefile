# 
#
#
# debug/testing
debug-envoy:
	docker run --net=host --rm -ti --name=envoy-test -v $(shell pwd)/sample/envoy-conf:/config/:ro envoyproxy/envoy:v1.9.0 /usr/local/bin/envoy --v2-config-only -l debug -c /config/envoy-grpc.yaml --service-zone ko --service-cluster xds_cluster

debug-envoy-rest:
	docker run --net=host --rm -ti --name=envoy-test -v $(shell pwd)/sample/envoy-conf:/config/:ro envoyproxy/envoy:v1.9.0 /usr/local/bin/envoy --v2-config-only -l debug -c /config/envoy-rest.yaml

coverage:
	go test -cover -coverprofile=/tmp/propsy-cover.out ./...
	go tool cover -html=/tmp/propsy-cover.out

build:
	CGO_ENABLED=0 go build -ldflags='-s -w -extldflags "-static"'
