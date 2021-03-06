#!/usr/bin/env bash
call_grpc() {
	type=$1

	dist/grpcurl -d '{"node": {"id": "e2e-test"}}' \
	    -import-path hack/test/proto/googleapis \
      -import-path hack/test/proto/data-plane-api \
      -import-path hack/test/proto/protoc-gen-validate \
	    -proto hack/test/proto/data-plane-api/envoy/api/v2/cds.proto \
	    -proto hack/test/proto/data-plane-api/envoy/api/v2/lds.proto \
	    -proto hack/test/proto/data-plane-api/envoy/api/v2/rds.proto \
	    -proto hack/test/proto/data-plane-api/envoy/api/v2/eds.proto \
	    -plaintext localhost:8888 $type 2>&1 | tee /tmp/test.log
	echo "--------------------"
}

test_value() {
    name=$1
    key=$2
    value=$3

    echo -n "Testing $name: "
    found=`cat /tmp/test.log | jq -S ".${key}" -r | sort | tr "\n" "*" | sed 's/*$//'`
    if [ x"${found}" != x"${value}" ]; then
        echo "Failed: have ${found}, was expecting ${value}"
        exit 1
    else
        echo "ok"
    fi
    echo "------------------"
}
