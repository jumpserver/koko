#!/bin/sh
#

while [ "$(curl -I -m 10 -o /dev/null -s -w %{http_code} ${CORE_HOST}/api/health/)" != "200" ]
do
    echo "wait for jms_core $CORE_HOST ready"
    sleep 2
done
# 限制所有可执行目录的权限
chmod 700 /usr/local/sbin && chmod 700 /usr/local/bin
chmod 700 /usr/sbin && chmod 700 /sbin &&  chmod 700 /bin


# 放开部分需要的可执行权限
chmod 755 `which mysql` `which psql` `which mongosh` `which tsql` `which redis` `which clickhouse`
chmod 755 `which kubectl` `which rawkubectl` `which helm` `which rawhelm`

cd /opt/koko
./koko
