# 
#
#
# debug/testing
debug-envoy:
	docker run --net=host --rm -ti --name=envoy-test -v $(shell pwd)/sample/envoy-conf:/config/:ro envoyproxy/envoy:v1.11.0 /usr/local/bin/envoy -l debug -c /config/envoy.yaml --service-zone ko --service-cluster xds_cluster

debug-envoy-rest:
	docker run --net=host --rm -ti --name=envoy-test -v $(shell pwd)/sample/envoy-conf:/config/:ro envoyproxy/envoy:v1.11.0 /usr/local/bin/envoy -l debug -c /config/envoy-rest.yaml

coverage:
	go test -cover -coverprofile=/tmp/propsy-cover.out ./pkg/...
	go tool cover -html=/tmp/propsy-cover.out

build:
	CGO_ENABLED=0 go build -ldflags='-s -w -extldflags "-static"'
