
# KoKo

**English** · [简体中文](./README_zh-CN.md)

KoKo is a connector of JumpServer for secure connections using character protocols, supporting SSH, Telnet, WinRM, Kubernetes, SFTP and database protocols

Koko is implemented using Golang, and the name comes from a Dota hero [Kunkka](https://www.dota2.com.cn/hero/kunkka)。

## Features


- SSH
- SFTP
- Web Terminal
- Web File Management
- [Agent MCP setup, common client configuration and troubleshooting (Chinese)](./docs/agent-mcp.md#用户接入与操作流程)

## Chen database workspace

Koko serves Luna's SQL workspace at `/koko/chen/api/v1/` using HTTP and SSE. Route this prefix to the Koko HTTP listener, disable proxy response buffering, and route each session back to its owning instance. The proxy must replace client-supplied `X-Forwarded-For`. Koko uses its own Core credentials for authorization, approvals and audit. Persist `CHEN_STATE_DIR` (default `data/chen`) per instance. When replacing an existing Chen server, drain its managed sessions and pending audits before stopping the old service.

### Local SQL workspace

Run `pnpm generate:chen` in Luna. Copy the [database configuration template](pkg/chen/standalone/standalone_backends_example.yaml) to `backends.yml`, run `chmod 600 backends.yml`, and fill in your test database connection details. Then start Koko.

```sh
koko --selector local-pg --selector local-mysql --backends ./backends.yml --web-dir /path/to/luna/.output/public
```

Each `--selector` enables one configured database. Open `http://127.0.0.1:8082/luna/chen/?selector=local-pg` and `http://127.0.0.1:8082/luna/chen/?selector=local-mysql` in separate tabs to create independent sessions. The default backend file is `standalone_backends.yml` in the current directory. Omit `--web-dir` for API-only testing at `/koko/chen/api/v1/`.

Local mode listens on loopback by default, uses Koko's database drivers and SQL session engine, and writes audit records to `data/chen-single/audit`. Any client that can reach this listener can use the selected database accounts without Core login or approvals. Starting `koko` without a selector uses Core as usual.

## Direct connections with personal credentials

Direct connection usernames support `platform_user#personal_credential_id#asset_ip_or_id` and `platform_user#protocol#personal_credential_id#asset_ip_or_id`, with `@` also supported as a separator. The asset must match uniquely. Koko uses the user's Bearer token from the current SSH login to retrieve credential metadata and create a connection token, preserving Core's asset account permissions and ACLs. If the Bearer token expires, establish a new SSH connection. Existing asset account usernames take precedence. Personal credential IDs are supported only in direct connection formats and are not added to the interactive asset menu.

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

`make run` and `make dev` both start KoKo and Kael locally by default. Set `KAEL_ENABLED=false` or `0` to disable Kael for either command.
`make run` also starts guacd using Docker Compose and stops it on exit. With `make dev`, start guacd separately if needed.

```shell

$ cp config_example.yml config.yml # 1. Prepare the configuration file
$ vim config.yml  # 2. Modify the configuration file, edit the address and bootstrap key
CORE_HOST: http://127.0.0.1:8080
BOOTSTRAP_TOKEN: PleaseChangeMe <change to the same as core>

$ make run # 3. Run; builds usql from source and sets up libghostty-vt on first use
```


`make build` produces `build/koko`, `build/kael` and `build/usql`. WebCLI still launches usql as a separate process. When running an unpacked release, put its directory first on `PATH`; `make dev` and the container already select the bundled binary.

[usql](tools/usql/README.md) is built from the KoKo root Go module. Its source was imported from [jumpserver-dev/usql v0.2.14](https://github.com/jumpserver-dev/usql/tree/3bb1da2711c04d84a3699aed75a209621e4eef86). Use `make usql` to build it alone and `CGO_ENABLED=0 go test ./tools/usql/...` to run its tests. Source-only changes ship with KoKo; rebuild the base image when module dependencies change. No separate usql release is needed.

WebCLI and WebGUI share the driver-independent [dbkit module](pkg/dbkit/README.md). Test it separately with `go -C pkg/dbkit test ./...`.

To run Kael locally, configure `cmd/kael/config.yml` from `cmd/kael/config_example.yml` and make the Codex CLI available on `PATH` or set `CODEX_BINARY`. Kael keeps its data in `cmd/kael/data/`. [Kael documentation](docs/kael/README.md)

Before committing, install the golangci-lint version pinned in
[the CI workflow](.github/workflows/golangci-lint.yml) and run `make lint`.
This prepares libghostty-vt and checks the same configuration and source as CI.

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

## License

Copyright (c) 2014-2026 FIT2CLOUD, All rights reserved.

Licensed under the GNU General Public License version 3 (GPLv3), consistent with the [JumpServer main repository](https://github.com/jumpserver/jumpserver). See [LICENSE](LICENSE) for the full license text.

## Acknowledgments
This project depends on [usql](https://github.com/xo/usql) for database connections. We appreciate their support.
