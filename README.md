
# KoKo

**English** · [简体中文](./README_zh-CN.md)

KoKo is a connector of JumpServer for secure connections using character protocols, supporting SSH, Telnet, Kubernetes, SFTP and database protocols

Koko is implemented using Golang and Vue, and the name comes from a Dota hero [Kunkka](https://www.dota2.com.cn/hero/kunkka)。

## Features

### Protocol Support
- **SSH/SFTP**: Secure remote connections and file transfers
- **Telnet**: Legacy device remote management
- **Kubernetes**: Container and Pod management
- **Databases**: MySQL, PostgreSQL, Redis, MongoDB, Oracle, SQL Server, and more

### Core Capabilities
- **Web Terminal**: Browser-based terminal access, no client installation required
- **Web File Management**: Visual file browsing, upload, and download
- **Session Recording**: Automatic recording of all operations with playback for auditing
- **Command Auditing**: Record and filter user commands, block dangerous commands
- **Session Sharing**: Real-time multi-user collaboration with session monitoring
- **Connection Reuse**: Optimize connection performance, reduce authentication overhead

### Additional Features
- Multi-language support (Chinese/English)
- Multiple storage backends (Local/S3/OSS/Azure/ES, etc.)
- VSCode Remote SSH support
- Distributed deployment and high availability
- Flexible configuration options

> 📖 For detailed feature documentation, see [docs/功能说明.md](./docs/功能说明.md) (Chinese)


## Installation

1. Clone the project

```shell
git clone https://github.com/jumpserver/koko.git
```

2. Build the application

Build the application in the koko project.
```shell
make
```
> If the build is successful, the build folder will be automatically generated under the project, which contains compressed packages of various architectures of the current branch.

## Usage (for Linux amd64 server)

1. Copy the compressed package file to the corresponding server

```
Build the default compressed package through make, the file name is as follows:
koko-[branch name]-[commit]-linux-amd64.tar.gz
```

2. Unzip the compiled compressed package
```shell
tar xzvf koko-[branch name]-[commit]-linux-amd64.tar.gz
```

3. Create the file `config.yml`, refer to [config_example.yml](https://github.com/jumpserver/koko/blob/master/config_example.yml)
```shell
touch config.yml
```

4. run koko
```shell
cd koko-[branch name]-[commit]-linux-amd64

./koko
```


## Setup development environment

1. Run the backend server

```shell

$ cp config_example.yml config.yml # 1. Prepare the configuration file
$ vim config.yml  # 2. Modify the configuration file, edit the address and bootstrap key
CORE_HOST: http://127.0.0.1:8080
BOOTSTRAP_TOKEN: PleaseChangeMe <change to the same as core>

$ go run ./cmd/koko/ # 3. Run, running requires go if not, download and install from go.dev
```


2. Run the ui frontend

```shell
$ cd ui 
$ yarn install
$ npm run serve
```

## Docker
To build multi-platform images using Docker Buildx, you need to install Docker version 19.03 or higher and enable the Docker Buildx plugin.

```shell
make docker
```

## Acknowledgments
This project depends on [usql](https://github.com/xo/usql) for database connections. We appreciate their support.
