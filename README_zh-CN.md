
# KoKo

**简体中文** · [English](./README.md)

Koko 是 JumpServer 连接字符协议的终端组件，支持 SSH、TELNET、WinRM、MySQL、Redis 等协议。

Koko 使用 Golang 来实现，名字来自 Dota 英雄 [Kunkka](https://www.dota2.com.cn/hero/kunkka)。

## 主要功能


- SSH
- SFTP
- web terminal
- web文件管理

## 通过 WinRM 连接 Windows PowerShell

Windows 资产中已授权且公开的 `winrm` 协议可通过 Koko Web CLI 连接，支持 Luna 网页和桌面客户端。需要同步更新 JumpServer Core、Koko 和 Luna，并执行 `assets.0031_winrm_terminal_access` 迁移，使已有平台的 WinRM 协议可供用户连接。

Koko 使用 PSRP over WinRM 打开持久 PowerShell 会话，通过账号密码进行 NTLM 认证。配置资产的 WinRM 监听端口（HTTP 通常为 5985，HTTPS 通常为 5986）及平台的 `use_ssl` 设置。两种传输均要求 NTLM 消息加密。HTTPS 默认校验目标证书；可配置可信 CA，或在需要时明确启用平台的 `allow_invalid_cert`。Windows 账号需要具有 PowerShell 远程管理端点的访问权限。

终端支持完整的单行 PowerShell 命令或脚本、行编辑、历史记录、Ctrl-C 取消及 Ctrl-D 断开。变量与当前目录和 AI 命令执行器共享。AI 的 Auto 和 PTY 模式会在当前终端显示命令、流式输出并进入会话录像；Background 模式仅向 AI 返回输出。所有模式都通过 PSRP 执行并保留命令审计。不支持交互式宿主输入提示及全屏控制台程序。终端每条命令限制 4095 个字符，AI 命令限制 64 KiB、最长十分钟。PSRP 返回管道状态，因此 AI 结果不会虚构操作系统退出码。

提交的命令和有界输出接入现有命令审计、命令 ACL 审批及告警通知。AI 命令命中“通知并告警”ACL 时会停止执行，需要用户在手动终端执行并确认；AI 任务批准不能替代该风险确认。终端活动沿用会话录像和权限生命周期。传输失败后不会自动重发已经认证的 WinRM 请求；响应丢失时，命令的执行结果可能无法确认。


## 开发环境

1. 下载项目

```shell
git clone https://github.com/jumpserver/koko.git
cd koko
```

2. 运行 server 后端

`make run` 需要 Docker Compose，用于启动 guacd，并在退出时停止它。
使用 `make dev` 运行 Koko 时不会启动或停止 guacd，无需 Docker Compose。

```shell

$ cp config_example.yml config.yml  # 1. 准备配置文件
$ vim config.yml  # 2. 修改配置文件, 编辑其中的地址 和 bootstrap key
CORE_HOST: http://127.0.0.1:8080
BOOTSTRAP_TOKEN: PleaseChangeMe<改成和core一样的>

$ make run # 3. 运行，首次运行会自动下载预编译的 libghostty-vt 和 usql
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
