#!/bin/sh
#

if [ -n "$CORE_HOST" ]; then
    until check ${CORE_HOST}/api/health/; do
        echo "wait for jms_core ${CORE_HOST} ready"
        sleep 2
    done
fi

: ${LOG_LEVEL:='ERROR'}

echo
date
echo "KoKo Version $VERSION, more see https://www.jumpserver.com"
echo "Quit the server with CONTROL-C."
echo

# 创建 server.key server.crt
if [ ! -f /opt/koko/server.key ]; then
    openssl req -x509 -nodes -days 3650 -newkey rsa:2048 -keyout /opt/koko/server.key -out /opt/koko/server.crt -subj "/C=CN/ST=Beijing/L=Beijing/O=JumpServer/OU=JumpServer/CN=JumpServer"
fi

# /opt/koko to 700 disable other user access
chmod 700 /opt/koko

exec "$@"