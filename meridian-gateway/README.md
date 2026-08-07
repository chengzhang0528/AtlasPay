# Meridian Gateway

Meridian Gateway 是商户自托管的支付连接服务。一个 Go 进程提供管理端、收银台、REST API、OpenAPI、SQLite 持久化和后台 Webhook 投递。

## 在 Windows 本机启动

前置条件：Go 1.24 或更高版本、PowerShell 7；从本目录执行命令。

```powershell
.\scripts\local.ps1 -Action start
.\scripts\local.ps1 -Action status
.\scripts\local.ps1 -Action credentials
```

`status` 应显示 `running` 和本机 URL。用 `credentials` 输出的一次性本机管理令牌登录 `http://127.0.0.1:8080/admin`；健康检查和 OpenAPI 分别位于 `/healthz`、`/openapi.json`。随机主密钥、管理令牌、数据库和日志只保存在 git-ignored 的 `.local/`。

停止或重新构建服务：

```powershell
.\scripts\local.ps1 -Action stop
.\scripts\local.ps1 -Action start
```

## 用 Docker Compose 运行

前置条件：Docker Engine 已启动。从本目录复制 `.env.example` 为 `deploy/.env`，把两个示例值替换为独立生成的 32 字节 Base64 主密钥和至少 24 字符的随机管理令牌，然后执行：

```powershell
docker compose --env-file deploy/.env -f deploy/compose.yaml up --build -d
docker compose --env-file deploy/.env -f deploy/compose.yaml ps
```

成功时容器健康且管理端只绑定宿主机 `127.0.0.1:8080`；数据保存在 `meridian-data` 卷。停止容器但保留数据：

```powershell
docker compose --env-file deploy/.env -f deploy/compose.yaml down
```

不要使用 `down --volumes`，除非明确要删除全部 Meridian 数据。

## 配置支付宝

先在管理端创建商户和应用，再在“配置”中选择支付宝沙箱或正式环境，并录入商户自己的 App ID、应用 RSA2 私钥和支付宝 RSA2 公钥。系统使用对应的支付宝官方网关，并从 `MERIDIAN_BASE_URL` 生成 Notify URL；该地址必须能被支付宝公网访问。凭证会使用 `MERIDIAN_MASTER_KEY` 加密；不要把私钥、API Key、Webhook Secret 或真实 `.env` 提交到 Git。

没有支付宝沙箱或正式凭据时，仍可完整验收静态收款码、人工确认、费用、审计、Webhook Outbox 和 CSV 对账；动态二维码、网页/WAP 支付、真实通知与退款必须用商户自己的支付宝环境验收。

## 修改后验证

```powershell
go test ./...
go vet ./...
go build ./cmd/server
node --check internal/httpapi/web/admin.js
node --check internal/httpapi/web/checkout.js
```

服务启动失败时查看 `.local/server.stderr.log`；API 失败返回 JSON 问题详情。生产部署应在服务前配置 HTTPS 反向代理，并限制管理端网络访问。
