# Agent 资产执行与 MCP 接入

Koko 提供标准 MCP Streamable HTTP 服务，使 Agent 通过 JumpServer 操作授权资产。支持 SSH、SFTP、MySQL/MariaDB 和 PostgreSQL。Codex 使用原生远程 MCP；DSH（DeepSeek Harness）由后续独立插件适配同一服务端契约。

| 组件 | 职责 |
| --- | --- |
| Core | 用户登录、OAuth 与组织授权、资产权限、既有审批、操作登记与派发授权、集中审计 |
| Koko | MCP 与文件流接口、资产连接与执行、执行资源限制、证据持久化及向 Core 交付 |
| Lina | Core 的 Agent/MCP 配置页面 |
| docker-web / Nginx | 外部 HTTP/HTTPS 入口、TLS 终止与反向代理 |
| Codex / DSH 插件 | 浏览器授权、客户端令牌管理、工具调用、结果查询和审批链接呈现；按需实现 PC 文件传输 |

MCP 数据库执行使用 Go 原生驱动：MySQL/MariaDB 使用 `go-sql-driver/mysql`，PostgreSQL 使用 `pgx`。该路径不需要 `usql`、`mysql` 或 `psql` 命令行程序。`open_terminal` 打开的人工数据库终端使用现有终端实现，完整 Koko 部署须保留其 `usql` 等依赖。

## 首次部署

当前按单个 Koko 节点部署。客户端须能访问 MCP 地址和 Core 登录页面。多节点随机负载均衡、资产连接迁移和不确定操作的自动重放不在支持范围内。

1. 部署包含此功能的 Core、Koko 和 Lina，并执行 Core 数据库迁移。
2. 确认 Koko 已通过现有组件注册取得 Access Key，能访问 Core。
3. 在 Lina 的 **系统设置 > 功能设置 > Agent / MCP** 配置以下 Core 参数。
4. 配置 Koko 持久目录和 docker-web 转发，启动 Koko；在 Core 启用 MCP，确认资源元数据已就绪后开放客户端接入。

| Core 配置 | 要求 |
| --- | --- |
| `MCP_ENABLED` | 默认 `false`；统一启停 MCP 接入，Koko 最多每 60 秒同步一次 |
| `MCP_PUBLIC_URL` | 用户可达的完整 HTTP 或 HTTPS 地址，通常以 `/koko/mcp` 结尾；不含末尾斜线、查询参数或片段。Core 是唯一配置源 |
| `MCP_DCR_ENABLED` | 默认 `false`；开启后允许客户端自动登记，用户只需提供 MCP 地址即可进入登录授权流程 |
| `SITE_URL` | 在 **基本设置 > 网站地址** 填写客户端可访问的 Core 地址；Core 从中生成授权 origin，并同时用于 OAuth 元数据和组件下发 |

MCP 地址始终可编辑。启用 MCP 时地址不能为空，网站地址也必须有效；关闭 MCP 后可清空地址并保留自动注册偏好，自动注册仅在 MCP 启用时生效。设置页面使用现有配置权限和变更审计，Core 的授权与组件 API 说明见 Core 仓库的 `docs/agent-mcp.md`。
地址支持合法的 UTF-8 百分号转义，保留原始字符串作为 OAuth resource；Core 与 Koko 均拒绝无效转义、解码后的末尾斜线、控制字符和反斜杠。资源元数据地址按 URL 编码生成。

Koko 无需本地 MCP 配置。Core 通过现有组件接口下发 `MCP_ENABLED`、`MCP_PUBLIC_URL` 和由网站地址生成的 `MCP_AUTHORIZATION_SERVER`。节点容量使用内置保护值：128 个资产会话、20 个并发任务、64 个待处理任务；同一资产会话串行执行。

MCP 地址和网站地址描述客户端实际访问的外部入口，内部 `CORE_HOST` 仅用于组件通信。TLS 在 docker-web/Nginx 配置，代理可通过 HTTP 转发到 Koko 和 Core；Agent OAuth 的 issuer 和端点来自 Core 网站地址。Core/Koko 接受 HTTP/HTTPS 地址并执行身份、权限和 Origin 校验。客户端自身须支持部署所用协议；人脸摄像头等浏览器能力需要安全上下文，可由 docker-web 提供 HTTPS。

MCP 和文件流接口沿用 Koko 普通 Web 终端的 `ClientIP()` 来源 IP 解析，不单独配置可信代理。生产环境由 docker-web 统一入口清洗转发头，Koko HTTP 端口和 docker-web 内部端口仅允许可信内部网络访问；默认 `USE_LB=1` 时入口覆盖客户端传入的 `X-Forwarded-For`。显式设置 `USE_LB=0` 时，外层负载均衡必须清洗来源头，并限制客户端绕过它访问后端。该来源 IP 经组件鉴权的请求传递给 Core，用于资产登录 IP 规则和审计。

开发时可直接通过 Koko 的 `5000` 端口使用 MCP，无需 docker-web、HTTPS 或额外的开发开关。在 Core 将 `MCP_PUBLIC_URL` 配置为客户端实际可达的 `http://<Koko开发机>:5000/koko/mcp`，网站地址填写 Core 登录授权入口，再启用 MCP；同机客户端可使用 `127.0.0.1`。无转发头时使用 TCP 对端 IP。直连开发服务应限制在可信开发网络；沿用普通终端的解析方式意味着直接传入的转发头也会被解析。

反向代理须将 MCP 地址及其 `/files/upload`、`/files/download`、`/.well-known/oauth-protected-resource` 子路径转发到该 Koko 节点，保留 Authorization、Content-Type、MCP 协议头和文件长度。文件上传大小需允许 100 MiB，文件请求超时需允许五分钟。Core 的 `/.well-known/oauth-authorization-server`、`/core/auth/` 和现有 API 路由转发到 Core。客户端从 Koko 的 401 响应 `WWW-Authenticate` 获取资源元数据地址。

持久挂载 Koko 的 data 目录，保留 `data/agent/journal.db` 及同目录 `journal.key`，目录只允许进程用户访问。审计事件与内部 capability 加密保存；文件 artifact 可使用加密卷保护。打开已有 journal 时校验结构和密钥，校验失败则停止初始化，不改写记录或生成替代密钥。停用服务前先排空操作并确认审计交付，不能通过删除 journal 恢复故障。

## 配置动态更新

Koko 启动时即通过现有组件配置接口获取 MCP 开关和地址，此后每 60 秒同步，未启用时也持续同步。首次启用时创建执行引擎；有效更新整体切换 MCP 路由、元数据、授权地址和鉴权资源，保留已有执行引擎、资产连接、任务及审计存储。地址不能覆盖 Koko 的既有路由。

| 配置或状态变化 | 行为 |
| --- | --- |
| 拉取失败、字段缺失或地址非法 | 保留上次有效配置并重试 |
| Core 关闭 MCP | 停止接收新 MCP 请求，保留已接受操作、资产连接和审计维护；可随后清空地址 |
| 启动时未启用或没有有效配置 | MCP 等待配置，其他 Koko 服务正常运行；无历史 journal 时不创建持久目录 |
| 已有 journal 或初始化失败 | 即使 Core 禁用或暂不可达也尝试恢复历史审计；失败只停用 MCP，记录错误并重试 |
| 关闭 DCR | 仅停止新客户端登记，不清空地址、不撤销已有授权、不停止 MCP |

对外地址也是 OAuth 资源标识。更换地址前先收尾重要会话、暂关 DCR，再修改 Core 配置与 docker-web 转发；确认 Koko 元数据已更新后重新开启 DCR。客户端随后更新地址并重新登记、授权。已有令牌和资产会话不会改绑到新地址，新客户端也不能接管其他客户端的会话。网站地址变化也会更新授权 issuer 和对外链接，应同步客户端的登录授权信息。

轮询并非原子发布，网络异常时不保证一分钟内生效。地址更新期间，已接受操作继续遵守原有授权及超时规则，执行证据继续交付，不自动重放操作。

## 用户接入与操作流程

### 接入前准备

1. 向管理员取得 Core 中保存的完整 `MCP_PUBLIC_URL`，确认客户端能访问该地址，浏览器能访问 Core 网站地址。下文命令中的 `<MCP_PUBLIC_URL>` 须连同尖括号替换为实际地址，保留引号。
2. 首次自动登记客户端时，管理员须同时启用 `MCP_ENABLED` 和 `MCP_DCR_ENABLED`。关闭 DCR 后，已登记且授权有效的客户端仍可使用；新客户端不能自动登记。
3. 客户端选择 **Streamable HTTP / HTTP** 和浏览器 OAuth 授权。无需填写资产密码、client secret 或组织 header；组织在 JumpServer 授权页面选择。
4. 使用本机 CLI 时，在同一台电脑的浏览器完成授权。SSH、容器或远程开发环境中的客户端须另外确保浏览器回调能到达客户端监听端口。

### 客户端接入要求

当前配套 Core 的 DCR 仅接受主机为字面 IP `127.0.0.1` 或 `::1` 的 HTTP 回调，注册请求中的**每个**回调地址都必须满足要求；`localhost`、自定义 URI scheme 和外部 HTTPS 回调均不符合该注册契约。回调路径与查询参数固定，本机端口可变化；仅支持 public client、PKCE S256 和 `agent:access`，未启用 CIMD。

以下接入方式依据当前 Core 实现和 Codex 官方文档核对，尚未完成端到端联调。保存配置后，仍须完成浏览器授权和实际工具调用验证。

### Codex CLI / IDE

1. 在终端执行 `codex mcp add jumpserver --url "<MCP_PUBLIC_URL>"`，添加远程服务。
2. 执行 `codex mcp login jumpserver --scopes agent:access`，按浏览器提示登录 JumpServer、选择组织并确认授权。Codex 从 MCP 元数据发现 Core，自动登记 public OAuth 客户端及本机回调地址。
3. 执行 `codex mcp list` 确认配置已保存，再启动或重新打开 Codex 会话，通过 `/mcp` 检查服务与工具是否可用。
4. 按下述统一流程验证 `search_assets`。切换组织时先执行 `codex mcp logout jumpserver`，再重新登录授权。

配置默认保存在 `~/.codex/config.toml`；同一 Codex 主机的 CLI 与 IDE 扩展共享配置。参见 [Codex 官方 MCP 文档](https://learn.chatgpt.com/docs/extend/mcp)。

### 授权、验证与资产操作

首次验证时，让 Agent 使用 `jumpserver` 的 `search_assets` 查询本人获准访问的资产、账号和协议，暂不连接资产或执行命令。展开工具调用，确认结果来自该服务且没有认证或连接错误；成功返回空列表时，再核对当前授权组织及资产权限。

1. 在浏览器登录 JumpServer，选择可用组织并确认授权。只有一个组织时可自动选中，多个组织须明确选择。
2. Agent 通过 `initialize`、`tools/list` 发现工具，用 `search_assets` 查询获准访问的资产、账号和协议。
3. 调用 `connect_asset`，保存返回的 `session_id` 和连接 `action_id`，通过 `get_action` 查询连接或审批状态。
4. 遇到审批、MFA、原因填写或个人凭据要求，用户打开返回的 JumpServer 页面完成流程，再回到 Agent 查询或继续操作。Agent 的工具确认不能代替 Core 审批。
5. 连接就绪后提交命令、SQL 或 SFTP 操作，保存 `action_id` 并查询到终态。结束后调用 `disconnect_asset` 释放连接。

OAuth 使用 authorization code、PKCE S256、`agent:access` scope 和精确的 MCP resource。组织绑定到每次授权及后续刷新，不能通过 header 更改；`X-JMS-ORG` 无需发送，发送时必须与授权组织一致。所有 Agent 授权均须具备组织绑定。切换组织需要重新授权。

客户端保存并复用自动登记的 client ID，管理访问令牌刷新与撤销。资产密码、私钥和内部执行 capability 不返回 PC。客户端类型不改变资产权限、审批、幂等与审计规则。

DSH 插件后续实现同一套发现、登记、浏览器授权、令牌管理、工具调用和审批链接呈现。PC 大文件传输属于额外客户端能力，按下述文件流协议实现；原生 MCP 工具调用不会自动传输 PC 文件，也不能把本地路径交给远端 Koko 读取。DSH 插件的开发和发布独立于 Core/Koko。

### 常见接入问题

| 现象 | 排查步骤 |
| --- | --- |
| HTTP 503，提示等待配置 | 核对 Core 的 MCP 开关与地址、Koko 到 Core 的连通性和配置同步日志；配置通常每 60 秒同步，失败时会重试 |
| HTTP 404 或返回登录页 HTML | 核对完整 MCP 地址及代理路由，确认没有把 Core 登录地址或旧 SSE 地址填作 MCP 地址 |
| 首次 HTTP 401 | 属于 OAuth 发现入口，客户端应继续读取 `WWW-Authenticate` 指向的元数据并发起授权；持续 401 时核对令牌、精确 resource、授权组织及客户端是否被停用 |
| 未发现自动登记入口 | 确认 Core 同时启用 MCP 与 DCR，且授权服务器元数据路由可达 |
| `invalid_redirect_uri` | 检查注册提交的全部回调地址；`localhost` 与 `127.0.0.1` 在当前契约下不同，仅改回调端口或 MCP 地址不能解决客户端回调兼容性 |
| `invalid_scope` 或 `invalid_client_metadata` | 核对客户端是否使用 public client、`agent:access` 和符合要求的注册参数；自动申请额外 scope 或 client secret 的配置不符合当前契约 |
| TLS 错误或浏览器授权后无法回到客户端 | 确认客户端信任入口证书，并检查本机回调端口、浏览器所在电脑与客户端运行位置 |
| 配置已保存但 Agent 没有工具 | 在客户端重新加载服务，检查连接和授权状态，确认当前会话已启用该服务的工具，再验证 `search_assets` |

## 工具与执行语义

| 工具 | 行为 |
| --- | --- |
| `search_assets` | 分页查询授权资产、账号和协议 |
| `connect_asset` | 创建或复用显式资产会话，返回 `session_id` 和连接 `action_id` |
| `execute_command` | 提交 SSH 独立命令或数据库语句，返回 `action_id` |
| `get_action` | 查询权威状态、退出码/结果、分页证据及审计交付状态 |
| `cancel_action` | 请求取消；远端未确认停止时结果可能仍不确定 |
| `disconnect_asset` | 拒绝新操作，等待已接受操作排空后释放连接 |
| `list_files`、`stat_file`、`read_file`、`write_file` | SFTP 目录、元信息和小文本操作 |
| `make_directory`、`rename_file`、`remove_file` | 沿用上传/删除权限；目录删除仅支持空目录 |
| `upload_file`、`download_file` | 登记大文件操作，通过同一 MCP 资源下的 HTTP 接口传输字节 |
| `open_terminal` | 准备登录后的 JumpServer 人工终端入口 |

每次新操作使用新的 `idempotency_key`；重试同一请求时复用原键。相同键改变资产、命令、工作目录或文件摘要会被拒绝。Core 持久登记并原子授予派发资格，Koko 在执行前持久记录意图。HTTP 200、工具调用成功和操作已接受均不代表资产执行成功。

命令 ACL 的 `warning` 允许执行，并保留命中规则、风险信息及既有告警通知；`notify_and_warn` 要求用户确认，自动执行会被拒绝并提示使用 `open_terminal` 完成人工确认。取消待审批操作或关闭会话会收尾关联待办，仍供其他连接使用的共享审批工单保留。

操作状态包含 `queued`、`awaiting_approval`、`dispatching`、`running`、`awaiting_input`，终态包含 `succeeded`、`failed`、`cancelled`、`rejected`、`expired`、`unknown`。`unknown` 表示无法确认副作用，必须先核对资产状态，禁止自动重发。SSH 成功需要收到退出码 0，关闭连接不代表远端进程已停止。

`get_action.offset` 是最后读取的事件序号，默认从 0 开始，单页最多 32 条；按 `next_offset` 继续。`evidence_pending` 表示该页证据尚未齐全，应保留游标继续查询。`audit_status=pending` 表示证据已在 Koko 落盘、尚待 Core 确认。资产输出始终按不可信数据处理。

资产搜索每页最多 100 项，每个资产的账号也分页返回。指定 `asset_id` 后，将该资产的 `accounts_next_offset` 作为下一次请求的 `account_offset` 继续查询。

SSH 复用传输连接，每条命令开启独立 exec。需要工作目录或环境变量时显式传入 `cwd` 和 `env`；一次调用中的 `cd`、`export` 不影响下一次。包装 `cwd`/`env` 要求目标提供 POSIX Shell、`/bin/sh` 和 `/usr/bin/env`，不传时原样提交命令。数据库会话保持同一物理连接，支持事务和连接内状态，不跨会话共享；断开时尝试回滚未提交事务。取消或网络故障导致 `context_lost` 后须新建会话。

长时间交互、敏感输入和需持续人脸监控的操作使用 `open_terminal`。自动连接失败后的逻辑会话在有效期及授权内仍可人工交接；Core 重新验证权限，并通过新的交互令牌执行现有连接审批。创建真实终端会话后交接才成功，其后输入沿用原有命令 ACL、审计和录像，不共享独立 exec 的 Shell 状态。人脸监控就绪后才加载终端，用户须保持页面及摄像头开启。

资产会话加入 Koko 的在线心跳、权限复验和管理员任务管理。管理员关闭、权限到期或锁定会话会取消操作并关闭连接；解锁不会重建已失去的事务或进程上下文。PC/MCP 断开不会撤销已提交操作或取消已接受任务。

原生会话结束复用 Core 的既有会话结束流程，符合条件的资产继续执行已配置的会话结束改密策略。
空闲会话也复核 Agent 授权：维护循环每隔至少 5 秒轮转检查最多 16 个会话，单请求最多 1 秒、每批最多 5 秒。Core 明确返回授权失效或会话不存在时关闭连接，数据库尝试回滚未提交事务；临时网络或服务错误保留连接并重试，新操作仍须取得 Core 授权。批处理和网络耗时会影响全节点检查时间，不保证全部会话在 5 秒内关闭。

## 客户端文件流协议

以下路径追加在 `MCP_PUBLIC_URL` 后，认证与 MCP 请求一致。客户端使用 Authorization header，不把令牌放进 URL，也不自动跟随重定向转发令牌。

| 阶段 | 契约 |
| --- | --- |
| 上传登记 | 调用 `upload_file`，提交 `session_id`、`remote_path`、`size`、64 位小写十六进制 `sha256`、`idempotency_key` 和可选 `overwrite`（默认 false），取得 `action_id` |
| 上传字节 | 操作允许上传后，POST `/files/upload`，提供 `X-JumpServer-Action-ID`、`Content-Length`、`X-Content-SHA256`；大小和摘要须与登记值一致 |
| 上传结果 | 接口返回 Action JSON；继续查询 `get_action`。完整暂存并校验字节后才向资产派发写入 |
| 下载登记 | 调用 `download_file`，提交 `session_id`、`remote_path`、`idempotency_key`，查询到 `succeeded` |
| 下载字节 | GET `/files/download`，提供 `X-JumpServer-Action-ID`；响应为 `application/octet-stream`，包含 `Content-Length` 和 `X-Content-SHA256` |
| 本地完成 | 客户端对照响应头及 Action 的 `result.size`、`result.sha256` 校验实际字节，再发布本地文件；服务端成功不代表 PC 已保存 |

上传缺少固定长度返回 411，超限返回 413，操作未就绪或内容不匹配可返回 409。文件接口错误通常为 HTTP 文本，客户端须与 MCP JSON-RPC 错误分别处理。审批后用原参数、原幂等键重调 `upload_file`，确认 `queued` 后传输字节。网络中断时先查询原操作，不能自动重复 `unknown` 操作。暂存文件有保留期限，不支持 HTTP Range 断点续传。

本地目录限制、符号链接检查、下载原子发布及覆盖策略由客户端负责。文件通过字节流传输，不放进模型的 base64 参数；小文本使用 `read_file` / `write_file`。

## 资源、恢复与验证

- 默认 15 分钟无操作回收连接，执行期间不按空闲超时回收；Core 会话绝对有效期两小时。
- 单条命令默认 120 秒，最多 600 秒；审批及排队工作总上限 20 分钟，连接审批最多等待 10 分钟。
- 命令最多 4 KiB，输出最多 8 MiB；数据库结果最多 1,000 行，单语句网络读取 16 MiB，PG 单消息 8 MiB。
- 小文本最多 64 KiB，目录最多 200 项，同时受请求和证据预算限制。
- 文件单个最多 100 MiB；节点暂存最多 1 GiB、128 个文件，并发上传提前预留空间。
- journal 默认逻辑容量 256 MiB，为最终状态保留空间。Core 已确认的终态记录和 artifact 保留 24 小时后回收；未交付事件不自动丢弃。
- 单个操作还受编码后证据预算限制，包含小块输出的事件开销，并为最终状态预留容量。审计按操作轮转投递，同一操作保持顺序；某个操作持续失败时保留其证据并记录错误，其他操作仍可交付。
- 重启后不重连并重发命令。未派发操作过期，派发但未确认的操作标记 `unknown`；人工终端交接向 Core 对账。资产连接需显式重新建立。

集中审计页面位于 `/core/auth/agent/audit/sessions/` 和 `/core/auth/agent/audit/actions/`，按本人及现有组织审计权限访问。
返回的审计链接携带操作所属组织，页内导航保留组织参数；浏览器当前组织不同不影响获准访问的记录定位。

构建沿用项目流程，包括 libghostty-vt。基础回归运行 `GOMAXPROCS=2 GOMEMLIMIT=512MiB go test -race -p 1 ./internal/agentexec ./internal/assetexec ./pkg/httpd`。数据库及 Core 贯通测试仅在显式提供隔离 fixture 时执行。部署验收使用隔离资产与账户，覆盖浏览器登录、组织选择、资产连接、审批、结果与审计查询，以及反向代理和文件传输。
