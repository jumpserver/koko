# 命令执行生命周期

本文描述 Kael 对 Panel 工具的调用、回执、超时和取消处理，以及终端命令接入时的边界。核心实现见 [execution.go](../../internal/kael/service/execution.go)、[run.go](../../internal/kael/service/run.go)、[command.go](../../internal/kael/policy/command.go) 和 [Harness](../../internal/kael/runtime/harness.go)。整体模型见[当前架构](./ARCHITECTURE.md)。

## 1. 所有权与状态

Kael 管理 Run、ToolCall、审批和工具回执。Luna 根据 Registration 将调用交给原资源连接的执行器，并关联命令进度；连接、远端命令和真实停止结果由执行器管理。Kael 不持有远端进程句柄，也不通过工具名猜测连接或重定向调用。

ToolCall 完成表示本次 RPC 已返回，不必表示远端命令结束。异步执行器可以在结果中返回 `execution_id`、原 `tool_call_id`、命令 `status`、有界输出及停止确认信息；外层 ToolResult 使用 `done=true` 结束本次调用，Harness 随后继续推理。

查询、等待或取消已有命令也是独立注册能力。只有本次 Registration 提供相应工具时，模型才能调用；命令预算、等待时长与停止确认由执行器契约决定，Kael 不把它们解释为自己的 Run 或回执期限。

当前 Harness 内置指令按以下接入契约引导模型：长命令在两秒内返回 execution ID，使用 `get_command_execution`、`wait_command_execution`、`cancel_command_execution` 查询、等待和取消；观察等待为 10–30 秒，预期耗时较长或安静的任务优先等待 30 秒。命令 `timeout_seconds` 默认 600 秒、最大 3600 秒，并受更短的会话和 Run 期限约束。这些执行端约定不在 Kael 内实现为远端命令计时器。

观察结果可包含 `execution_elapsed_ms`、`output_idle_ms`、`remaining_ms`、`attention_reason`、`process_finished` 和 `stop_confirmed`。模型在每次观察后重新评估证据；无输出不能证明进程挂起，`waiting_input` 也不构成输入凭据或代替用户确认的授权。

## 2. 派发与回执

一次 Panel 工具调用顺序为：

1. Harness 校验动态工具归属、参数及输入 schema。
2. Service 复验原 Panel、Registration 状态、lease、revision 和 digest，计算调用风险与审批要求。
3. 如需审批，创建绑定原调用和参数摘要的 Approval；批准后复验并消费批准状态。
4. 写入派发状态和 `tool.call`，提交事务后通知原 Panel。
5. 等待 `POST /kael/api/v1/tool-calls/{id}/results` 回执。
6. 将终态 ToolResult 交给 Harness，继续本轮推理；有输出 schema 时继续校验结果。

回执携带递增 `sequence`、`done`、`status`、`result` 和 `error`。status 接受 `running`、`success`、`error`、`cancelled`、`timeout`；非终态回执投影为 `tool.progress`，终态投影为 `tool.completed`、`tool.failed` 或 `tool.cancelled`，并将 Run 从等待工具恢复为 running。

相同 sequence 和 payload 的重投复用已有回执；相同 sequence 改变内容、序列倒退或终态之后追加结果均拒绝。Run/ToolCall 已取消或终止时也不能接受新的结果。回执由事务与超时、取消竞争，已经提交的终态不会被晚到结果覆盖。

## 3. 审批与命令只读判定

Panel 的 `auto` 模式依据注册及调用策略，`always` 始终审批，`never` 跳过 Panel 审批。Service binding 的 Core 写审批单独执行，不受 Panel 模式影响。

Luna 可通过工具注解传递 `com.jumpserver/commandPolicy`，规范化为 `annotations.command_policy`。只有 `panel` binding、`luna.terminal` namespace、`shell-readonly-v1` 策略且无 destructive/final-result 注解的注册会进入命令参数判定。

Kael 使用 shell 语法树检查完整 command，只接受有限的系统查询命令、参数、管道与逻辑组合，并限制重定向。所有片段均确认只读时，本次调用得到 `risk=read` 且不要求自动审批；未知程序、不支持的语法、写入、环境赋值、变量替换、命令替换等沿用注册审批策略。解析过程不会执行命令，也不代替远端 ACL。

符合命令策略的 Approval 可由用户选择记住。auto 模式只复用同一 Panel、Registration ID、定义版本及参数 digest 的已记住批准，不形成跨连接或任意命令的授权。

## 4. 期限与丢失回执

| 期限 | Kael 当前行为 |
|---|---|
| 完整 Run | 默认 30 分钟，包含模型、工具和审批等待 |
| Approval | 默认 10 分钟，派发前再次检查有效期 |
| Panel 工具结果 | 审批及派发完成后开始等待，默认 45 秒 |
| 远端命令 | 执行器管理；RPC 提前返回不延长命令期限 |

`TOOL_RESULT_TIMEOUT` 可设为 1 秒至 10 分钟。收到非终态进度不会重新计时；它限制一次已派发调用等待终态回执的总时间。

结果超时时，Service 在同一事务中：

- 若并发终态回执已提交，直接使用该回执。
- 否则保留最新部分结果，追加 `done=true/status=timeout` 回执，错误码为 `execution_state_unknown`。
- 将 ToolCall 标记 timeout，将 Run 恢复为 running。
- 向原 Panel 投递 `tool.cancel` 和 `tool.failed`，提示执行端取消。

超时结果作为工具观察交给模型，不自动终止整个 Run。发送取消请求不证明远端已停止，已完成写入可能已经生效；后续必须依据真实状态决定处理方式，不能以丢失回执为理由重放写操作。

## 5. 取消与恢复

显式取消 Run 会更新业务状态、取消未完成工具和审批，并终止对应 Harness 执行；服务关闭和 Run 总超时也会取消活动执行。SSE 断开只结束事件订阅，不直接取消 Run。

命令执行器通过注册能力返回的部分输出、等待输入、已结束或停止未确认等状态是模型的观察依据。Harness 指令要求模型在观察后决定继续等待、查询独立证据、按授权处理输入或取消；没有输入能力时不能自行绕过等待或审批。最终答复应据实说明执行和停止是否确认。

Kael 不通过 Run resume 重放已经进入模型执行或产生 ToolCall 的任务，这类恢复返回 `execution_rebind_required`。Terminal JSONL 可恢复历史，但重启时活动 Run 中断、未完成工具取消、Approval 与 Panel/Registration 过期。新 Panel 或新资源连接需要重新注册能力，不能接管旧连接上的执行句柄。

## 6. 现有验证入口

- [tool_timeout_test.go](../../internal/kael/service/tool_timeout_test.go)：缺失回执、部分输出、超时和晚到结果。
- [tool_continuation_test.go](../../internal/kael/service/tool_continuation_test.go)：工具终态后继续 Harness。
- [command_approval_test.go](../../internal/kael/service/command_approval_test.go)：命令审批与记住批准。
- [command_test.go](../../internal/kael/policy/command_test.go)：完整 shell 参数的只读判定边界。
- [turn_test.go](../../internal/kael/runtime/turn_test.go)：动态工具回执、重复调用与 Harness 协议处理。
