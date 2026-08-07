# Meridian Gateway 项目智能体入口

Status: Active
Kind: AgentEntry
Scope: Meridian Gateway project
Owner: Codex
Updated: 2026-08-05
Depends On:
- ../../工作空间/Meridian_Gateway_产品契约.md
- AGENTS.md

## 事实入口

- 源码根：`meridian-gateway/`
- 产品行为：`../../工作空间/Meridian_Gateway_产品契约.md`
- 当前能力设计：`Meridian_Gateway_CurrentDesign.md`
- 活动任务：`../../TASK_CONTROL.md`

## 源码路由

- `cmd/server/`：进程组合根、信号与 HTTP 主机。
- `internal/httpapi/`：路由、身份门禁、传输 DTO、安全响应头和嵌入式页面。
- `internal/gateway/`：支付、退款、渠道、管理用例和后台 worker 编排。
- `internal/domain/`：统一状态、方法、金额和模型不变量。
- `internal/storage/`：SQLite 迁移、仓储、事务、租户条件和 Outbox。
- `internal/alipay/`：支付宝 RSA2 请求、响应与异步通知适配。
- `internal/security/`：密钥生成、摘要、AES-256-GCM 和 Webhook HMAC。

## 定向验证

- 纯逻辑/合同：`go test ./internal/domain ./internal/security ./internal/alipay`
- 真实 SQLite：`go test ./internal/storage`
- 应用与真实 HTTP：`go test ./internal/gateway ./internal/httpapi`
- 影响范围全量：`go test ./...`
- 静态与构建：`go vet ./...`；`go build ./cmd/server`

真实支付宝验证只读取 git-ignored 本机配置；不得把密钥、原始渠道日志或客户数据写入源码、测试、文档或提交。
