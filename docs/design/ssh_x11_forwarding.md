# Koko SSH X11 转发技术设计

## 1. 背景与目标

为 Koko 的原生 SSH 客户端接入链路增加 X11 转发能力。用户必须显式使用
OpenSSH 的 `-X` 或 `-Y` 发起请求，且只有目标资产所关联的平台扩展字段明确允许
时，Koko 才向目标 SSH 服务端申请 X11 转发。

目标范围：

- 支持 OpenSSH 兼容客户端，包括 Linux、macOS 和 Windows OpenSSH。
- 同时支持 `ssh -X`（非受信任）和 `ssh -Y`（受信任）。
- 支持交互式资产选择、SSH 直连资产、远程命令和无 PTY Shell。
- 支持 Koko 直连资产以及通过 JumpServer SSH 网关连接资产。
- 平台字段缺失、类型错误或值为 `false` 时默认禁止。
- 不审计 X11 图形内容，不把 X11 认证 cookie 写入日志。

非目标：

- 不负责客户端本机 X Server、`xauth` 或 `DISPLAY` 环境的安装和配置。
- 不为 PuTTY 等非 OpenSSH 客户端做专门适配。
- 不代理 Web 终端中的 X11。
- 不解析、录制或审计 X11 图形协议内容。
- 本次不修改 JumpServer Core，只约定 Core 下发字段。

## 2. Core 数据契约

目标资产的连接令牌中已经包含 `platform.meta`。Core 应在该映射中下发：

```json
{
  "platform": {
    "meta": {
      "x11_forwarding_enabled": true
    }
  }
}
```

约束：

- 字段名固定为 `x11_forwarding_enabled`。
- 仅 JSON 布尔值 `true` 表示允许。
- `false`、字段缺失、`null`、字符串 `"true"`、数字 `1` 等均表示禁止。
- 策略在最终选中目标资产后，使用该资产连接令牌中的平台信息进行判断。

该约定使旧 Core 与旧平台配置保持安全兼容：未下发字段时默认禁止。

## 3. SSH 协议流程

```text
OpenSSH 客户端              Koko                       网关（可选）          目标资产 sshd
      |                       |                              |                    |
      |-- session:x11-req --->|                              |                    |
      |<-- 暂存并确认请求 -----|                              |                    |
      |-- shell/exec -------->|                              |                    |
      |                       |-- 获取目标资产和平台策略 ---->|                    |
      |                       |-- 建立专用 SSH 连接 ----------------------------->|
      |                       |-- 原样发送 x11-req ------------------------------>|
      |                       |<================ x11 channel =====================|
      |<====================== x11 channel ================>|                    |
```

### 3.1 入站请求

OpenSSH 在 `shell` 或 `exec` 前，在 session channel 上发送 `x11-req`。请求载荷包含：

- `single_connection`
- `auth_protocol`
- `auth_cookie`
- `screen_number`

Koko 使用独立的 session context 保存请求，避免同一 SSH transport 上多个 session
channel 相互覆盖。

在交互式资产选择场景中，收到 `x11-req` 时尚不知道最终资产。客户端又会等待
`x11-req` 响应后才继续发送 `shell`，因此 Koko 必须先“暂存并确认”合法请求，
待资产选定后再执行平台策略：

- 允许：向目标资产发送请求并建立通道桥接。
- 禁止：不向目标资产发送请求，并在终端提示 X11 已被目标平台策略禁用。

暂存请求不会建立网络转发，也不会绕过平台策略。

### 3.2 `-X` 与 `-Y`

RFC 4254 的 `x11-req` 没有独立的 trusted/untrusted 标志。发起端 OpenSSH
进程通过本地 X11 SECURITY 扩展及其关联的认证状态实现 `-X`、`-Y` 的语义差异。

Koko 不生成或替换认证信息，而是把客户端提供的 auth protocol/cookie 原样重新
编码后发给目标资产，并把回程 channel 交还同一个 OpenSSH 客户端。目标程序连接
回来时，X11 初始化数据仍由原客户端校验和转换，因此两种模式的语义得以保留。

### 3.3 回程 X11 channel

目标资产的 sshd 接收 `x11-req` 后设置远端 `DISPLAY`，并在图形程序连接时向
Koko 打开类型为 `x11` 的 SSH channel。Koko 随后：

1. 使用原始 channel extra data，在入站 SSH 连接上向 OpenSSH 客户端打开 `x11`
   channel。
2. 接受目标资产的 channel。
3. 双向复制字节流，并正确传递半关闭和连接关闭。
4. 不解析或记录图形数据。

### 3.4 SSH 网关

网关仅承载 Koko 到目标资产的 SSH transport。`x11-req` 和回程 `x11` channel
都终止于最终目标 SSH client 对象，因此直连和经网关连接使用同一套桥接逻辑，
无需在网关上开放额外 TCP 端口。

## 4. 连接隔离

SSH 的回程 `x11` channel 不包含“由哪个 session 请求创建”的标识。如果多个用户
或多个入站连接复用同一个 Koko 到资产的 SSH transport，Koko 无法安全确定回程
channel 应交给哪个客户端。

因此，只要当前会话实际启用 X11：

- 禁止复用已有的目标 SSH client。
- 新建专用于当前来源会话的目标 SSH transport。
- 不把该 transport 放入 Koko SSH client 缓存。
- 来源会话结束后关闭该 transport。

未请求 X11 或平台禁止 X11 时，继续使用原有连接复用逻辑。

## 5. 错误处理

| 场景 | 行为 |
| --- | --- |
| 入站 `x11-req` 载荷无效 | 拒绝该请求，SSH 会话仍可继续 |
| 同一 session 重复请求 X11 | 拒绝后续请求 |
| 平台字段缺失或不为布尔 `true` | 不向资产转发，向用户显示禁用提示 |
| 目标 sshd 拒绝 `x11-req` | 向用户显示设置失败，Shell/命令仍继续 |
| 无法向原客户端打开 `x11` channel | 拒绝对应目标 channel 并关闭该图形连接 |
| 任一侧关闭 X11 channel | 结束双向复制并关闭另一侧 channel |
| 来源 SSH 连接结束 | 停止接收回程 channel，关闭专用目标连接 |

X11 设置失败不会把普通 SSH 登录变为失败，以保持 OpenSSH 的常见降级行为。

## 6. 安全与可观测性

- 默认拒绝：必须由平台扩展字段显式允许。
- 用户显式请求：未使用 `-X/-Y` 时不触发任何 X11 逻辑。
- 连接隔离：启用 X11 时禁止复用目标 SSH transport。
- Cookie 保密：日志中不输出 `auth_cookie`、完整请求载荷或 X11 数据。
- 内容不审计：不向录像、命令记录或文件审计管道发送 X11 字节流。
- 运维日志仅记录请求暂存、策略允许/拒绝、设置失败和通道错误。

`ssh -Y` 会把远端程序视为受信任 X11 客户端，安全风险高于 `ssh -X`。Koko 保持
用户的显式选择，不自动把 `-X` 提升为 `-Y`。

## 7. 目标环境前置条件

目标资产仍需满足 OpenSSH 自身的 X11 条件，例如：

- `sshd_config` 允许 `X11Forwarding yes`。
- 目标系统安装并可执行 `xauth`。
- 登录账号有权创建所需的 Xauthority 数据。

客户端需要自行启动本地 X Server 并准备 `xauth` 环境。Koko 容器或主机不需要
运行 X Server。

## 8. 代码改造映射

- `pkg/sshd/session_x11.go`
  - 截获 session channel 上的 `x11-req`。
  - 为每个 session 建立隔离 context。
- `pkg/sshx11/x11.go`
  - 定义并解析 RFC 4254 请求。
  - 严格解析平台扩展字段。
- `pkg/sshx11/forward.go`
  - 向目标会话发送 `x11-req`。
  - 把目标 `x11` channel 桥接回原始 OpenSSH 客户端。
- `pkg/proxy/server.go`
  - 覆盖交互式资产选择和 PTY 直连链路。
  - 启用 X11 时使用专用目标 SSH client。
- `pkg/handler/server_ssh.go`
  - 覆盖远程命令和无 PTY Shell 链路。
  - 启用 X11 时绕过连接复用。

## 9. 验收建议

至少验证以下组合：

1. 字段缺失、`false`、错误类型时，`ssh -X/-Y` 登录成功但资产上没有 `DISPLAY`。
2. 字段为 `true` 时，`ssh -X` 可运行 `xclock` 等测试程序。
3. 字段为 `true` 时，`ssh -Y` 可运行同一测试程序。
4. 不带 `-X/-Y` 时，资产上没有由 Koko 创建的 X11 转发。
5. 目标 sshd 设置 `X11Forwarding no` 时，普通 Shell 仍可使用且客户端收到失败提示。
6. 经 SSH 网关访问时，与直连资产行为一致。
7. 同时连接多个启用 X11 的会话时，图形流量不会跨客户端。
8. 检查 Koko 日志、命令审计和会话录像，确认没有 cookie 和 X11 内容。

示例客户端命令：

```bash
ssh -X '<JumpServer 直连用户名格式>'@<koko-host>
ssh -Y '<JumpServer 直连用户名格式>'@<koko-host>
```

登录目标资产后可先检查：

```bash
printf '%s\n' "$DISPLAY"
xauth list
```

## 10. 发布与回滚

建议发布顺序：

1. Core/API 保证连接令牌的 `platform.meta` 会包含该字段，但先保持默认 `false`。
2. 发布支持 X11 的 Koko。
3. 逐个平台开启字段，并按直连、网关、`-X`、`-Y` 的组合验收。
4. 确认日志和审计存储中没有认证 cookie 或 X11 内容后再扩大范围。

回滚不依赖降级 Koko：把 `x11_forwarding_enabled` 设置为 `false` 或删除字段即可立即
让后续新会话恢复默认拒绝。已有 X11 会话在其 SSH 会话关闭时终止。
