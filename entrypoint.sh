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
echo "KoKo Version $VERSION, more see https://www.jumpserver.org"
echo "Quit the server with CONTROL-C."
echo

exec "$@"