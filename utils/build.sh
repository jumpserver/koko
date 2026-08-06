#!/bin/sh
utils_dir=$(pwd)
project_dir=$(dirname "$utils_dir")
release_dir=${project_dir}/release
OS=${INPUT_OS-'linux'}

if [[ -n "${GOOS-}" ]];then
  OS="${GOOS}"
fi

function install_git() {
  sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
  && apk update \
  && apk add git
}

# 安装依赖包
command -v git || install_git
kokoVersion='unknown'
goVersion="$(go version)"
gitHash="$(git rev-parse HEAD)"
buildStamp="$(date -u '+%Y-%m-%d %I:%M:%S%p')"
set +x
cipherKey="$(head -c 100 /dev/urandom | base64 | head -c 32)"
# 修改版本号文件
if [[ -n "${VERSION-}" ]]; then
  kokoVersion="${VERSION}"
fi

goldflags="-X 'main.Buildstamp=$buildStamp' -X 'main.Githash=$gitHash' -X 'main.Goversion=$goVersion' -X 'github.com/jumpserver/koko/pkg/koko.Version=$kokoVersion' -X 'github.com/jumpserver/koko/pkg/config.CipherKey=$cipherKey'"
k8scmdflags="-X 'github.com/jumpserver/koko/pkg/config.CipherKey=$cipherKey'"
# 下载依赖模块并构建
cd .. && go mod download || exit 3
CGO_ENABLED=0 GOOS="$OS" go build -ldflags "$goldflags" -o koko ${project_dir}/cmd/koko/ || exit 4
set -x

# 打包
rm -rf "${release_dir:?}/*"
to_dir="${release_dir}/koko"
mkdir -p "${to_dir}"

cp -r "${utils_dir}/init-kubectl.sh" "${to_dir}"

for i in koko kubectl helm static templates locale config_example.yml;do
  cp -r $i "${to_dir}"
done
