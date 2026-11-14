# Docker 启动稳定币观察台

准备能运行 Linux 容器的 Docker 和 Docker Compose 2.17 或更新版本。默认目录布局如下。

```text
同一个父目录/
├── depeg-platform/
└── Depeg-future/
```

如果 Python 仓库在其他位置，在 `.env.docker` 的 `DEPEG_PYTHON_DIR` 填写该目录的路径。

## 首次配置

在 `depeg-platform` 目录运行以下命令。

```powershell
Copy-Item docker.env.example .env.docker
```

打开 `.env.docker`，为 `DEPEG_MYSQL_PASSWORD`、`DEPEG_MYSQL_ROOT_PASSWORD` 和 `DEPEG_JWT_SIGNING_KEY` 分别填写不同的随机十六进制字符串。可以用以下命令生成三行，每行用作一个值。

```powershell
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try {
    foreach ($i in 1..3) {
        $bytes = New-Object byte[] 32
        $rng.GetBytes($bytes)
        [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
    }
} finally {
    $rng.Dispose()
}
```

默认网页端口是 `8080`。如已被占用，可修改 `DEPEG_PORT`。Python 采集器需要访问 OKX。若通过宿主机代理访问，可将 `COLLECTOR_PROXY` 设为容器可访问的代理地址，例如 Docker Desktop 上的 `http://host.docker.internal:7890`。

新账号通过邮件验证码注册。请在 `.env.docker` 填写 SMTP 发信参数。已有账号可直接登录。详见[注册邮件配置](../README.md#配置注册邮件)。

## 启动与查看

```powershell
docker compose --env-file .env.docker up -d --build
docker compose --env-file .env.docker ps -a
```

打开 [稳定币观察台](http://127.0.0.1:8080/)。`migrate` 等 MySQL 就绪后执行数据库迁移，成功时显示 `Exited (0)`。Go 网页和 Python 采集器随后启动。Go 使用 Redis 缓存币种列表，用户账号和登录会话继续保存在 MySQL。

Python 运行 `collect_ohlcv.py`，持续采集 OKX 的 USDC/USDT 已收盘分钟 K 线，并调用镜像内的 Go 导入工具写入同一数据库。空库默认从采集器启动前约 10 分钟开始。如果需要指定首次起点，请在启动前设置 `COLLECTOR_BOOTSTRAP_FROM`，填写对齐整分钟的 UTC 时间。已有记录时，采集器根据库里的最新开盘时间继续。

```powershell
docker compose --env-file .env.docker logs --tail 50 migrate api collector
```

这套配置使用自己的 MySQL 数据卷。首次启动会创建新数据库，需要在网页重新注册账号。已有本机数据库的数据需另行迁移。未入库的采集批次保存在单独的数据卷，采集器恢复后会先重试。

```powershell
docker compose --env-file .env.docker down
```

普通 `down` 会保留 MySQL 和待入库批次的数据卷。后续启动沿用同一份 `.env.docker`。MySQL 密码环境变量用于初始化空数据卷，已有数据库的密码通过 MySQL 修改。
