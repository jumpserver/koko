
# KoKo

**简体中文** · [English](./README.md)

Koko 是 JumpServer 连接字符协议的终端组件，支持 SSH、TELNET、MySQL、Redis 等协议。

Koko 使用 Golang 和 Vue 来实现，名字来自 Dota 英雄 [Kunkka](https://www.dota2.com.cn/hero/kunkka)。

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


## 开发环境

1. 运行 server 后端

```shell

$ cp config_example.yml config.yml  # 1. 准备配置文件
$ vim config.yml  # 2. 修改配置文件, 编辑其中的地址 和 bootstrap key
CORE_HOST: http://127.0.0.1:8080
BOOTSTRAP_TOKEN: PleaseChangeMe<改成和core一样的>

$ go run cmd/koko/koko.go # 3. 运行, 运行需要 go 如果没有，golang.org 下载安装
```


2. 运行 ui 前端

```shell
$ cd ui 
$ yarn install
$ npm run serve
```

3. 测试
在 luna 访问 linux 资产，复制 iframe 地址，端口修改为 9530 即可，也可以修改 nginx 将 /koko 映射到这里

### 无 Core Web 开发模式

`make run-dev` 会启动 Koko 和仅监听本机回环地址的内置开发 Core，启动前不需要构建 `ui/dist`。另一个终端运行 `make run-ui`，由 Vite 提供页面，然后访问 `/koko/connect/?token=dev`。

启动前通过环境变量提供开发资产：

| 环境变量 | 必需 | 说明 |
| --- | --- | --- |
| `KOKO_DEV_ASSET_HOST` | 是 | SSH/SFTP 资产地址 |
| `KOKO_DEV_ASSET_USERNAME` | 是 | 资产用户名 |
| `KOKO_DEV_ASSET_PASSWORD` | 是 | 资产密码 |
| `KOKO_DEV_ASSET_PORT` | 否 | SSH/SFTP 端口，默认 `22` |
| `KOKO_DEV_ASSET_NAME` | 否 | 页面显示的资产名称 |
| `KOKO_DEV_SFTP_ROOT` | 否 | SFTP 根目录，默认使用用户家目录 |
| `KOKO_DEV_CORE_PORT` | 否 | 内置开发 Core 端口，默认 `18080` |

Terminal AI 使用 OpenAI-compatible 接口。设置 `KOKO_DEV_AI_API_KEY` 和 `KOKO_DEV_AI_MODEL` 后启用；`KOKO_DEV_AI_BASE_URL` 和 `KOKO_DEV_AI_PROXY` 可选。AI 未配置时不影响 Web Terminal 和 SFTP。

## 构建docker镜像
依赖 docker buildx 构建多平台镜像，需要安装 docker 19.03+ 版本，并开启 docker buildx 插件。

```shell
make docker
```
构建成功后，生成koko镜像
