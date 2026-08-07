# Meridian Gateway CurrentDesign

Status: Active
Kind: CurrentDesign
Scope: Meridian Gateway / current implementation
Owner: Codex
Updated: 2026-08-07
Depends On:
- ./AGENTS.md
- ../../工作空间/Meridian_Gateway_产品契约.md

## 运行边界

`cmd/server` 组合一个 Go 进程：同一 HTTP 主机提供商户 API、本机管理端、消费者收银台、OpenAPI、支付宝通知入口和健康检查；同一进程内的 worker 从 SQLite Outbox 领取商户事件并执行重试。默认数据库是持久卷中的 SQLite WAL，不存在 Redis、MQ 或第二服务。

必要配置是 `MERIDIAN_MASTER_KEY`、`MERIDIAN_ADMIN_TOKEN`、`MERIDIAN_DATABASE_PATH`、`MERIDIAN_BASE_URL` 和监听地址。主密钥只用于 AES-256-GCM；全局管理令牌只在运行环境存在。系统管理员签发的单商户管理令牌只存 SHA-256 摘要。`scripts/local.ps1` 把本机随机配置和数据库放在 git-ignored `.local/`。

## 请求路径

商户 `Bearer API Key` 先由应用服务认证为 Application，再调用统一支付用例。HTTP 不引用仓储或 SQL；`gateway.Service` 编排用例，`storage.Store` 独占 SQLite 事务和映射，`alipay.Client` 独占渠道协议和 RSA2。

`/admin/api` 门禁先识别全局管理令牌或单商户管理令牌。全局身份可列出和创建商户、签发商户令牌；单商户身份的列表固定为自身，所有带 `{merchant}` 的管理路径在进入 handler 前执行范围校验，跨商户请求返回 403。应用 API Key 继续只认证商户 REST API，不进入浏览器管理会话。

静态支付在创建时进入 pending 并绑定不可猜测收银令牌。人工确认由一个 SQLite 事务同时推进 paid、写入费率快照、审计和 `payment.paid` Outbox。重复确认不会重复产生副作用。

动态二维码、电脑网站和手机网站支付先持久化 created 订单，再以平台 payment ID 作为支付宝 `out_trade_no`。成功预创建或签名跳转后进入 pending；失败保留标准化错误。支付宝通知先按订单找到商户配置，再验签并核对 app_id、订单、金额和 trade_no；成功通知或查询在同一事务中保存 `trade_no`、结算、费用和 Outbox。仅本地过期不会丢弃已验签成功，渠道已确认关闭的订单才保持 closed；重复与终态乱序事件不重复计费或重开订单。

电脑网站和手机网站收银台把已签名字段提交到同源 `/checkout/{token}/redirect`；HTTP 层按订单中保存的签名字段精确拒绝缺失、重复、额外或篡改输入，并拒绝终态或本地过期订单，支付宝适配器再向固定官方网关 POST，并只接受支付宝官方 HTTPS 子域返回的重定向。收银页轮询返回 `payable`，在支付完成、退款、关闭、过期、浏览器后退恢复或重新可见时立即隐藏二维码和跳转表单。未配置商户 return_url 时，签名请求使用 `/result?payment_id=...` 只读结果页。

管理员为每个商户选择支付宝官方 `sandbox` 或 `production` 环境，并录入 App ID、应用 RSA2 私钥和支付宝 RSA2 公钥。`gateway.Service` 将环境映射为固定官方网关，并从 `MERIDIAN_BASE_URL` 派生 `/v1/webhooks/alipay`；HTTP 不接受自定义网关、手工 Notify URL 或证书字段。支付宝包的测试可直接注入 `httptest` 地址，但该入口不属于运行时配置。

退款先在事务中校验累计金额、同号幂等记录和累计上限，再进入 refunding，并在调用渠道前写入恢复期限。支付宝 `out_request_no` 固定为 merchant_refund_id；明确成功推进 partially_refunded/refunded 并生成 `refund.updated` Outbox，明确业务拒绝恢复到 paid 或 partially_refunded，传输或响应结果未知则保持 pending/refunding。worker 到期后先调用退款查询，查询不存在时才同号重试，次数和下次尝试时间持久化并按最多一小时的指数退避推进。静态码订单不提供自动退款。

过期 worker 只领取 `close_next_attempt_at` 已到期的订单，先查询渠道：已支付则结算，已关闭或 `ACQ.TRADE_NOT_EXIST` 则本地关闭，其他关闭失败写入 `payment_attempts` 并更新持久化指数退避。默认两秒 tick 不再等于渠道重试频率。

## 数据与安全

核心表为 merchants、applications、channel_accounts、static_codes、payments、payment_attempts、fees、audit_logs、outbox_deliveries、channel_events 和 refunds。`merchants.admin_token_hash` 保存可空的商户管理令牌摘要，版本 2 迁移会为既有 SQLite 无损补列并建立唯一索引。租户资源仓储方法显式接收 merchant_id；商户 API 还校验 Application 所有权。

商户管理令牌和 API Key 只存 SHA-256 摘要。应用 Webhook 密钥和支付宝凭证以带记录 AAD 的 AES-256-GCM 密文保存。应用更新、凭证轮换、商户令牌轮换和投递重试与审计写入同一事务；轮换立即替换旧摘要或密文。Webhook 对 `timestamp.raw_body` 使用 HMAC-SHA256，并携带事件、投递 ID 和时间戳。管理端默认只接受回环地址；Docker 网桥场景必须显式关闭该门禁并把宿主端口绑定到回环地址。

## 可观察入口

- `/healthz`：进程健康。
- `/openapi.json`：商户 API 契约。
- `/admin`：全局管理员或单商户管理员进入商户、应用、支付宝账户、静态码、订单、费用、退款、投递、审计和对账；商户身份不显示跨商户动作。
- `/checkout/{token}`：静态码、动态码或支付跳转收银页，只轮询只读状态。
- `/v1/webhooks/alipay`：支付宝签名通知，成功返回纯文本 `success`。

## 当前外部条件

支付宝 RSA2 公钥模式已有请求、同步响应、通知、退款查询和官方沙箱/正式环境映射的合同测试；真实 SQLite、Service、worker 和 HTTP 测试覆盖过期到账、关闭退避、退款未知恢复、同号幂等和终态收银台门禁。本机 git-ignored 沙箱凭据与公网 `MERIDIAN_BASE_URL` 已验证电脑网站支付可进入支付宝官方沙箱收银页；真实付款成功、异步通知、主动查询、退款和容器持久化由后续外部验收触发，本次白盒完成不代表这些真实渠道或容器验证通过。
