#!/bin/bash -eux

mkdir -p /data/gopath
export GOPATH=/data/gopath

yum -y install epel-release
yum -y install git go gcc


# 输出所有内容到目录releases中
build_stamp=$(date -u '+%Y-%m-%d %I:%M:%S%p')
git_hash=$(git rev-parse HEAD || echo 'None')
go_version=$(go version)

#cd cmd && go build -ldflags "-X 'main.Buildstamp=${build_stamp}' -X 'main.Githash=${git_hash}' -X 'main.Goversion=${go_version}'" -o koko koko.go
#
#exit_code=$?
#if [ ${exit_code} != 0 ];then
#    echo "Exist code: ${exit_code}"
#    exit ${exit_code}
#fi
touch koko

mkdir -p ../release && rm -rf ../release/*
output_dir="../release/koko"
mkdir -p ${output_dir}
cp -R koko locale static templates config_example.yml ${output_dir}

output=$(cd ${output_dir} && pwd)
echo "${output}"


