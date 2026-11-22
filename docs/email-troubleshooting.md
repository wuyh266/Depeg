# 注册与密码重置邮件排障

## 本次确认的问题与修改

2026-11-21 从原仓库 main 同步邮箱认证实现后检查。原实现只支持 SMTP AUTH PLAIN；邮件发送失败统一返回通用 unavailable；找回密码在后台发送，其错误没有诊断日志。旧默认 tls 配合 587 端口会尝试错误的握手方式。

现在支持服务器声明的 PLAIN/LOGIN，默认模式改为 auto（465 隐式 TLS，其余端口 STARTTLS）。465+starttls、587+tls 配置在启动时拒绝并说明修正方法。TLS 仍验证证书、主机名及 TLS 1.2 下限，不降级为明文认证。

注册发码失败返回 `email_delivery_failed`，没有配置 SMTP 返回 `email_not_configured`；页面显示对应原因，不再将邮件错误提示为 MySQL 故障。SMTP 适配器为注册和后台密码重置发送记录同样的脱敏日志，保留阶段、原因类别和 SMTP 数字状态，不记录收件人、密码、授权码、验证码或原始服务商响应。

密码重置仍统一返回“如果邮箱符合条件将收到邮件”，防止暴露账号是否存在；204 不代表后台投递一定成功，管理员需查看发信日志。验证码的过期、冷却和每来源限额沿用现有实现；没有增加自动重发，以免 SMTP 状态不确定时产生重复邮件。

## 配置

本地 `.env` 或 Docker 的 `.env.docker` 需要实际可用的 SMTP_ADDRESS、SMTP_USERNAME、SMTP_PASSWORD，SMTP_FROM 留空时采用用户名。SMTP_PASSWORD 使用服务商的授权码或 SMTP 密码，不应假定网页登录密码也适用。该文件保持本地，不提交到 Git。

示例地址仅用于说明格式，需换为自己的服务商：

```dotenv
SMTP_ADDRESS=smtp.example.com:465
SMTP_USERNAME=sender@example.com
SMTP_PASSWORD=your-smtp-authorization-code
SMTP_FROM=sender@example.com
SMTP_TLS_MODE=auto
```

重新启动 API 使配置生效；Docker 运行 `docker compose --env-file .env.docker up -d --build api`。同时确保已应用邮箱注册及密码重置的数据库迁移。`SMTP_FROM` 必须是单个邮箱地址，且满足服务商对授权发件人的要求。

## 日志定位

| stage / reason / status | 检查内容 |
| --- | --- |
| connect / transport_error | 容器或主机能否连接 SMTP 地址、DNS、网络和端口 |
| greeting / timeout | 是否把 587 配成隐式 TLS、465 配成 STARTTLS，或服务商连接过慢 |
| starttls 或 greeting / tls_verification_failed | 证书域名、证书链、系统时间及网络代理；不要关闭证书验证 |
| authenticate / authentication_unavailable | 服务商是否声明 PLAIN 或 LOGIN，是否已启用 SMTP |
| authenticate / smtp_rejected / 535 等 | 授权码是否正确、账号是否允许 SMTP 登录 |
| mail_from / smtp_rejected | 发件地址是否获该账号授权 |
| recipient / smtp_rejected | 收件人地址或服务商收件策略 |
| data / smtp_rejected | 内容策略、配额或服务商投递限制 |

服务商接受 DATA 后的 QUIT 失败不视为发送失败，避免对已接受的邮件重复发送。SMTP 接受邮件仍不保证最终进入收件箱，需要同时检查垃圾邮件和服务商投递状态。

## 验证结果与边界

```powershell
go test -buildvcs=false ./internal/adapter/email ./internal/config ./internal/adapter/http
```

本地模拟 SMTP 测试已通过 TLS、STARTTLS、PLAIN、LOGIN、明文本地无认证、认证拒绝、收件人拒绝、DATA 拒绝、超时取消和日志脱敏；HTTP 回归包括注册、密码重置与邮件错误码。

当前工作区没有实际 SMTP 配置，也未取得服务商报错、测试收件人或真实投递记录。本次未向真实邮箱发送邮件，因此以上是代码和模拟协议验证结果，不能代替部署后的真实收件验收。
