
# KoKo

**English** · [简体中文](./README_zh-CN.md)

KoKo is a connector of JumpServer for secure connections using character protocols, supporting SSH, Telnet, WinRM, Kubernetes, SFTP and database protocols

Koko is implemented using Golang, and the name comes from a Dota hero [Kunkka](https://www.dota2.com.cn/hero/kunkka)。

## Features


- SSH
- SFTP
- Web Terminal
- Web File Management

## Windows PowerShell over WinRM

Windows assets with an authorized, public `winrm` protocol can use Koko's Web CLI in Luna's web and desktop clients. Update JumpServer Core and Luna alongside Koko and apply the `assets.0031_winrm_terminal_access` migration to expose existing WinRM platform protocols.

Koko opens a persistent PowerShell runspace using PSRP over WinRM and password-based NTLM authentication. Configure the asset's WinRM listener port (usually 5985 for HTTP or 5986 for HTTPS) and the platform's `use_ssl` setting. NTLM message encryption is required on both transports. HTTPS verifies the target certificate by default; configure a trusted CA or explicitly enable `allow_invalid_cert` on the platform when needed. The account must have permission to use the Windows PowerShell remoting endpoint.

The terminal supports complete PowerShell commands or scripts on one line, line editing, command history, Ctrl-C cancellation and Ctrl-D disconnect. Variables and the working directory are shared with the AI command executor. AI Auto and PTY modes display commands and streamed output in the current terminal and session recording; Background mode returns output only to AI. All modes execute through PSRP and retain command auditing. Interactive host prompts and full-screen console programs are unsupported. Terminal commands are limited to 4095 characters; AI commands to 64 KiB and ten minutes. PSRP reports pipeline state, so AI results do not invent an OS exit code.

Submitted commands and bounded output use JumpServer's command auditing, ACL review and warning notification paths. AI commands matching a notify-and-warn ACL are refused. Execute and confirm such commands manually in the terminal; AI task approval does not replace that risk confirmation. Terminal activity uses the existing session recording and permission lifecycle. Authenticated WinRM requests are never automatically replayed after a transport failure; a lost response can leave the command outcome unknown.


## Setup development environment

1. Clone the project

```shell
git clone https://github.com/jumpserver/koko.git
cd koko
```

2. Run the backend server

`make run` requires Docker Compose to start guacd and stops it on exit.
Use `make dev` to run Koko without starting or stopping guacd; Docker Compose is not required.

```shell

$ cp config_example.yml config.yml # 1. Prepare the configuration file
$ vim config.yml  # 2. Modify the configuration file, edit the address and bootstrap key
CORE_HOST: http://127.0.0.1:8080
BOOTSTRAP_TOKEN: PleaseChangeMe <change to the same as core>

$ make run # 3. Run; prebuilt libghostty-vt and usql are downloaded on first use
```


## Docker
Use Docker Buildx to build the image and load it into the local Docker image store:

```shell
make docker
```

For local development and testing, export the bootstrap token used by JumpServer Core and start Koko with Docker Compose:

```shell
docker compose up --build
```

By default, Koko connects to `http://host.docker.internal:8080`, exposes SSH on port `2222`, HTTP on port `5050`, and the Web Proxy on port `5001`. Set `CORE_HOST`, `KOKO_SSH_PORT`, `KOKO_HTTP_PORT`, or `KOKO_WEB_PROXY_PORT` to override them. The Web Proxy accepts all target hosts after establishing a session with the existing Koko connect ticket and Core connection token. Unauthenticated HTTP and CONNECT requests receive 407; the deployment host allowlist has been removed. Established Web sessions use the shared Koko session registry for permission checks and administrative tasks.

## Acknowledgments
This project depends on [usql](https://github.com/xo/usql) for database connections. We appreciate their support.
