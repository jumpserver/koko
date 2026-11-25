#!/usr/bin/env bash
set -eu

mode=$1
bin_dir=$2
case "${KAEL_ENABLED:-true}" in
    true|1) enable_kael=true ;;
    false|0) enable_kael=false ;;
    *) echo "Invalid KAEL_ENABLED: $KAEL_ENABLED" >&2; exit 1 ;;
esac

usql_dir=$(./utils/setup-usql.sh)
export PATH="$usql_dir:$PATH"
if [ "$enable_kael" = true ]; then
    CGO_ENABLED=0 go build -trimpath -o "$bin_dir/kael" ./cmd/kael
fi

cleanup() {
    status=$?
    trap - EXIT
    trap '' INT TERM
    for pid in $(jobs -pr); do kill -TERM "$pid" 2>/dev/null || :; done
    wait || :
    if [ "$mode" = run ]; then docker compose -f docker-compose-guacd.yml down || :; fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ "$mode" = run ]; then docker compose -f docker-compose-guacd.yml up -d; fi
if [ "$enable_kael" = true ]; then
    (
        case "${KAEL_CONFIG_FILE:-}" in
            ''|/*) ;;
            *) export KAEL_CONFIG_FILE="$PWD/$KAEL_CONFIG_FILE" ;;
        esac
        case "${CODEX_BINARY:-}" in
            /*) ;;
            */*) export CODEX_BINARY="$PWD/$CODEX_BINARY" ;;
        esac
        cd cmd/kael
        NAME="${KAEL_NAME:-}" BIND_HOST="${KAEL_BIND_HOST:-}" HTTPD_PORT="${KAEL_HTTP_PORT:-}" exec "$bin_dir/kael"
    ) &
fi
"$bin_dir/koko" &
wait "$!"
