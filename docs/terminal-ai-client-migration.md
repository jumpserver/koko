# Terminal AI Client 迁移记录

## 关联提交

- Koko `perf_ai_agent` 合并 `new_terminal`：`f76f552b`
- Client `new_terminal` 完整迁移 Terminal AI：`03a15ca`

## 迁移结果

Koko 继续作为终端、SFTP、Terminal AI 和命令 ACL 的后端服务。旧 `ui` 目录及其实现保留，不删除；新的 Web 终端入口和交互 UI 由 `/opt/codes/client` 提供。

Client 现已支持 Koko 的二进制终端信封和 AI Chat 信封，并实现以下 Terminal AI 功能：

- 对话、执行计划和步骤状态
- 风险等级、审批与拒绝
- PTY 和后台执行模式
- 执行中断、输入锁和命令 ACL 结果
- 按终端 pane 隔离运行时状态，并跟随当前激活 pane
- 独立 `/luna/koko/connect/` Client 入口

文件管理、Kubernetes 和其他工作区能力继续以 Client 现有实现为准。

## 合并说明

`new_terminal` 合并到 `perf_ai_agent` 时保留了 Terminal AI 命令授权逻辑，同时接入新终端分支的连接票据、Web 路由、会话引用和页面参数处理。冲突集中在：

- `pkg/proxy/server.go`
- `ui/src/utils/config.ts`

冲突已按上述行为完成语义合并。

## 验证记录

验证使用 Koko `.env` 和 Client `.env.development` 中已配置的环境，未记录密钥。

- `go test -p 2 ./pkg/terminalai ./pkg/httpd ./pkg/proxy ./pkg/auth` 通过。
- `TERMINAL_AI_LIVE_TEST=1` 的真实模型协议检查通过，Tool Call 可用。
- Client 终端浏览器测试 7 项通过。
- Client `pnpm generate` 通过，共生成 31 个路由。
- Client 改动文件 ESLint 与 `git diff --check` 通过。
- 真实浏览器联调通过：Client 新入口连接开发 Core 提供的 SSH 资产，Terminal AI 面板完成一次不执行命令的模型对话往返，浏览器无控制台错误。
- Client 全量 `pnpm typecheck` 仍存在 `new_terminal` 基线中的既有类型错误；本次新增 Terminal AI 文件未出现类型检查错误。
