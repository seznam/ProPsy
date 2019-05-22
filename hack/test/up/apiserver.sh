#!/bin/bash

source hack/test/tools.sh

APISERVER_NAME=$1
PORT=$2

APISEVER=`ls /www/adm/kubernetes/bin/hyperkube`

if [ x"${APISERVER_NAME}" = "x" -o x"${PORT}" = "x" ]; then
  echo "Missing apiserver name or port!"
  exit 1
fi

if [ x"$APISERVER" = "x" ]; then
  apt update >>/tmp/log/apiserver-install.log
  apt -y install --no-install-recommends --no-install-suggests adm-kubernetes-hyperkube >>/tmp/log/apiserver-install.log
fi

nohup /www/adm/kubernetes/bin/hyperkube apiserver --etcd-servers=http://localhost:2379 --etcd-prefix="/apiserver-${APISERVER_NAME}" --insecure-port=${PORT} >/tmp/log/apiserver-${APISERVER_NAME}.log 2>&1 &