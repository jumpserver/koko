#!/bin/sh
#

if [ -n "$CORE_HOST" ]; then
    until check ${CORE_HOST}/api/health/; do
        echo "wait for jms_core ${CORE_HOST} ready"
        sleep 2
    done
fi

export WORK_DIR=/opt/koko
export COMPONENT_NAME=koko
export WISP_TRACE_PROCESS=1
export EXECUTE_PROGRAM=/opt/koko/koko

if [ ! "$LOG_LEVEL" ]; then
    export LOG_LEVEL=ERROR
fi

exec "$@"