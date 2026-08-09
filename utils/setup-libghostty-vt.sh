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

case "${target}" in
	darwin-arm64) expected_sha256=1c47ab5795f55c398a24b3dde2633f9bc7ce6ae0701c713b21a3504f5c2701e9 ;;
	linux-amd64-glibc) expected_sha256=26c3c09230499edcf308913c1ca3fee4d33aaf572d383f7f9cd0d72e3459da87 ;;
	linux-amd64-musl) expected_sha256=49456f124e35ff26c3e37894dee33bc7dbf68efe7f16a182f98afc9edf5a4116 ;;
	linux-arm64-glibc) expected_sha256=a79ee6de2a58519b6d34af723f78e5ea27e24ac4010eded9ab65fa39f43eb2e7 ;;
	linux-arm64-musl) expected_sha256=66a41be0c1228997e503f9ab79ae45e13b90ad31e8406b8c1d31332e46e78959 ;;
esac

target_dir=${cache_dir}/${release_tag}/${target}
install_dir=${target_dir}/libghostty-vt
if [[ -f ${install_dir}/lib/libghostty-vt.a &&
	-f ${install_dir}/lib/pkgconfig/libghostty-vt-static.pc &&
	-f ${install_dir}/include/ghostty/vt.h ]]; then
	printf '%s\n' "${install_dir}"
	exit 0
fi

if command -v sha256sum >/dev/null 2>&1; then
	checksum_command=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
	checksum_command=(shasum -a 256)
else
	die "missing required command: sha256sum or shasum"
fi

mkdir -p -- "${target_dir}"
work_dir=$(mktemp -d "${target_dir}/download.XXXXXX")
trap 'rm -rf -- "${work_dir}"' EXIT

asset=${asset_prefix}-${target}.tar.gz
archive=${work_dir}/${asset}
printf 'libghostty-vt: downloading %s\n' "${asset}" >&2
curl --fail --location --retry 3 --output "${archive}" "${release_url}/${asset}"

actual_sha256=$("${checksum_command[@]}" "${archive}" | awk '{print $1}')
[[ ${actual_sha256} == "${expected_sha256}" ]] || die "checksum mismatch for ${asset}"

tar -xzf "${archive}" -C "${work_dir}"
extracted_dir=${work_dir}/libghostty-vt
[[ -f ${extracted_dir}/lib/libghostty-vt.a ]] || die "static library is missing from ${asset}"
[[ -f ${extracted_dir}/lib/pkgconfig/libghostty-vt-static.pc ]] || die "pkg-config metadata is missing from ${asset}"
[[ -f ${extracted_dir}/include/ghostty/vt.h ]] || die "headers are missing from ${asset}"

rm -rf -- "${install_dir}"
mv -- "${extracted_dir}" "${install_dir}"
printf '%s\n' "${install_dir}"
