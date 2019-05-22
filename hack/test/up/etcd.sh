#!/bin/bash

source hack/test/tools.sh

apt update >>/tmp/log/etcd.log
apt -y install --no-install-recommends --no-install-suggests adm-etcd >>/tmp/log/etcd.log

nohup /www/adm/etcd/bin/etcd --data-dir /tmp/etcd --name etcd-test >>/tmp/log/etcd.log 2>&1 &
