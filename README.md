
# KoKo

Koko 是 Go 版本的 coco；重构了 coco 的 SSH/SFTP 服务和 Web Terminal 服务


## 主要功能


- SSH
- SFTP
- web terminal
- web文件管理


## 安装

1.下载项目

```shell
git clone https://github.com/jumpserver/koko.git
```

2.编译应用

在 koko 项目下构建应用.
```shell
make
```
> 如果构建成功，会在项目下自动生成 build 文件夹，里面包含当前分支各种架构版本的压缩包。
默认构建的 VERSION 为 [branch name]-[commit]。
因为使用go mod进行依赖管理，可以设置环境变量 GOPROXY=https://goproxy.io 代理下载部分依赖包。

## 使用 (以 Linux amd64 服务器为例)

1.拷贝压缩包文件到对应的服务器

```
通过 make 构建默认的压缩包，文件名如下: 
koko-[branch name]-[commit]-linux-amd64.tar.gz
```

2.解压编译的压缩包
```shell
tar xzvf koko-[branch name]-[commit]-linux-amd64.tar.gz
```

3.创建配置文件config.yml，配置参数请参考[config_example.yml](https://github.com/jumpserver/koko/blob/master/config_example.yml)文件
```shell
touch config.yml
```

4.运行koko
```shell
cd koko-[branch name]-[commit]-linux-amd64

./koko
```


## 构建docker镜像

```shell
make docker
```
构建成功后，生成koko镜像
