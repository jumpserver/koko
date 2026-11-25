# Kael 当前架构

本文描述当前仓库的实现。Kael 是注册到 JumpServer Core 的 Terminal component，提供统一 AI 会话、运行、审批和事件服务；Codex App Server 负责模型推理与工具循环，Luna 和 Platform Gateway 负责实际能力执行。

## 1. 组件与依赖

| 模块 | 当前职责 |
|---|---|
| [cmd/kael](../../cmd/kael/main.go) | 装配组件客户端、Harness、Store、身份适配器、Gateway、Service 和 HTTP 服务 |
| [internal/kael/component](../../internal/kael/component/client.go) | Core 组件注册、AccessKey、TerminalConfig、heartbeat、OpenAPI 与签名 Runtime Journal 通信 |
| [internal/kael/api](../../internal/kael/api/server.go) | `/kael/api/v1` 路由、身份入口、请求校验、HTTP 与 SSE |
| [internal/kael/identity](../../internal/kael/identity/identity.go) | Core 用户身份与权限查询、Origin 和 CSRF 校验 |
| [internal/kael/service](../../internal/kael/service/service.go) | Conversation、Message、Panel、Run、工具调用、审批、Artifact、审计与 worker 调度 |
| [internal/kael/runtime](../../internal/kael/runtime/harness.go) | 通过 stdio JSON-RPC 管理 Codex App Server、上下文、动态工具和回调 |
| [internal/kael/model](../../internal/kael/model/types.go) | 模型配置、消息、usage 和错误值类型 |
| [internal/kael/policy](../../internal/kael/policy/profiles.go) | Profile、工具风险、审批模式和 shell 参数级只读判定 |
| [internal/kael/platformgateway](../../internal/kael/platformgateway/gateway.go) | Core OpenAPI Registry、Operation 筛选、请求构建、用户凭据转发与结果脱敏 |
| [internal/kael/ports](../../internal/kael/ports/store.go) | Store/Tx 和 CapabilityProvider 接口 |
| [internal/kael/store](../../internal/kael/store/core.go) | 内存事务、Core 历史 Journal、Terminal 本地 JSONL 与保留策略 |
| [internal/kael/event](../../internal/kael/event/bus.go) | DomainEvent、PanelDelivery 投影及提交后的订阅通知 |
| [internal/kael/domain](../../internal/kael/domain/types.go) | 领域对象、协议版本和大小限制 |
| [internal/kael/logger](../../internal/kael/logger/logger.go) | stdout 与轮转文件日志 |

模型执行只依赖 Harness 的通用输入与回调。Platform 业务通过 `CapabilityProvider` 隔离；Runtime 不直接调用 Core 业务 API，也不解析 MCP 或持有 SSH、SFTP、数据库连接凭据。

两条能力执行路径：

- `panel`：Kael → 原 PanelSession 的 SSE → Luna 本地 Registration 路由 → Koko、Chen 或本地执行器；结果通过 HTTP 回传。
- `service`：Kael → Platform Gateway → Core API；Panel 接收过程、结果与审批事件，不执行这次 Core 请求。

Lina 使用 Kael 的原生资源和 PanelDelivery。Luna 同时承载普通对话及 workspace、terminal、file、sql、script 等能力对话，负责采集 Context、注册工具、关联真实会话与渲染结果。实际资源权限和执行审计仍由执行组件负责。

## 2. 领域对象与一次 Run

| 对象 | 含义与生命周期 |
|---|---|
| Principal | 经 Core 验证的用户、组织及权限事实；客户端字段不能自证身份 |
| Conversation / Message | 对话及用户问题、回答、附件引用、结果卡片；与浏览器 Tab 分离 |
| Artifact | 附件元数据、摘要和有界提取文本；原始字节独立存储 |
| PanelSession | 某个客户端与一个 Conversation 的临时绑定，包含 lease、resume token、审批模式和独立 cursor |
| ContextSnapshot | Panel 的版本化上下文，包含 digest、domain、surface 和有界数据 |
| Registration | 能力定义、schema、注解、版本、digest、namespace、执行绑定和 lease |
| Run | 一次问题的编排，固定发起 Panel、Context、Registration 与策略快照 |
| ModelCall | 一次完整 Harness turn 的记录 |
| ToolCall / ToolResult | 一次准确绑定的能力调用及带 sequence、done、status 的回执 |
| Approval | 对原 ToolCall 和参数摘要的一次审批，默认有效期 10 分钟 |
| DomainEvent / PanelDelivery | Conversation 内领域事实与针对某个 Panel 的投递；两者序列独立 |
| AuditRecord | 身份、会话、Run、工具与审批操作的审计关联 |

一个 Conversation 可被同一用户、组织的多个 Panel 显式打开。每个 Panel 只绑定一个 Conversation；新 Panel 不继承旧 Panel 的工具、cursor 或未完成调用。Panel 和 Registration 默认租约均为 2 分钟，客户端通过 heartbeat 续租。

一次请求依次执行：

1. 创建或打开 Conversation，为当前客户端创建 PanelSession。
2. 提交 Context；能力对话原子替换 Registration。
3. 创建用户 Message，再用 Conversation、Message、Panel ID 创建 Run。
4. Kael 固定 Context version/digest、Registry revision、工具定义、Profile 及授权快照，写入 `queued` 并通知 worker。
5. Worker 领取 Run，创建输出 Message，由 Harness 提交模型输入。文本增量经 Service 合并后写入状态与事件。
6. 动态工具调用经 schema、绑定、风险及审批校验后执行，结果回到同一 Harness turn。
7. 保存最终回答、usage、结果卡片或失败说明，提交终态事件。

同一 Conversation 同时只允许一个非终态 Run。Run 的执行态包括 `queued`、`running`、`waiting_capability`、`waiting_approval` 和 `cancelling`；终态包括 `completed`、`failed`、`cancelled`、`interrupted`。当前只接受前台执行；请求后台 Run 返回 `background_requires_durable_store`。

Message、Run 和结果提交各有幂等校验：同一个幂等键或回执序列不能对应不同内容。用户消息可携带 `text`、`artifact` 和 `data` Parts；branch/regenerate 使用已有消息及其附件、上下文关系，不以当前页面内容悄悄替换原问题。

## 3. Codex Harness

### 引擎与模型配置

Codex App Server 是唯一推理引擎，通过子进程 stdio 双向 JSON-RPC 接入，不开放 Codex 网络监听。Kael 启动时验证 `CODEX_BINARY` 可执行，版本不低于 `codex-cli 0.153.2`；这是最低版本要求。

Core TerminalConfig 的 `CHAT_AI_*` 是模型配置与凭据来源。每次 Run 执行前读取配置，已开始的 turn 不热切模型。Chat AI 关闭或模型未配置不阻止 Kael 启动，但会拒绝执行 Run；Core 保存有效配置后可再次发起请求。

模型端点必须支持 Responses API。Harness 不提供 Chat Completions 路径，明确拒绝 DeepSeek Provider 配置；请求和流式自动重试均关闭。模型密钥只传入子进程环境，不写入命令行、配置文件或客户端。

`bootstrap` 返回 `agent_engine=codex`、`agent_protocol_version=1`。`model.requested` / `model.completed` 的 `scope=agent_turn` 表示完整 turn，耗时包含工具和审批等待。ModelCall 与 ModelRequestCount 记录 turn，不能解释为 Codex 内部模型请求数；usage 使用 Codex 累积计数减去本 turn 起点。

### 进程隔离与工具

每个缓存会话使用私有 HOME、CODEX_HOME 和空工作目录，不继承用户登录、插件、MCP 配置和应用 Secret。线程使用 `ephemeral=true`、`environments=[]`，禁用 shell、unified exec、Code Mode、浏览器、computer use、联网搜索、hooks、apps 和 subagents 等能力。

业务工具以 `kael_` 安全别名暴露为 dynamic tools。Kael 校验 thread、turn、Registration 与输入输出 schema，再进入业务审批和执行通道。相同 callId 的相同重投复用回执，修改参数则失败；同一 turn 内相同写操作不自动重复执行。Service 调用使用执行端解析的实际 Operation 风险，允许重复只读查询；Core 写入的去重摘要不包含 `progress`、`action` 等展示文案。成功的 final-result 工具之后拒绝后续业务工具并要求模型总结。

未集成的问询表单返回空答案，提示模型在普通对话中提问；未知 host request 拒绝执行。子进程 stderr 不直接进入业务错误或日志。

### 上下文与会话复用

同一用户、组织、Conversation、Panel，模型配置、Profile 指令、工具注册未变且历史仍为追加关系时，复用进程内 Codex thread，只提交新增历史和本轮 Context。历史变化、能力变更、模型配置变化或上次执行失败会使缓存失效；新线程从 Kael 的业务历史构建输入。

Context 是不可信数据，不构成权限或指令。`response_language` 只接受 `zh`、`zh_hant`、`en`、`fr`、`ja`、`pt_br`、`es`、`ru`、`ko`、`vi`，映射为固定语言名称后附于本轮输入。用户明确指定语言时优先遵循；字段缺失或无效时跟随最新问题，无法判断时使用英语。语言偏好覆盖工具调用前说明、进度、工具参数中的展示文案、提问及最终回答，API 描述与工具输出不改变该偏好。语言变化不改变 thread 复用签名，当前 Run 仍使用已冻结快照。

输入上限为 4 MiB，超限明确报错，不按固定历史条数静默裁剪；上下文压缩由 Codex 负责。同一 turn 最多处理 128 个动态工具请求。最多缓存 16 个 Panel 进程，空闲超过 5 分钟回收；容量满时优先回收空闲进程。

取消或失败会关闭对应进程，不自动续跑或重放工具。Codex thread 是执行缓存，Kael 业务历史仍是权威数据；进程回收后不保留其内部推理和压缩状态。

## 4. Profile、Registration 与审批

### 当前 Profile

| Profile | Conversation kind | 能力范围 |
|---|---|---|
| `general` | `general` | 产品问答及授权 Core Operation，使用 service binding |
| `platform.management` | `general` | 管理员可用的 Core 管理操作，写操作审批 |
| `platform.asset` | `general` | 资产、节点、平台等只读操作 |
| `platform.session_audit` | `general` | 会话、命令、登录、访问、操作及工单审计只读操作 |
| `platform.ops` | `general` | 作业、任务、组件与终端健康只读操作 |
| `workspace` | `capability` | Luna 工作区、授权资产连接及委派终端任务 |
| `terminal` | `capability` | 已连接资源的终端能力 |
| `file` | `capability` | 已连接文件会话能力 |
| `sql` | `capability` | SQL 编辑器上下文和草稿 proposal |
| `script` | `capability` | 脚本编辑器上下文和草稿 proposal |

Profile 发现会校验管理员标志及所需权限。`management`、`asset`、`session_audit`、`ops` 可解析为对应的 `platform.*` Profile。Profile 提供指令与能力范围，真实可执行工具仍以本次 Registration 为准。

### Panel 工具

Kael 接受经 Core 登录认证的客户端提交工具定义。工具名称不依赖内置目录；namespace 由 Profile 派生，风险由注解与显式声明合并，缺省注解按写操作处理。`read_only` 得到 read，`destructive` 或非只读 `open_world` 得到 dangerous；显式风险只能提高注册风险，最终校验 Profile 风险上限。

注册替换携带 `base_registry_revision`。全部定义通过名称、数量、schema、注解和风险校验后，事务内替换 Registry，分配新 ID、digest、revision 和 lease；任一失败保留上一完整版本。

每次执行校验 Run、原 Panel、Registration ID、Registry revision、定义 digest、状态和 lease。工具只能回到原执行 Panel，不能按用户、工具名、当前焦点或最近活跃 Tab 猜测执行器。客户端注册与回执不提供额外的组件签名证明；Core/Koko/Chen 的资源权限、ACL 和会话校验仍是执行边界。

Panel 审批模式为 `auto`、`always`、`never`：auto 依据调用策略，always 每次审批，never 跳过 Panel 工具审批。声明 `shell-readonly-v1` 的终端工具可通过 shell 语法树做参数级只读判定；不支持的语法、写入或无法确认的命令仍按原策略处理，细节见[命令执行生命周期](./command-execution-lifecycle.md)。

审批绑定原用户、组织、Run、ToolCall、Registration 版本及参数 digest，决定和实际派发分别复验，派发时消费批准状态。支持为声明 shell 命令策略的注册记住批准，复用范围限定为同一 Panel、Registration、定义版本和参数摘要。审批决定、模式变化和执行结果进入审计；拒绝审批作为工具反馈交给 Harness，不代表已执行或必然终止整个 Run。

### Platform Gateway

Gateway 在 Kael 启动后后台加载 Core OpenAPI，失败时每 5 秒重试，首次成功后停止自动请求。Registry 未首次加载完成时，依赖 Core API 的 Run 立即返回能力未就绪，不会触发同步抓取。管理员仍可显式刷新；Registry 按内容 hash 版本化并最多保留四个版本。Run 固定自己的注册版本；模型通过搜索取得候选 Operation，再提交 operation ID 及 path/query/body 参数。

Method 和 URL 由可信 Registry 构建。默认允许 `GET/POST/PUT/PATCH`，`DELETE` 需配置显式启用。`general` 使用源码内固定 Operation 范围，asset/audit/ops 进一步收窄，management 为管理员提供较宽范围；Kael 不读取 Core 自定义 Operation allowlist 配置。

搜索和调用使用相同权限筛选：读取 `x-jms-required-permissions`、`x-jms-permission-dynamic`，缺失、非法或 dynamic 元数据均拒绝，Principal 必须具备全部静态权限。Run 保留创建时权限快照用于一致选择，Core 对用户凭据认证的业务请求仍执行实时 RBAC。

Gateway 解析引用、移除请求 schema 的 `readOnly` 字段并规范化 required/nullable，验证参数及 query 序列化，拒绝敏感路径与字段。参数错误可作为结构化结果返回模型修正。

业务请求沿用发起 Run 的用户 Cookie（或已有 Authorization），Cookie 写请求同时携带 CSRF token；组织头来自已验证的 Principal。凭据只按 Run 保存在当前进程内存中，不进入 Journal、工具参数、模型输入或审计；创建、重新生成及显式恢复 Run 时从已认证请求绑定，运行结束、取消或服务关闭后清理。Gateway 不再需要平台委托共享密钥，Core 使用现有用户认证、CSRF 和 RBAC 校验。

Service 写操作必须经过独立 Approval，不受 Panel 的 never 模式豁免；执行前重新校验请求和原审批绑定。HTTP 默认超时 15 秒、响应上限 1 MiB，结果限长、脱敏后写入 ToolResult、结果卡片及审计。凭据缺失、CSRF 失败、连接失败与超时返回独立错误码，并记录脱敏诊断。Gateway 不继承进程代理、不跟随重定向，支持私有 CA 与客户端证书。

## 5. HTTP 与事件协议

唯一业务根路径为 `/kael/api/v1`。Kael 原生处理 `/kael` 前缀，canonical 路径不带尾斜杠，不启用路径纠正或尾斜杠重定向。浏览器通过同源代理访问，代理保留该前缀。

下表路径均相对于 `/kael/api/v1`；完整路由和请求校验见 [server.go](../../internal/kael/api/server.go)。

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/bootstrap`、`/assistants`、`/runtime-profiles` | 协议、功能、限制和可用 Profile |
| GET / POST | `/conversations` | 列表、创建 |
| GET / PATCH / DELETE | `/conversations/{id}` | 详情、修改、软删除 |
| GET / POST | `/conversations/{id}/messages` | 历史、创建用户消息 |
| GET | `/conversations/{id}/runs`、`/conversations/{id}/approvals` | 运行与审批记录 |
| POST | `/conversations/{id}/branches`、`/messages/{id}/regenerations` | 分支、重新生成 |
| POST | `/artifacts` | 上传 |
| GET / DELETE | `/artifacts/{id}` | 元数据、删除 |
| GET | `/artifacts/{id}/content` | 鉴权读取原始内容 |
| POST | `/panel-sessions` | 创建 Panel |
| POST | `/panel-sessions/{id}/heartbeat`、`/panel-sessions/{id}/resume` | 续租、恢复原 Panel |
| PATCH | `/panel-sessions/{id}/approval-mode` | 更新审批模式 |
| DELETE | `/panel-sessions/{id}` | 关闭 Panel |
| PUT | `/panel-sessions/{id}/context`、`/panel-sessions/{id}/registrations` | 版本化上下文、原子替换工具 |
| GET | `/panel-sessions/{id}/events` | SSE 或 `once=true` 单次读取 |
| DELETE | `/registrations/{id}` | 撤销能力 |
| POST | `/runs` | 创建 Run |
| GET | `/runs/{id}` | 运行详情 |
| POST | `/runs/{id}/cancel`、`/runs/{id}/resume` | 取消、受限恢复 |
| POST | `/tool-calls/{id}/results` | 提交工具回执 |
| GET | `/approvals/{id}` | 审批详情 |
| POST | `/approvals/{id}/decisions` | 批准或拒绝 |
| POST | `/admin/platform-registry/refresh` | 刷新 Registry |
| GET | `/admin/stats` | 管理统计 |
| POST | `/transcriptions` | 服务端语音转写的禁用占位，返回 unavailable |

HTTP 命令与事件订阅分离，创建 Message/Run 不直接建立响应流。JSON 请求拒绝未知字段、多个 JSON 值和超限内容。错误返回安全 code/detail 与 retryable，不把内部堆栈交给客户端。

DomainEvent 与状态在同一 Store 事务中提交；Event Projector 为指定 Panel 分配独立递增 sequence，生成 PanelDelivery，事务成功后 Bus 才通知订阅者。工具、完整结果、文本增量和审批投递给发起 Panel；可共享的终态消息和脱敏 Run 状态投影给显式打开同一 Conversation 的有效 Panel。

SSE 的 `data` 是完整 PanelDelivery，`event` 为 `delivery.type` 的 dot 名称，`id` 为该 Panel 的十进制 sequence。主要事件包括 `message.delta`、`message.completed`、`run.*`、`model.*`、`tool.call`、`tool.progress`、`tool.completed`、`tool.failed`、`tool.cancel`、`approval.required`、`approval.resolved`。

重连通过 `after` 或 `Last-Event-ID` 续传；同时传入时必须一致。客户端按 Panel ID 和 sequence 去重，不能将 Conversation 的 DomainEvent seq 当作 SSE cursor。建连时 cursor 过期返回 `410 cursor_expired`；已连接流遇到读取失败或 cursor 失效会关闭，由客户端重新查询状态。heartbeat 每 15 秒发送无 ID 的 SSE comment。

SSE 断开只结束订阅，取消 Run 必须显式调用 cancel；Panel 关闭、租约和工具可用性另行决定执行状态。Delivery 默认保留 24 小时，新 Panel 从自己的序列开始。代理需关闭响应缓冲和缓存，并允许长连接。

## 6. 持久化与恢复

### 固定存储路由

Service 依赖 `ports.Store` / `ports.Tx`，生产装配使用带 split persistence 的 Memory Store。存储由 Conversation 的 Profile 决定，不提供部署模式切换：

| 数据 | 存储位置与恢复范围 |
|---|---|
| 非 `terminal` Profile 的用户可见历史 | Core Runtime Journal；保存已有用户消息的 Conversation、用户 Message、终态 Assistant Message、结果卡片和关联 Artifact 元数据 |
| 非 `terminal` 的 Run、Panel、Context、Registration、Model/Tool、Approval、Event/Delivery、运行审计 | 当前进程内；不随 Core 历史恢复 |
| `terminal` Profile 的完整状态 | `data/terminal/store/runtime.jsonl`，含消息、运行、事件、审批和审计 |
| Terminal 可读领域事件 | `data/terminal/events/<conversation-id>.jsonl`，按 Conversation seq 归档 |
| Artifact 原始字节 | `data/artifacts` 私有目录；不上传 Core Journal |
| 组件 AccessKey | `data/keys/.access_key` |
| Codex 执行缓存 | `data/harness/instance-*`，正常关闭时删除本实例目录 |

空 Conversation、未关联消息的 Artifact 和流式 Assistant 内容不进入 Core 历史投影。终态 Assistant 的正文、失败摘要、结果卡片一次提交；纯运行态变更只提交进程内状态。更换节点后恢复附件原文仍需复用 Artifact 卷。

### Core Journal

组件 AccessKey 签名访问 `/api/v1/chat-ai/runtime-store/`。Core 保存 opaque journalRecord，含版本、时间、base64 payload 和 SHA-256 checksum；Kael 解析其中的 Go gob snapshot/delta。

加载使用 `after`、`limit` 和一次性 nonce 分页，验证整页 HMAC receipt、记录顺序与 revision 后重放。提交绑定 `commit_id`、`expected_revision`、snapshot 标志、record 和 HMAC integrity；Core 按 commit ID 幂等，以 revision CAS 追加，Kael 校验返回的 revision、commit ID 和 receipt。

网络错误和 5xx 最多尝试三次，每次使用同一提交请求。结果仍不确定、回执校验失败或 revision 冲突时，adapter 标记不可用，readiness 与后续写失败，需要重启并从 Journal 重放，不能猜测提交结果。单条历史 record 上限 8 MiB，超过上限直接拒绝，不使 adapter 进入不可用状态。

正常运行持续追加历史 delta，不周期性生成 snapshot 或清除早期记录，启动重放开销随历史增长。载入包含完整运行态的旧版 Kael Journal 时，现有加载代码会分离 Terminal 数据并写入精简历史 snapshot；该路径不导入旧 Platform ORM 或 Koko 历史。

所有 Kael component account 使用同一个 `default` store，当前只支持一个活动写入者。CAS 提供冲突检测，不提供多实例状态同步或分布式 Run ownership。Core 故障时普通历史不会自动改写本地。

Core Journal 无按 user/org 的物理 purge、级联删除或 retention。Conversation DELETE 是软删除，相关 Message/Artifact 历史并未因此物理清除。

### Terminal JSONL 与重启

Terminal Journal 为带版本、checksum 的事务记录，启动时仅截断末尾不完整记录，按阈值原子压缩快照。状态持久化成功后才对进程内读者和订阅者可见。

重启恢复 Terminal 历史后，未完成 Run 收敛为 `interrupted/process_restarted`，相关未完成消息和工具取消，未决 Approval 过期，Panel/Registration 过期。客户端必须建立新的执行绑定；持久化记录不等于恢复浏览器连接。

`Run resume` 仅允许受限的 interrupted Run。已经开始模型执行或产生 ToolCall 的运行返回 `execution_rebind_required`，Panel 能力还需原绑定有效；不会自动重新执行结果未知的操作。

Terminal 本地历史默认保留 7 天、容量上限 1 GiB、磁盘最低余量 1 GiB。启动及每小时清理过期历史；容量达到 90% 或余量不足时，每分钟最多淘汰 100 个最旧非活跃会话，目标降至 80%，跳过活动 Run 和有效 Panel。

配额统计 `data/terminal/store/` 与 `events/`，包括临时快照文件，不含 Artifact 原文和日志。写入前检查追加、快照空间和磁盘余量，新请求额外预留配额的 10%（最多 64 MiB）给在途执行；容量预检失败返回可重试的 `storage_capacity_exceeded`，实际 I/O 失败则关闭后续写入。清理先提交保留状态的快照，再删除对应事件归档。

## 7. 身份、配置与运行限制

### 身份和部署入口

KoKo 镜像包含 `/opt/kael/kael` 和 Codex CLI。s6 默认启动 KoKo 和 Kael，两个进程共享容器的启动用户（默认为 root）。设置 `KAEL_ENABLED=false` 或 `0` 可关闭 Kael。

业务请求要求 `X-JMS-ORG`。Kael 使用请求 Cookie/Bearer 向 Core 的 profile 与 permissions 接口验证用户，每次请求重新取得权限；除 superuser 外要求 `chat_ai.use_chatai`。带 Authorization 时仅使用该头认证，不回退到 Cookie；鉴权请求不跟随重定向。会话及关联资源按用户、组织校验所有权，管理接口另行校验管理员权限。

Origin 校验默认关闭；只有 `ALLOWED_ORIGINS` 包含非空值时启用，允许精确列表或当前同源 Origin，不发送 CORS 响应头。Cookie 写请求另行校验 CSRF。网关终止 HTTPS 时可配置外部 Origin；`TRUST_FORWARDED_HEADERS` 默认关闭，仅在可信网关覆盖 forwarded headers 且 Kael 端口不直接暴露时使用。

Kael 不直接连接业务数据库。首次通过 BootstrapToken 注册 `kael` 组件，后续使用私有 AccessKey 文件。Platform Gateway 是必需依赖：`PLATFORM_GATEWAY_ENABLED` 必须为 true，组件签名必须可访问 Core OpenAPI；不再配置 `PLATFORM_DELEGATION_KEY` 等委托参数。Registry 初始化失败会在后台重试，不阻止 Kael 监听端口和基础健康检查；依赖 Core API 的能力在加载成功前不可用。

### 配置与启动

配置使用平铺大写 YAML 键及环境变量；从当前目录依次选择 `config.yml`、`config.yaml`、`.config.yml`、`.config.yaml`，也可通过 `-f` 或 `KAEL_CONFIG_FILE` 指定。模型凭据由 Core 管理。配置清单和默认值见 [config_example.yml](../../cmd/kael/config_example.yml)，有效校验见 [config.go](../../internal/kael/config/config.go)。

默认监听 `0.0.0.0:8083`；`cluster_id` 为 `kael`，`instance_id` 使用组件 NAME。数据路径从进程工作目录的 `data` 派生。日志同时写 stdout 与 `data/logs/kael.log`，按 50 MB 轮转，保留 7 份、7 天。

### 已实现限制与观测

| 项目 | 当前值 |
|---|---|
| Worker / 排队 Run 上限 | 4 / 64 |
| 完整 Run 超时 | 30 分钟 |
| Panel 工具回执超时 | 默认 45 秒，`TOOL_RESULT_TIMEOUT` 可配置为 1 秒至 10 分钟 |
| Context / Harness 输入 | 4 MiB |
| 单条消息 / 单个工具 schema | 各 64 KiB |
| Registration / 单 turn 工具请求 | 64 / 128 |
| 工具参数 / 结果 / Event payload | 128 KiB / 128 KiB / 256 KiB |
| 单 Artifact / 提取文本 / 图片像素 | 20 MiB / 40 KiB / 4000 万像素 |
| Panel / Registration lease | 默认 2 分钟 |
| Approval / Delivery 保留 | 默认 10 分钟 / 24 小时 |

`bootstrap.features` 启用 Conversation、Panel/service capability、Platform Gateway、Artifact、branch、regenerate 与 SSE replay；background、transcription、web_search、notifications 为 false。

运维端点为 `/kael/health/live`、`/kael/health/startup`、`/kael/health/ready`、`/kael/internal/metrics` 和 `/kael/openapi.json`。ready 在 2 秒内检查 Store、签名 Core head 与本地 Terminal Journal，不探测模型端点或 worker 工作情况。

metrics 包含 `kael_runtime_store_snapshot_disabled`、`kael_runtime_store_revision`、`kael_runtime_store_records_since_snapshot`。运维端点不经过业务用户认证，部署通过代理或网络访问控制限制暴露。统计与运行审计受上述持久化范围约束，不能作为跨重启完整运行轨迹。

### Core 会话审计

会话审计由 Core 的 `/api/v1/chat-ai/audit/conversations/` 提供列表、搜索和详情。
Kael 在 Runtime Store 的签名 journal envelope 中附带 version=1 的 audit JSON 数据，
Core 将它与 journal 在同一事务提交。普通会话和终端 AI 的问题及最终回复均被审计；
运行时清理不删除 Core 审计历史。启动时按稳定 ID 分批补录仍保留的历史。
部署先升级 Core 并执行审计表迁移，再升级 Kael，最后更新 Lina。
