#!/bin/sh
#

until /usr/local/bin/check ${CORE_HOST}/api/health/; do
    echo "wait for jms_core ${CORE_HOST} ready"
    sleep 2
done

# 限制所有可执行目录的权限
chmod -R  700 /usr/local/sbin/* && chmod -R 700 /usr/local/bin/* && chmod -R 700 /usr/bin/*
chmod -R  700 /usr/sbin/* && chmod -R 700 /sbin/* && chmod -R 700 /bin/*

function init_jms_k8s_user(){
    echo `getent passwd | grep 'jms_k8s_user' || useradd -M -U -d /nonexistent jms_k8s_user` > /dev/null 2>&1
    echo `getent passwd | grep 'jms_k8s_user' | grep '/nonexistent'  || usermod -d /nonexistent jms_k8s_user` > /dev/null 2>&1
    echo `getent group | grep 'jms_k8s_user' || groupadd jms_k8s_user` > /dev/null 2>&1
}
init_jms_k8s_user

# 放开部分需要的可执行权限
chmod 755 `which mysql` `which psql` `which mongosh` `which tsql` `which redis` `which clickhouse-client`
chmod 755 `which kubectl` `which rawkubectl` `which helm` `which rawhelm`

# k8s 集群连接需要的命令
chown :jms_k8s_user  `which jq` `which less` `which vim` `which ls` `which bash` `which grep`
chmod  750 `which jq` `which less` `which vim` `which ls` `which bash` `which grep`

cd /opt/koko
./koko