#!/usr/bin/env bash

set -euo pipefail

release_tag=v0.1.13
release_url=https://github.com/jumpserver-dev/usql/releases/download/${release_tag}

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
project_dir=$(CDPATH='' cd -- "${script_dir}/.." && pwd)
cache_dir=${USQL_CACHE_DIR:-${project_dir}/third_party/usql}

die() {
	printf 'usql: %s\n' "$*" >&2
	exit 1
}

for command_name in go curl tar; do
	command -v "${command_name}" >/dev/null 2>&1 || die "missing required command: ${command_name}"
done

goos=$(go env GOOS)
goarch=$(go env GOARCH)

case "${goos}:${goarch}" in
	darwin:amd64|darwin:arm64|linux:amd64|linux:arm64) ;;
	*) die "unsupported development platform: ${goos}/${goarch}" ;;
esac

target_dir=${cache_dir}/${release_tag}/${goos}-${goarch}
install_dir=${target_dir}/bin
if [[ -f ${install_dir}/usql && -x ${install_dir}/usql ]]; then
	printf '%s\n' "${install_dir}"
	exit 0
fi

mkdir -p -- "${target_dir}"
work_dir=$(mktemp -d "${target_dir}/download.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT

asset=usql-${release_tag}-${goos}-${goarch}.tar.gz
archive=${work_dir}/${asset}
printf 'usql: downloading %s\n' "${asset}" >&2
curl --fail --location --retry 3 --output "${archive}" "${release_url}/${asset}"

tar -xzf "${archive}" -C "${work_dir}"
[[ -f ${work_dir}/usql ]] || die "binary is missing from ${asset}"
chmod 755 "${work_dir}/usql"
mkdir -p -- "${install_dir}"
mv -- "${work_dir}/usql" "${install_dir}/usql"
printf '%s\n' "${install_dir}"
