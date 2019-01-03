# 
#
#
# debug/testing
debug-envoy:
	docker run --net=host --rm -ti --name=envoy-test -v $(shell pwd)/sample/envoy-conf:/config/:ro envoyproxy/envoy:v1.9.0 /usr/local/bin/envoy --v2-config-only -l debug -c /config/envoy.yaml

alias:
	@echo alias go='docker run -u `id -u`:`id -g` --rm -ti -e XDG_CACHE_HOME=/go/src/propsy/.godir -e GO111MODULE=on -v `pwd`:/go/src/propsy -w /go/src/propsy golang:1.11-stretch go'
