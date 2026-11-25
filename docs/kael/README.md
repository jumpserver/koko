# Kael 文档

本目录只描述当前代码已实现的设计与接口边界。

- [当前架构](./ARCHITECTURE.md)：组件职责、Codex Harness、领域对象、能力与审批、HTTP/SSE、存储及部署约束。
- [命令执行生命周期](./command-execution-lifecycle.md)：Panel 工具调用、命令状态、回执超时、取消与恢复边界。

配置项和默认值见 [config_example.yml](../../cmd/kael/config_example.yml)，启动装配见 [cmd/kael/main.go](../../cmd/kael/main.go)。

修改组件职责、模型执行、领域对象、协议、审批或持久化行为时，同步更新对应设计说明。文档不维护迁移计划、历史契约或尚未实现的目标方案；接口字段和校验以链接的实现为准。

## 在 KoKo 镜像中运行 Kael

s6 使用相同的容器用户启动 KoKo 和 Kael，镜像默认为 root。迁移时先停止旧 Kael，再将原数据目录以可读写方式挂载到 `/opt/kael/data`，即可复用原有组件密钥。多副本部署时保留一个活动实例，在其余副本上设置 `KAEL_ENABLED=false` 或 `0`。

配置文件可挂载到 `/opt/kael/config.yml`，也可用 `KAEL_NAME`、`KAEL_BIND_HOST` 和 `KAEL_HTTP_PORT` 单独配置 Kael。网关应将 `/kael/` 转发到启用 Kael 的实例，默认端口为 `8083`，保留路径前缀并关闭 SSE 缓冲。就绪探测端点为 `/kael/health/ready`。

## 许可证

本项目采用 GNU General Public License version 3（GPLv3），与 [JumpServer 主库](https://github.com/jumpserver/jumpserver)保持一致。完整许可条款见 [LICENSE](../../LICENSE)。
