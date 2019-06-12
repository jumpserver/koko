#KoKo

koko是golang版本的的coco；重构了coco的SSH/SFTP服务和web terminal服务。

##主要功能

- SSH
- SFTP
- web terminal
- web文件管理

##安装

1.下载项目

```shell
git clone https://github.com/jumpserver/koko.git
```

2.下载依赖包

koko的项目使用[dep](https://github.com/golang/dep)管理依赖包, 需要预先安装dep;

```shell
dep ensure
```

> 由于网络问题,部分依赖包需要走代理. 需要自行设置http_proxy和https_proxy代理便于下载

3.编译应用

先进入cmd文件夹, 并构建应用.
```shell
cd cmd
```
```shell
make linux
```
> 如果构建成功，会在项目下自动生成build文件夹,里面包含当前分支的linux 64位版本压缩包.

##使用

1.拷贝压缩包文件到服务器

2.解压编译的压缩包
```shell
tar xzf koko-[branch name]-[commit]-linux-amd64.tar.gz
```

3.创建配置文件config.yml,配置参数请参考[coco](https://github.com/jumpserver/coco/blob/master/config_example.yml)
```shell
touch config.yml
```

4.运行koko
```shell
cd kokodir
./koko
```

##构建docker镜像

进入cmd文件夹
```shell
cd cmd
```
```shell
make docker
```
构建成功后，生成koko镜像
> 由于网络问题,部分依赖包需要走代理. 需要自行设置http_proxy和https_proxy代理便于下载
