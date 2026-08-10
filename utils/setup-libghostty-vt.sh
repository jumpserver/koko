#!/usr/bin/env bash

set -euo pipefail

release_tag=ghostty-d4ac93a0395d-zig-0.15.2
asset_prefix=libghostty-vt-d4ac93a0395d-zig-0.15.2
release_url=https://github.com/LeeEirc/libghostty-vt/releases/download/${release_tag}

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
project_dir=$(CDPATH='' cd -- "${script_dir}/.." && pwd)
cache_dir=${LIBGHOSTTY_VT_CACHE_DIR:-${project_dir}/third_party/libghostty-vt}

die() {
	printf 'libghostty-vt: %s\n' "$*" >&2
	exit 1
}

for command_name in go curl tar pkg-config; do
	command -v "${command_name}" >/dev/null 2>&1 || die "missing required command: ${command_name}"
done

goos=$(go env GOOS)
goarch=$(go env GOARCH)

case "${goos}:${goarch}" in
	darwin:arm64)
		target=darwin-arm64
		;;
	linux:amd64|linux:arm64)
		libc=${LIBGHOSTTY_VT_LIBC:-}
		if [[ -z ${libc} ]]; then
			if command -v getconf >/dev/null 2>&1 && getconf GNU_LIBC_VERSION >/dev/null 2>&1; then
				libc=glibc
			elif command -v ldd >/dev/null 2>&1 && (ldd --version 2>&1 || true) | grep -qi musl; then
				libc=musl
			else
				die "cannot detect Linux libc; set LIBGHOSTTY_VT_LIBC to glibc or musl"
			fi
		fi
		case "${libc}" in
			glibc|musl) ;;
			*) die "unsupported Linux libc: ${libc}" ;;
		esac
		target=linux-${goarch}-${libc}
		;;
	*)
		die "unsupported development platform: ${goos}/${goarch}"
		;;
esac

target_dir=${cache_dir}/${release_tag}/${target}
install_dir=${target_dir}/libghostty-vt
if [[ -f ${install_dir}/lib/libghostty-vt.a &&
	-f ${install_dir}/lib/pkgconfig/libghostty-vt-static.pc &&
	-f ${install_dir}/include/ghostty/vt.h ]]; then
	printf '%s\n' "${install_dir}"
	exit 0
fi

mkdir -p -- "${target_dir}"
work_dir=$(mktemp -d "${target_dir}/download.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT

asset=${asset_prefix}-${target}.tar.gz
archive=${work_dir}/${asset}
printf 'libghostty-vt: downloading %s\n' "${asset}" >&2
curl --fail --location --retry 3 --output "${archive}" "${release_url}/${asset}"

tar -xzf "${archive}" -C "${work_dir}"
extracted_dir=${work_dir}/libghostty-vt
[[ -f ${extracted_dir}/lib/libghostty-vt.a ]] || die "static library is missing from ${asset}"
[[ -f ${extracted_dir}/lib/pkgconfig/libghostty-vt-static.pc ]] || die "pkg-config metadata is missing from ${asset}"
[[ -f ${extracted_dir}/include/ghostty/vt.h ]] || die "headers are missing from ${asset}"

rm -rf -- "${install_dir}"
mv -- "${extracted_dir}" "${install_dir}"
printf '%s\n' "${install_dir}"
