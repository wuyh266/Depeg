# Depeg Platform · 稳定币观察台

当前版本包含由 Go 服务直接提供的网页、Gin + GORM 后端、JWT 登录、MySQL 行情查询，以及按账号保存的监测阈值和历史检测记录。MySQL 连接信息在本地 .env 中配置。启动网站无需 Node.js 或独立前端服务。

已实现：配置读取、结构化日志、HTTP 启动与关闭、注册/登录、JWT 验证、MySQL 会话持久化、刷新轮换/登出、稳定币列表、历史趋势查询、CSV 校验/事务导入、显式数据库迁移。

现已增加 CoinGecko ZIP/Parquet/JSONL 导入、可空成交量、日价格快照与 JWT 保护的快照查询。[按此导入已下载的数据集](docs/import-coingecko.md)。已有数据库需要先再次运行 cmd/migrate，应用新增的 000002 迁移。

2026-11-03 新增：市场总览、搜索筛选、币种详情、历史价格/偏离/市值/成交额图表、日期范围切换、CSV 导出、个人阈值设置和历史命中记录。再次运行 cmd/migrate 应用 000003_monitoring.up.sql 后重启服务。[页面使用与监测口径](docs/monitoring.md)。

- [技术设计与完整目录](docs/technical-design.md)
- [本阶段取舍与边界](docs/review-decisions.md)
- [验证记录](docs/verification.md)
- [OpenAPI 接口说明](api/openapi.yaml)
- [可直接修改的请求样例](api/requests.http)

## MySQL 配置好后这样启动

需要 Go 1.25.0 或以上，MySQL 8.0 或以上。本次使用 Go 1.25.4。

在项目目录运行：

```powershell
cd "$env:USERPROFILE\Desktop\depeg-platform"
.\scripts\setup.ps1
```

setup 只在不存在 .env 时生成本地配置与随机 JWT 密钥，不覆盖已有 .env，不输出密钥。随后在 .env 中填写 MYSQL_DSN：

```text
MYSQL_DSN=你的账号:你的密码@tcp(127.0.0.1:3306)/depeg?charset=utf8mb4&parseTime=true&loc=UTC
```

先在 MySQL 中创建专用数据库 depeg，再显式创建表，启动 API：

```powershell
go run -buildvcs=false ./cmd/migrate
go run -buildvcs=false ./cmd/api
```

启动后在浏览器打开 [稳定币观察台](http://127.0.0.1:8080/)，注册或登录即可使用。启动窗口需要保持运行；已有旧进程时先按 Ctrl+C，再执行上述迁移及启动命令。可检查 [存活状态](http://127.0.0.1:8080/healthz) 和 [数据库连接状态](http://127.0.0.1:8080/readyz)。没有数据库配置或数据库无法连接时，API 明确退出，不切换到演示数据。

若列表为空，先导入已有数据包。下面命令只导入 USDT、USDC、DAI 的每日快照，可重复执行：

```powershell
go run -buildvcs=false ./cmd/import-coingecko -file .\coingecko_news.zip -kind daily -coin-ids tether,usd-coin,dai
```

进入币种详情，确认并保存锚定价（例如美元锚定币以 USD 报价时为 1）和预警阈值，再点击“检测当前区间”。导入文件没有提供经确认的锚定价，因此不会自动把所有币种设为 1。当前价格来自历史记录，刷新按钮重新读取数据库，不会采集外部实时行情。

API 启动不会自动改表；迁移必须通过 cmd/migrate 单独执行。当前简单迁移执行器只接受本项目中不含存储过程、复杂分隔符的 DDL，并利用 schema_migrations 记录已应用文件。MySQL DDL 有隐式提交行为，迁移失败后应先检查数据库状态，再修复或重新执行。

## 现在没有 MySQL 也可以检查

```powershell
go build -buildvcs=false ./...
go test -buildvcs=false ./...
go vet -buildvcs=false ./...
go mod verify
```

也可运行 scripts/check.ps1。首次运行需要依赖可用，已有完整模块缓存可离线执行。-buildvcs=false 用于规避当前上层目录不完整的 Git 元数据；未修改全局 Go/Git 设置。

测试使用单独的临时 SQLite 数据库和真实 GORM 仓储来验证接口及事务逻辑。SQLite 是测试依赖，API 运行时只支持 MySQL，不提供 SQLite 或内存数据库降级。CoinGecko 适配另在独立临时 MySQL 8.0.43 上验证了迁移、DECIMAL 精度、NULL、唯一键和事务回滚，详见 docs/verification.md；业务数据库仍需显式执行迁移和导入。

## 导入论文项目的历史行情

先只校验 CSV，不连接数据库：

```powershell
go run -buildvcs=false ./cmd/import -file "$env:USERPROFILE\Desktop\DepegDetector-2026-5-3\DepegDetector-2026-5-3\data\raw data\ohlcv_USDC.csv" -asset-id usdc-ethereum -symbol USDC -chain ethereum -timezone Asia/Shanghai -validate-only
```

配置 MySQL 并执行迁移后，去掉 -validate-only 即可导入。也可以更换文件、asset-id、symbol、quote、anchor 等参数导入其他资产。

本次提供的 USDC 文件中，Unix 时间戳对应 UTC，而无时区的 datetime 文本对应东八区，所以必须显式传入 -timezone Asia/Shanghai。导入器会核对两列是否指向同一时刻，统一存成 UTC；默认时区为 UTC，不自动猜测。若 CSV 的 datetime 已含时区，按其显式偏移解析。

支持列：timestamp（秒或毫秒）和/或 datetime、open、high、low、close、volume；exchange 可选。若存在 exchange，来源会记录为 paper-csv:okx 等形式。价格和成交量使用精确十进制运算校验，也支持 9e-06 之类科学计数法；超出 DECIMAL(38,18) 精度的输入会被拒绝，不静默舍入。

所有行验证通过后，资产信息和行情在一个数据库事务中写入。相同资产/来源/周期/时刻的记录按唯一键更新，重复导入不会重复增加记录。不填补缺失分钟，不生成模拟行情。每个文件最多 128 MiB、500000 行。

## 已实现接口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | /healthz | 进程存活 |
| GET | /readyz | MySQL 连通性 |
| POST | /api/v1/auth/register | 注册，用户名规范为小写 |
| POST | /api/v1/auth/login | 登录，返回 access/refresh token |
| POST | /api/v1/auth/refresh | 单次刷新并轮换会话 |
| GET | /api/v1/auth/me | 当前用户，需 access token |
| POST | /api/v1/auth/logout | 撤销当前会话 |
| GET | /api/v1/assets | 稳定币列表，需登录 |
| GET | /api/v1/trends | 历史行情，需登录 |
| GET | /api/v1/market-snapshots | 每日价格、市值及 24 小时成交额，需登录 |
| GET | /api/v1/dashboard | 币种、来源覆盖、最新观测与个人规则，需登录 |
| POST | /api/v1/monitor/rules | 保存个人规则，绑定资产、来源、周期及锚定价 |
| POST | /api/v1/monitor/replay | 检测指定历史区间，事务保存超阈值观测 |
| GET | /api/v1/monitor/events | 当前账号的命中记录，支持币种筛选和游标分页 |
| GET | /api/v1/risks | 预留，当前明确返回 503 |

趋势参数：asset_id 必填，source 可选，from/to 使用 RFC3339，区间为 [from,to)，最长 31 天。默认查询最近 24 小时。limit 默认 500，范围 1–2000；interval 可为 1m/5m/15m/1h/24h，仅查询该周期的已存数据，不自动聚合。

响应返回 next_cursor 和实际 from/to。翻页时应保留相同的资产、来源、周期和时间范围。游标同时包含时间和来源，避免同一分钟多个来源漏行。缺少数据返回空数组，不补成 0。

## 认证与当前边界

密码使用 bcrypt，明文不入库、不返回。JWT 固定使用 HS256，验证签名、有效期、签发者、受众、令牌用途和会话；签名密钥至少 32 字节。刷新令牌在 MySQL 中只保存摘要，刷新通过事务保证单次使用。刷新后旧 access/refresh 均失效，登出也会使当前两种令牌失效。数据库不可用时拒绝授权。

本阶段未启用 Redis、缓存、分布式锁、定时采集、外部消息通知和模型服务。历史检测由用户在网页中发起；不把历史异常当作当前行情告警。高级模块的旧接口与 TODO 文件保留，但不接入运行入口。行情写入仍通过本地 CLI 执行。面向公网的部署与登录限流等运维措施需在后续部署阶段完善。

配置优先级：进程环境变量 > .env > 可选 configs/config.yaml > 非敏感默认值。环境覆盖值不会写回 .env，也不修改进程全局环境。支持 ENV_FILE、CONFIG_FILE 指定其他路径。
