
# KoKo

**English** · [简体中文](./README_zh-CN.md)

KoKo is a connector of JumpServer for secure connections using character protocols, supporting SSH, Telnet, Kubernetes, SFTP and database protocols

Koko is implemented using Golang, and the name comes from a Dota hero [Kunkka](https://www.dota2.com.cn/hero/kunkka)。

## Features


- SSH
- SFTP
- Web Terminal
- Web File Management


## Setup development environment

1. Clone the project

```shell
git clone https://github.com/jumpserver/koko.git
cd koko
```

2. Run the backend server

`make run` requires Docker Compose to start guacd and stops it on exit.

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
