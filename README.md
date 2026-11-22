# Depeg Platform · 稳定币观察台

DG Transformer 已补充离线 JSON 交付校验命令，见 [模型联调说明](docs/dg-inference-handoff.md)。真实模型验收和网站风险服务接入仍待完成。

2026-11-21：邮件新增 PLAIN/LOGIN 兼容、自动 TLS 端口选择和脱敏发送日志，见[邮件排障](docs/email-troubleshooting.md)。Go → Python gRPC 已完成独立联通验证，见[gRPC 方案与运行方法](docs/grpc-inference.md)。

当前版本包含由 Go 服务直接提供的网页、Gin + GORM 后端、JWT 登录、MySQL 行情查询，以及按账号保存的监测阈值和历史检测记录。MySQL 连接信息在本地 .env 中配置。启动网站无需 Node.js 或独立前端服务。

已实现配置读取、结构化日志、HTTP 启动与关闭、邮箱验证码注册、用户名密码登录、JWT 验证、MySQL 会话持久化、刷新轮换与登出、稳定币列表、历史趋势查询、CSV 校验与事务导入、显式数据库迁移。

现已增加 CoinGecko ZIP/Parquet/JSONL 导入、可空成交量、日价格快照与 JWT 保护的快照查询。[按此导入已下载的数据集](docs/import-coingecko.md)。已有数据库需要先再次运行 cmd/migrate，应用新增的 000002 迁移。

2026-11-03 新增：市场总览、搜索筛选、币种详情、历史价格/偏离/市值/成交额图表、日期范围切换、CSV 导出、个人阈值设置和历史命中记录。再次运行 cmd/migrate 应用 000003_monitoring.up.sql 后重启服务。[页面使用与监测口径](docs/monitoring.md)。

升级市场总览时，先暂停行情导入，使用新版 `cmd/migrate` 执行 `000005_market_stream_summaries.up.sql`，从已有行情回填来源摘要。随后使用新版 API 和导入工具恢复服务。导入在同一事务内写行情和摘要，提交后刷新总览即可看到结果。总览通过摘要定位最新行情，再按账号计算阈值状态。后续补录也应通过项目的导入入口维护摘要。

- [Docker 一起启动 Go、Python、MySQL 和 Redis](deployments/docker.md)
- [技术设计与完整目录](docs/technical-design.md)
- [本阶段取舍与边界](docs/review-decisions.md)
- [验证记录](docs/verification.md)
- [OpenAPI 接口说明](api/openapi.yaml)
- [可直接修改的请求样例](api/requests.http)

## MySQL 配置好后这样启动

需要 Go 1.26.0 或以上，MySQL 8.0 或以上。

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

启动后在浏览器打开 [稳定币观察台](http://127.0.0.1:8080/)。已有账号可直接登录，新账号需要先配置发信服务。启动窗口需要保持运行；已有旧进程时先按 Ctrl+C，再执行上述迁移及启动命令。可检查 [存活状态](http://127.0.0.1:8080/healthz) 和 [数据库连接状态](http://127.0.0.1:8080/readyz)。没有数据库配置或数据库无法连接时，API 明确退出，不切换到演示数据。

若列表为空，先导入已有数据包。下面命令只导入 USDT、USDC、DAI 的每日快照，可重复执行：

```powershell
go run -buildvcs=false ./cmd/import-coingecko -file .\coingecko_news.zip -kind daily -coin-ids tether,usd-coin,dai
```

进入币种详情，确认并保存锚定价（例如美元锚定币以 USD 报价时为 1）和预警阈值，再点击“检测当前区间”。导入文件没有提供经确认的锚定价，因此不会自动把所有币种设为 1。当前价格来自历史记录，刷新按钮重新读取数据库，不会采集外部实时行情。

API 启动不会自动改表；迁移必须通过 cmd/migrate 单独执行。当前简单迁移执行器只接受本项目中不含存储过程、复杂分隔符的 DDL，并利用 schema_migrations 记录已应用文件。MySQL DDL 有隐式提交行为，迁移失败后应先检查数据库状态，再修复或重新执行。

## 配置注册邮件

在本地 `.env` 或 Docker 部署使用的 `.env.docker` 填写发信服务提供的 SMTP 地址、账号和授权码，再重新启动 API。

```dotenv
SMTP_ADDRESS=smtp.example.com:465
SMTP_USERNAME=sender@example.com
SMTP_PASSWORD=your-smtp-authorization-code
SMTP_FROM=sender@example.com
SMTP_TLS_MODE=auto
```

`SMTP_FROM` 默认使用 `SMTP_USERNAME`。`auto` 会对 465 端口使用隐式 TLS，其他端口使用 STARTTLS；也可明确设置 `tls` 或 `starttls`。常见端口与加密方式冲突时会在启动时给出说明。认证按服务器声明选择 PLAIN 或 LOGIN，凭据必须在 TLS 中发送。直接运行 API 时，可用 `127.0.0.1` 上的无凭据测试收件服务配合 `SMTP_TLS_MODE=none`。Docker 通过 `docker compose --env-file .env.docker up -d --build` 重建服务并运行迁移。未填写 `SMTP_ADDRESS` 时，已有账号可登录，发验证码会明确返回 `email_not_configured`。

运行迁移后，在注册页填邮箱并点「发送验证码」。验证码为 6 位数字，10 分钟有效，最多尝试 5 次，同一邮箱每 60 秒可重发。服务器按连接来源限制 10 分钟内发送 10 次，部署在反向代理后时，来自同一个代理地址的请求共享额度。验证成功后，后端在同一个事务里创建账号并让验证码失效。每个邮箱地址可注册一个账号，邮箱域名统一转小写，@ 前的部分保留原大小写。前端随后用用户名和密码登录。

### 通过邮箱找回密码

已有部署先运行 `cmd/migrate` 应用 `000007_password_reset.up.sql`，再重启新版 API。Docker 部署沿用上方的重建命令。找回密码使用同一套 SMTP 配置。

在登录页点击「忘记密码？」，填写账号已验证的邮箱并发送重置码。重置码为 6 位数字，10 分钟内有效，最多输错 5 次。同一邮箱每 60 秒可重发。发码接口统一提示符合条件的邮箱会收到邮件，发信由后台处理。注册和密码重置按连接来源共享发送限额。

填写重置码和两次新密码后提交。后端在同一事务里更新密码哈希、作废重置码并撤销账号的旧登录会话。成功后回到登录页，使用用户名和新密码登录。此前未验证邮箱的旧账号无法通过邮件自助重置。

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

## 把 OKX 分钟采集接到 Go 导入工具

在 `depeg-platform` 目录中先编译导入工具。使用 `go.mod` 要求的 Go 版本；目标 MySQL 要先执行 `cmd/migrate`。

```powershell
New-Item -ItemType Directory -Path .\bin -Force | Out-Null
go build -buildvcs=false -o .\bin\import.exe ./cmd/import
.\bin\import.exe -latest-open-time -asset-id ccxt.usdc-usdt -source ccxt:okx -interval 1m
```

最后一条是只读检查：有记录时只输出 UTC 开盘时间（如 `2026-11-08T08:34:00Z`）；没有该资产、来源和周期的记录时正常退出，输出为空。它只告诉采集器从哪里继续，不能证明更早的分钟都齐全。

要试跑一轮，在同一个 PowerShell 窗口继续执行。先确保 `.env` 指向你要使用的数据库，第一次建议用隔离测试库。

```powershell
$env:ENV_FILE = (Resolve-Path .\.env).Path
$importer = (Resolve-Path .\bin\import.exe).Path
Set-Location "$env:USERPROFILE\Desktop\Depeg-future"
python src/collect_ohlcv.py --once --import-bin $importer --start 2026-11-07T08:35:00Z --end 2026-11-07T08:45:00Z
```

如果已经用 Python 保存了 CSV，Go 可以单独导入：用 `-file 路径 -asset-id ccxt.usdc-usdt -symbol USDC -quote USDT -source ccxt -interval 1m -timezone UTC "-anchor="`。CSV 里的 `exchange=okx` 和 `-source ccxt` 合起来是 `ccxt:okx`，跟论文历史的 `paper-csv:okx` 分开。显式的 `-anchor=` 让新资产的锚定价留空；已有资产的配置不会被改写，报价冲突会拒绝整批。工具只需要数据库配置，不需要网站的 JWT 密钥。

确认单次写入正常后，连续采集可用 `--loop --import-bin $importer`。目标库没有该行情时，再加 `--bootstrap-from` 和明确的 UTC 起点。写库失败的 CSV 留在 Python 仓库的 `data/collector-pending/`，再次运行会先重试。

## 已实现接口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | /healthz | 进程存活 |
| GET | /readyz | MySQL 连通性 |
| POST | /api/v1/auth/register/email-code | 发送注册邮箱验证码 |
| POST | /api/v1/auth/register | 核对验证码并注册，用户名规范为小写 |
| POST | /api/v1/auth/password-reset/email-code | 为符合条件的已验证邮箱申请重置码 |
| POST | /api/v1/auth/password-reset | 核对重置码并更新密码、撤销旧会话 |
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
