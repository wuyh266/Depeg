# 稳定币平台基础后端技术设计

当前版本：0.5 · 2026-11-03，新增本地网站与个人阈值，详见文末增量和 dashboard-review-20261103.md。下方保留 0.4 · 2026-10-27 的后端设计记录。Gin/GORM 技术选型已确认；本阶段暂缓 Redis 等高级部分。本地 MySQL 连接信息通过 .env 提供。

## 1. 本阶段结果

从占位骨架推进到基础后端：Gin 服务、配置/日志、GORM/MySQL、注册登录与 JWT 校验、MySQL 会话、刷新轮换和登出、资产列表、历史趋势查询、CSV 与 CoinGecko ZIP/Parquet/JSONL 校验和事务导入、日快照查询、显式迁移。

API 运行时只依赖 MySQL；Redis、缓存、租约、worker、模型推理均不在启动依赖图中。未接入的风险预测明确返回 503，不用模拟数据填充。

## 2. 项目结构

本目录共 82 个源码与文档文件，其中 61 个 Go 源文件，属于 33 个包。本地 .env、原始 ZIP、构建缓存和测试数据库不列入下方源码目录。

```text
depeg-platform/
├── api/
│   ├── openapi.yaml
│   └── requests.http
├── cmd/
│   ├── api/
│   │   └── main.go
│   ├── import/
│   │   └── main.go
│   ├── import-coingecko/
│   │   └── main.go
│   ├── migrate/
│   │   └── main.go
│   └── worker/
│       └── main.go
├── configs/
│   └── config.example.yaml
├── deployments/
│   └── compose.yaml
├── docs/
│   ├── dataset-review-20261027.md
│   ├── import-coingecko.md
│   ├── review-decisions.md
│   ├── technical-design.md
│   └── verification.md
├── internal/
│   ├── adapter/
│   │   ├── cache/
│   │   │   └── redis/
│   │   │       ├── client.go
│   │   │       ├── leases.go
│   │   │       ├── sessions.go
│   │   │       └── trends.go
│   │   ├── dataset/
│   │   │   └── coingecko/
│   │   │       ├── archive.go
│   │   │       ├── news.go
│   │   │       ├── parquet.go
│   │   │       └── reader_test.go
│   │   ├── http/
│   │   │   ├── handler/
│   │   │   │   └── handler.go
│   │   │   ├── middleware/
│   │   │   │   ├── jwt.go
│   │   │   │   └── jwt_test.go
│   │   │   ├── server.go
│   │   │   └── server_test.go
│   │   ├── logging/
│   │   │   └── logger.go
│   │   ├── persistence/
│   │   │   └── mysql/
│   │   │       ├── client.go
│   │   │       ├── dataset.go
│   │   │       ├── dataset_test.go
│   │   │       ├── jobs.go
│   │   │       ├── market.go
│   │   │       ├── migrate.go
│   │   │       ├── models.go
│   │   │       ├── risk.go
│   │   │       ├── sessions.go
│   │   │       ├── snapshots.go
│   │   │       └── users.go
│   │   ├── pipeline/
│   │   │   └── python/
│   │   │       └── client.go
│   │   ├── scheduler/
│   │   │   └── scheduler.go
│   │   └── security/
│   │       ├── jwt/
│   │       │   ├── manager.go
│   │       │   └── manager_test.go
│   │       └── password/
│   │           └── hasher.go
│   ├── bootstrap/
│   │   └── factory.go
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go
│   ├── domain/
│   │   ├── auth/
│   │   │   └── auth.go
│   │   ├── dataset/
│   │   │   └── dataset.go
│   │   ├── job/
│   │   │   └── job.go
│   │   ├── market/
│   │   │   └── market.go
│   │   ├── risk/
│   │   │   └── risk.go
│   │   └── errors.go
│   ├── identity/
│   │   └── id.go
│   ├── importcsv/
│   │   ├── csv.go
│   │   └── csv_test.go
│   ├── placeholder/
│   │   └── error.go
│   ├── port/
│   │   ├── auth.go
│   │   ├── dataset.go
│   │   ├── infrastructure.go
│   │   ├── job.go
│   │   ├── market.go
│   │   ├── pipeline.go
│   │   ├── risk.go
│   │   └── runtime.go
│   └── service/
│       ├── auth/
│       │   └── service.go
│       ├── ingestion/
│       │   └── service.go
│       ├── market/
│       │   └── service.go
│       └── risk/
│           └── service.go
├── migrations/
│   ├── 000001_initial.down.sql
│   ├── 000001_initial.up.sql
│   ├── 000002_coingecko_import.up.sql
│   └── embed.go
├── scripts/
│   ├── check.ps1
│   ├── setup.ps1
│   └── start.ps1
├── web/
│   └── README.md
├── .env.example
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

## 3. 依赖与装配

| 层 | 职责 |
| --- | --- |
| domain | 领域数据与错误，只使用标准库 |
| port | 仓储、认证、运行时等行为接口，不导入 Gin/GORM |
| service | 凭据策略、会话验证、趋势查询约束 |
| adapter/http | Gin 路由、请求/响应、认证中间件、CORS、HTTP 启停 |
| adapter/persistence/mysql | GORM 会话/事务、模型、MySQL 用户/会话/行情仓储与迁移 |
| adapter/security | bcrypt 密码与 JWT HS256 |
| importcsv | 精确数值和时间校验，不直接连接数据库 |
| bootstrap | 选择具体适配器，返回接口 |
| cmd/api | 从配置、MySQL、仓储、认证到 HTTP 的完整装配 |
| cmd/migrate | 显式迁移命令 |
| cmd/import | 显式本地历史 CSV 导入；validate-only 不连接数据库 |

main 不直接创建 Gin Engine、GORM DB 或具体仓储，继续通过 Factory 装配。HTTP 层使用原生 Gin 类型，业务服务不持有 Gin Context；GORM DB 仅在数据库适配器内使用。SQLite 只出现在测试中，API 无 SQLite/内存数据库降级路径。

## 4. 配置和运行

配置优先级为进程环境变量 > .env > 可选 YAML > 非敏感默认值。环境配置加载不调用 os.Setenv，不修改全局设置。YAML 拒绝未知字段；.env 支持常见引号写法。

MySQL DSN 与 JWT 密钥是必填项，密钥至少 32 字节。连接池、HTTP 超时、JWT 生命周期、来源白名单经过校验。数据库驱动统一使用 UTC，并设置连接/读写超时。数据库无法连接时不启动 API。

HTTP 包含 /healthz 存活检查和 /readyz 数据库连通性检查。后者验证数据库连接，不代替所有表和业务数据的检查。请求具有随机 request_id、大小限制及统一错误格式；CORS 只允许明确配置的来源。服务支持退出信号和优雅关闭。

## 5. 认证链路

注册规范化用户名为小写，限定 3–32 位 ASCII 用户名；密码 8–72 UTF-8 字节，使用 bcrypt 哈希。数据库唯一索引防止重复用户名，响应不包含密码哈希。

登录签发不同用途的 access/refresh JWT，固定 HS256，验证签名、exp、nbf、iat、iss、aud、用途、subject、jti 与 session id。数据库只保存 refresh 摘要。受保护请求同时检查数据库会话和账户状态。

刷新先验证 refresh 用途，再在事务中条件消费旧摘要并创建新会话；条件更新必须恰好影响一行。旧 access/refresh 随旧会话撤销而失效。重复刷新被拒绝；登出撤销当前会话。当前没有设备管理、会话家族级失窃处置或多签名密钥轮转。

会话查找或数据库操作不可用时返回 503；无效凭据返回 401。不会以 Redis 缺失或数据库故障作为允许访问的理由。

## 6. 数据模型与迁移

已建立 users、auth_sessions、assets、candles、market_snapshots、news_articles 六张业务表，迁移执行记录保存在 schema_migrations。

- users：随机 ID、唯一用户名、密码哈希、禁用标记及时间。
- auth_sessions：会话 ID、用户 ID、唯一刷新摘要、到期/撤销时间。
- assets：稳定资产 ID、符号、链、合约、报价币种与锚定价格。
- candles：资产、来源、周期、UTC 开盘时间、OHLC/可空成交量、质量标记、源收盘时刻及原始数值。
- market_snapshots：每日价格、市值、24 小时成交额、质量标记及原始数值。
- news_articles：按来源与 URL 摘要标识，保存标准化作者、源币种标签及完整原始 JSON。

持久化金额使用 DECIMAL(38,18)，领域及 API 使用字符串，未知金额和锚定价格为 null。行情唯一键为资产/来源/周期/时间，另有按资产/周期/时间查询的索引。

应用启动不 AutoMigrate。cmd/migrate 执行内嵌、经 review 的简单 SQL。MySQL DDL 可能隐式提交，迁移不宣称整批原子性，不提供自动破坏性回滚。后续复杂变更需新增迁移，并进一步确定成熟迁移工具。

## 7. CSV 和趋势查询

CSV 支持 Unix 秒/毫秒、RFC3339 或明确时区的无时区文本。两列时间都存在时必须一致。所给 USDC 文件的 datetime 为东八区文本，需显式传入 -timezone Asia/Shanghai，统一转为 UTC。科学计数法使用有理数精确转换，不经 float64，超出数据库精度会报错。

先验证全部行，再在同一 GORM 事务中 upsert 资产和行情。支持重复导入，不填补缺失数据。限制每文件 128 MiB、500000 行。exchange 列会附加到来源命名空间，保留价格来源。

趋势查询为 [from,to)，最长 31 天，每页最多 2000 条；使用时间与来源组成的游标，绑定查询的资产/来源/周期/时间范围。返回实际边界以供下一页复用。不自动重采样，查询周期必须对应已存记录；无记录返回空数组。

## 8. 测试与未实现边界

已使用真实 Gin、JWT、bcrypt、GORM 和独立临时 SQLite 测试数据库验证基础流程。覆盖凭据错误、令牌篡改/过期/错误用途、刷新防重用、登出失效、禁用账户、数据库故障拒绝授权、分页幂等、事务回滚、CSV 格式/时区/科学计数法、CORS、服务关闭。

独立临时 MySQL 8.0.43 已验证迁移、DECIMAL、NULL、唯一键、事务回滚、真实日行情/新闻及小时样本的导入与幂等。业务库仍需按导入说明执行显式迁移与写入。模型服务、Redis、自动采集、分布式任务、前端和生产运维能力留待后续，不以保留的 TODO 接口宣称已完成。

参考：[Gin 模块版本](https://github.com/gin-gonic/gin/blob/v1.12.0/go.mod)、[GORM 事务](https://gorm.io/docs/transactions.html)、[JWT v5.3.1](https://pkg.go.dev/github.com/golang-jwt/jwt/v5@v5.3.1)。

## 9. CoinGecko 导入与每日快照

端口定义在 internal/port/dataset.go，领域数据定义在 internal/domain/dataset。main 通过 Factory 注入 DatasetReader、DatasetRepository；Parquet 与 ZIP 解码位于 coingecko 适配器，GORM 事务与 upsert 位于 MySQL 适配器。

导入流程为完整校验 → 比较文件指纹 → 开启整包事务 → 按资产/新闻小批次读取和写入 → 再核对文件指纹 → 提交。任一步失败或取消均回滚，不填补时间缺口。未知锚定与成交量保留 NULL，负市值规范为 NULL 并保留原值；数值舍入增加质量标记。

/api/v1/market-snapshots 使用 JWT 保护，允许最长 3,660 天的半开区间查询，沿用每页最多 2,000 条与绑定查询范围的时间/来源游标。它返回实际每日观测，不将单点价格伪造为 OHLC。小时趋势保留原接口。

完整参数、源时间含义、精度策略和命令见 [导入说明](import-coingecko.md)。新闻本轮支持导入与存储，展示接口另行实现。
# 2026-11-03 增量：本地网站与个人阈值

网页由 Go 嵌入提供，无独立 Node 服务。新增 domain/monitor、port/monitor.go、service/monitor、MySQL monitor.go、HTTP handler/monitor.go；main 通过 Factory 注入 MonitorRepository 和 MonitorService。迁移 000003 仅增加 monitor_rules 和 monitor_events。

web/static 中的 index.html、app.css、app.js、charts.js 提供总览、详情、历史趋势、个人阈值和记录。精确判断在后端，图表与 CSV 基于原始分页结果。规则和事件均按用户隔离，不改变已导入的资产锚定元数据。当前历史检测由用户发起，不启用实时采集或 Redis。

完整使用和边界见 [monitoring.md](monitoring.md)，本次取舍见 [dashboard-review-20261103.md](dashboard-review-20261103.md)。下方为原项目结构与后端设计记录。

