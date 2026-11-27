# Core SQL 数据库覆盖与驱动来源核对

核对日期：2026-10-28。

Core 基线：`origin/dev@2452f9cdd23a29efd067a8a5135a0c3dcd3c1fa1`。
检查 `apps/assets/const/database.py` 的 `DatabaseTypes`、
`apps/assets/const/protocol.py` 及 OceanBase 平台协议设置。
本地 Core `dev@8f4eae3cf` 的数据库类型清单与此远端基线相同。

## 覆盖结论

Core 列出 11 类数据库资产。其中 MongoDB、Redis 为非关系型数据库，
本次核对其余 9 类 SQL 数据库（包括使用 SQL 的列式分析数据库 ClickHouse）。
加入本次 Db2 驱动后，usql 已注册并编译全部 9 类，没有其他数据库类型缺失。
OceanBase 分别检查 MySQL 和 Oracle 两种租户模式。

| Core 类型 | usql URL scheme | 实际 Go 驱动模块 | 来源说明 |
| --- | --- | --- | --- |
| MySQL | `mysql` | `github.com/go-sql-driver/mysql` v1.8.1 | 社区维护的 database/sql 驱动，不标称为 Oracle 公司官方驱动 |
| MariaDB | `mariadb` / `maria` | `github.com/go-sql-driver/mysql` v1.8.1 | 复用 MySQL 协议驱动，不是独立的 MariaDB 官方 Go 连接器 |
| PostgreSQL | `postgresql` / `postgres` | `github.com/lib/pq` v1.10.9 | 社区 Go 驱动，不等于 PostgreSQL 官方的 C libpq |
| Oracle | `oracle` | `github.com/sijms/go-ora/v2` v2.8.17 | 社区纯 Go 驱动，不标称为 Oracle 公司官方驱动 |
| SQLServer | `sqlserver` / `mssql` | `github.com/microsoft/go-mssqldb` v1.7.1 | Microsoft 官方项目 |
| DB2 | `db2` / `db` / `db2s` | `github.com/go-db2/go-db2` v0.0.0-20261015010257-944faf6c05dc | 社区纯 Go 驱动；本次新增，非 IBM 官方驱动 |
| Dameng | `dameng` / `dm` | `gitee.com/chunanyong/dm` v1.8.23 | 第三方 Go module 分发；其 README 声明同步达梦官方驱动，本次未逐文件比对厂商 SDK |
| OceanBase | `oceanbase`、`oboracle` | `github.com/oceanbase/go-oceanbase-driver` v1.7.0-oceanbase.1 | OceanBase 官方项目驱动，两种租户模式各有 usql 适配器 |
| ClickHouse | `clickhouse` / `ch` | `github.com/ClickHouse/clickhouse-go/v2` v2.24.0 | ClickHouse 官方客户端 |

驱动存在、厂商官方维护、真实服务兼容性是三项不同结论。
这次没有缺失类型需要引入额外驱动，也没有将原有社区驱动改称官方驱动。

## Go 驱动来源与当前发布约束

用户已确认使用 Go 驱动即可，不要求将现有社区 Go 驱动全部替换成厂商官方包。
因此保留已验证的驱动组合和纯 Go 跨平台发布方式。

IBM 官方 Go 驱动是 [`github.com/ibmdb/go_ibm_db`](https://github.com/ibmdb/go_ibm_db)，
需要 CGO、C 头文件和 IBM CLI 原生库；参见其
[安装说明](https://github.com/ibmdb/go_ibm_db/blob/master/INSTALL.md)。
它不能直接作为当前 `CGO_ENABLED=0`、单二进制发布方案的等价替换。
现有 Db2 实现使用已验证的纯 Go 驱动；改用 IBM 官方驱动会涉及构建、运行依赖
和各目标平台的原生 SDK，需要单独处理，不能仅替换 import 后宣称支持。

其他来源依据：

- [Microsoft Go SQL Server 驱动](https://github.com/microsoft/go-mssqldb)
- [ClickHouse 官方 Go 文档](https://clickhouse.com/docs/integrations/language-clients/go/index)
- [OceanBase 官方 Go 驱动项目](https://github.com/oceanbase/go-oceanbase-driver)
- [Go MySQL Driver 项目](https://github.com/go-sql-driver/mysql)
- [lib/pq 项目](https://github.com/lib/pq)
- [go-ora 项目](https://github.com/sijms/go-ora)
- [社区 Db2 驱动项目](https://github.com/go-db2/go-db2)，其 README 明确说明不是 IBM 官方驱动
- 达梦来源声明见当前依赖 `gitee.com/chunanyong/dm@v1.8.23/README.md`；
  其指向的厂商下载地址为 `https://package.dameng.com/eco/adapter/resource/go/dm-go-driver.zip`。

## 验证与边界

```sh
CGO_ENABLED=0 go run . --drivers
GOMAXPROCS=2 CGO_ENABLED=0 go test -p 2 . \
  -run TestCoreSQLDriverConnections -count=1 -v
```

`core_coverage_test.go` 检查 10 个连接入口（9 类数据库，含 OceanBase 两种模式）：
使用真实 URL 解析器、usql 注册驱动和实际 `drivers.Open` 初始化，并确认未建立网络连接。
PostgreSQL 测试 URL 显式配置 SSL 模式，以避免上游适配器默认 SSL retry 在 Open 阶段联网。
测试结果为全部通过，不能据此宣称对这 9 类数据库均完成了真实服务器连接验证。

Db2 另已在真实 LUW 11.5.7 测试库验证；详见 README 中的验证说明。
MongoDB、Redis 不计入此 SQL 覆盖清单，也没有为它们增加 usql SQL 驱动。
Core 和 KoKo 的 Db2 Web CLI 路由仍是发布新版 usql 后的独立接入步骤；
Core 资产类型存在不等于当前端到端 Web CLI 已可使用。

本次仅合并及提交到本地 usql `master`，不 push、不创建新 tag、不发布 release。
