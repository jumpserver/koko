
# KoKo

koko是golang版本的的coco；重构了coco的SSH/SFTP服务和web terminal服务。


## 主要功能


- SSH
- SFTP
- web terminal
- web文件管理


## 安装

1.下载项目

```shell
go get github.com/jumpserver/koko
```

2.编译应用

先进入cmd文件夹, 并构建应用.
```shell
cd cmd
```
```shell
make linux
```
> 如果构建成功，会在项目下自动生成build文件夹,里面包含当前分支的linux 64位版本压缩包.
因为使用go mod进行依赖管理，可以设置GOPROXY=https://goproxy.io代理下载部分依赖包。

## 使用

1.拷贝压缩包文件到服务器

2.解压编译的压缩包
```shell
tar xzf koko-[branch name]-[commit]-linux-amd64.tar.gz
```

3.创建配置文件config.yml,配置参数请参考[cmd](https://github.com/jumpserver/koko/tree/master/cmd)目录下的config_example.yml文件
```shell
touch config.yml
```

4.运行koko
```shell
cd kokodir
./koko
```


## 构建docker镜像

进入cmd文件夹
```shell
cd cmd
```
```shell
make docker
```
构建成功后，生成koko镜像
