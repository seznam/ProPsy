#!/usr/bin/env bash
call_grpc() {
	type=$1

	dist/grpcurl -d '{"node": {"id": "e2e-test"}}' -import-path proto/data-plane-api -proto envoy/api/v2/cds.proto -plaintext localhost:8888 $type | tee /tmp/test.log
	echo "--------------------"
}

test_value() {
    name=$1
    key=$2
    value=$3

    echo -n "Testing $name: "
    found=`cat /tmp/test.log | jq ".${key}" -r`
    if [ x"${found}" != x"${value}" ]; then
        echo "Failed: have ${found}"
        exit 1
    else
        echo "ok"
    fi
    echo "------------------"
}