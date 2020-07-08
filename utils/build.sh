#!/bin/bash
#
# 该build基于 golang:1.12-alpine
utils_dir=$(pwd)
project_dir=$(dirname "$utils_dir")
release_dir=${project_dir}/release
OS=${INPUT_OS-''}
ARCH=${INPUT_ARCH-''}

function install_git() {
  sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
  && apk update \
  && apk add git
}


if [[ $(uname) == 'Darwin' ]];then
  alias sedi="sed -i ''"
else
  alias sedi='sed -i'
fi


# 安装依赖包
command -v git || install_git

# 修改版本号文件
if [[ -n ${VERSION} ]]; then
  sedi "s@Version = .*@Version = \"${VERSION}\"@g" "${project_dir}/pkg/koko/koko.go" || exit 2
fi


# 下载依赖模块并构建
cd .. && go mod download || exit 3
cd cmd && CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -ldflags "-X 'main.Buildstamp=`date -u '+%Y-%m-%d %I:%M:%S%p'`' -X 'main.Githash=`git rev-parse HEAD`' -X 'main.Goversion=`go version`'" -o koko koko.go || exit 4

# 打包
rm -rf "${release_dir:?}/*"
to_dir="${release_dir}/koko"
mkdir -p "${to_dir}"

for i in koko static templates locale config_example.yml;do
  cp -r $i "${to_dir}"
done

