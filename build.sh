#!/bin/sh

# 可以设置一些ENV

# 输出所有内容到目录releases中

cd cmd && go build -ldflags "-X 'main.Buildstamp=`date -u '+%Y-%m-%d %I:%M:%S%p'`' -X 'main.Githash=`git rev-parse HEAD`' -X 'main.Goversion=`go version`'" -x -o koko koko.go

exit_code=$?
if [ ${exit_code} != 0 ];then
    echo "Exist code: ${exit_code}"
    exit ${exit_code}
fi

mkdir -p ../release && rm -rf ../release/*
mkdir -p ../release/koko
cp -R koko locale static templates config_example.yml ../release/koko
