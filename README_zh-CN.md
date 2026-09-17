
# KoKo

**简体中文** · [English](./README.md)

Koko 是 JumpServer 连接字符协议的终端组件，支持 SSH、TELNET、MySQL、Redis 等协议。

Koko 使用 Golang 来实现，名字来自 Dota 英雄 [Kunkka](https://www.dota2.com.cn/hero/kunkka)。

## 主要功能


- SSH
- SFTP
- web terminal
- web文件管理


## 开发环境

1. 下载项目

```shell
git clone https://github.com/jumpserver/koko.git
cd koko
```

2. 运行 server 后端

`make run` 需要 Docker Compose，用于启动 guacd，并在退出时停止它。

```shell

$ cp config_example.yml config.yml  # 1. 准备配置文件
$ vim config.yml  # 2. 修改配置文件, 编辑其中的地址 和 bootstrap key
CORE_HOST: http://127.0.0.1:8080
BOOTSTRAP_TOKEN: PleaseChangeMe<改成和core一样的>

$ make run # 3. 运行，首次运行会自动下载预编译的 libghostty-vt
```


## 构建docker镜像
使用 Docker Buildx 构建镜像并加载到本地 Docker：

```shell
make docker
```
构建成功后，生成koko镜像

本地开发测试时，导出与 JumpServer Core 一致的 Bootstrap Token，然后通过 Docker Compose 启动：

```shell
docker compose up --build
```

默认连接宿主机的 `http://host.docker.internal:8080`，SSH 端口为 `2222`，HTTP 端口为 `5050`，Web Proxy 端口为 `5001`。可通过 `CORE_HOST`、`KOKO_SSH_PORT`、`KOKO_HTTP_PORT` 和 `KOKO_WEB_PROXY_PORT` 覆盖。Web Proxy 通过现有 Koko connect ticket 和 Core 连接令牌建立会话后允许所有目标地址；未认证的 HTTP 和 CONNECT 请求返回 407，不再使用部署级主机白名单。建立后的 Web 会话由 Koko 公共 session 管理执行权限检查和管理任务。
