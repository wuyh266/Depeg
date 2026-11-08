# CoinGecko 数据导入

项目现在可用 Go 直接读取下载的 ZIP/Parquet 和 JSONL，无需先解压、转 CSV 或安装 Python。运行目录为桌面的 depeg-platform。现有 cmd/import CSV 命令继续保留。

## 先检查三个主要资产

这一步不加载 .env，不连接数据库：

```powershell
cd "$env:USERPROFILE\Desktop\depeg-platform"
go run -buildvcs=false ./cmd/import-coingecko -file .\coingecko_news.zip -kind daily -coin-ids tether,usd-coin,dai -validate-only
```

预期报告为 3 个资产、8,617 条 snapshots、0 条 news，mode 为 validated。

参数 coin-ids 使用 CoinGecko ID，例如 tether、usd-coin、dai；不要填写 USDT、USDC 等简称。数据库与查询接口中的资产 ID 会加 coingecko. 前缀，例如 coingecko.usd-coin。

## 执行迁移并导入日行情与新闻

先完成 .env 中的 MYSQL_DSN 和 JWT_SIGNING_KEY 配置，确保专用数据库存在。已经初始化过数据库也需要再运行迁移：

```powershell
go run -buildvcs=false ./cmd/migrate
go run -buildvcs=false ./cmd/import-coingecko -file .\coingecko_news.zip
```

第二条默认导入包里的全部日行情和新闻。本次文件对应 27 个资产、48,284 条日快照、2,934 条新闻；成功报告的 mode 为 imported。

若只想先接入 USDT、USDC、DAI：

```powershell
go run -buildvcs=false ./cmd/import-coingecko -file .\coingecko_news.zip -kind daily -coin-ids tether,usd-coin,dai
```

kind 可为 all（默认）、daily、ohlc、news。coin-ids 同时筛选行情 ID 与新闻的 matched_coin_ids；一篇文章只写入一次，保留来源记录中的完整关联标签。拼错 ID、所选类型没有匹配记录都会明确报错。

## 小时行情

小时包的元数据缺少报价币种，导入器要求操作者明确确认。以下命令适用于已确认该包价格以美元报价的情况：

```powershell
go run -buildvcs=false ./cmd/import-coingecko -file .\coingecko_ohlcv_stablecoin_list_full.zip -quote-currency USD -validate-only
```

本次文件对应 84 个资产、1,233,413 条小时 K 线，所有成交量均缺失。确认校验报告后，执行写入：

```powershell
go run -buildvcs=false ./cmd/import-coingecko -file .\coingecko_ohlcv_stablecoin_list_full.zip -quote-currency USD
```

也可添加 -coin-ids a7a5 等参数，先导入单个资产。当前适配器仅支持 USD 报价，不猜测其他报价币种。

## 数据含义与处理规则

| 内容 | 存储与处理 |
| --- | --- |
| 资产标识 | coingecko.<coin_id>；简称仅展示，不凭简称合并资产 |
| 锚定价格 | 元数据无法确认时 anchor_price 为 NULL；导入不覆盖已有人工确认的锚定价格、链与合约 |
| 小时 K 线 | 写入 candles；volume 可以为 NULL，真实的 0 仍是 0 |
| 小时源时间 | /ohlc/range 的时间是收盘时刻，保存在 source_time；open_time/接口 time 为源时间减 1 小时 |
| 每日快照 | 写入 market_snapshots，保留 price_usd、market_cap_usd、volume_24h_usd；不生成虚构 OHLC |
| 负市值 | 规范字段写 NULL，quality_flags 加 market_cap_usd_invalid；原始负数仍保存在 raw_values |
| 零市值、零成交额 | 保留 0 并加对应 _zero 标记，不自动解释成缺失 |
| 数值精度 | 保留源浮点的最短可往返十进制文本或精确整数文本；规范字段按有理数四舍五入到小数 18 位，增加字段级 _rounded 标记和报告计数 |
| 小时数值舍入 | quality 为 historical:rounded，并保留 raw_values；本次小时文件没有发生舍入 |
| 价格超出存储能力 | 非正数、NaN/无穷、整数超过 20 位或舍入后为零都会拒绝 |
| 时间缺口 | 按实际观测保留，报告 missing_periods；不插值、不补 0、不制造观测 |
| 新闻 | 写入 news_articles；按来源+URL 摘要幂等，作者统一为字符串数组，保留 raw_json；不抓取链接正文，不把源关联标签视为已验证事件 |

CSV 命令继续要求显式、有效的锚定价格和完整成交量，仍使用精确十进制并拒绝超出 18 位小数；CoinGecko 原始二进制浮点数据采用上述显式舍入与原值保留策略。

当前文件的 264 条负市值全部为 ESD 的 -1。日快照有 1,313 个价格值与 2 个成交额值需要舍入，报告 rounded_values 为 1,315（统计值的数量）。

UTC 时间、字段类型、源声明、行数和首末时刻都进行核对。同一资产重复/倒序时间、缺少元数据、未知字段结构、重复表和损坏文件会导致失败。ZIP 不解压到磁盘；限制压缩文件及总展开大小 512 MiB、单项 64 MiB、4,096 个条目、每表 500,000 行、每包 5,000,000 条行情和 100,000 条新闻。

## 事务与重复执行

每次实际导入先完整校验，再在一个 GORM/MySQL 事务内按资产和小批次写入。使用 SHA-256 检查两次读取之间及读取过程中是否发生文件变化。任何解析、写入、取消或文件变化错误都会使本包事务回滚。

重复导入按唯一键更新，不增加相同来源、资产和时间的重复记录；本包未包含的旧记录不会被删除。不同 ZIP 是独立事务，命令之间不共享整批事务。大包导入会持有较长事务，本阶段不引入后台任务或检查点恢复。

000002 是显式前向迁移，保留旧表数据，新增两张表并允许未知成交量和锚定值。新增列执行前会核对是否存在，以支持 MySQL DDL 部分成功后的重试；没有提供会丢弃这些新数据的自动降级脚本。

## 查询导入结果

迁移后启动或重启后端，注册并登录，使用 access_token 调用新接口：

```http
GET http://127.0.0.1:8080/api/v1/market-snapshots?asset_id=coingecko.usd-coin&from=2023-03-01T00:00:00Z&to=2023-04-01T00:00:00Z&limit=500
Authorization: Bearer <access_token>
```

返回 data.asset、data.points、data.from/to、data.next_cursor。金额使用字符串，缺失金额返回 null，quality_flags 返回字符串数组。区间为 [from,to)，最长 3,660 天，每页 1–2,000 条；翻页保留资产、来源和时间范围。游标按时间+来源排序，并绑定查询类型和范围。

小时行情继续查询 /api/v1/trends，参数 interval=1h；时间范围按开盘时刻指定，源收盘时刻在 source_time 中返回。api/requests.http 提供了完整样例。

这些文件截至 2026-09-28/29；选择对应历史日期才能查到数据。USTC 的 2022-04-25 至 2022-05-22 缺口会原样保留。新闻本阶段提供导入与存储，新闻展示接口另行实现。

## 验证依据

真实压缩包已完整执行 validate-only。独立临时 MySQL 8.0.43 上验证了新迁移、部分迁移重试、DECIMAL 精度、NULL、事务回滚、日行情与新闻实际导入及重复导入；另实际导入 a7a5 的 10,953 条小时行情。测试实例使用单独端口与数据目录，未使用项目 .env 中的业务数据库。

参考：[CoinGecko OHLC 时间定义](https://docs.coingecko.com/reference/coins-id-ohlc-range)、[每日市场字段](https://docs.coingecko.com/reference/coins-id-market-chart-range)、[Go Parquet 库](https://pkg.go.dev/github.com/parquet-go/parquet-go@v0.32.0)。
