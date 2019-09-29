#!/bin/sh
#

while [ "$(curl -I -m 10 -o /dev/null -s -w %{http_code} $CORE_HOST)" != "302" ]
do
    echo "wait for jms_core ready"
    sleep 2
done

cd /opt/koko
./koko
