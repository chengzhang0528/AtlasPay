# Meridian Gateway Source Entry

Scope: `meridian-gateway/`
Owner: Codex
Updated: 2026-08-05
Depends On:
- `../文档/项目/项目_meridian-gateway/AGENTS.md`

- Keep HTTP binding in `internal/httpapi`, use-case orchestration in `internal/gateway`, invariants in `internal/domain`, SQL/transactions in `internal/storage`, and Alipay protocol code in `internal/alipay`.
- Run focused tests first, then `go test ./...`, `go vet ./...`, and `go build ./cmd/server` for shared changes.
- Do not start or restart the service until the scoped code and white-box tests are complete.
- Keep runtime credentials and databases under ignored `.local/` or deployment secrets; never commit them.
- A browser return, screenshot, countdown, or unsigned callback must never mark a payment paid.
