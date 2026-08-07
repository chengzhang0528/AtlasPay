# AtlasPay 根任务总控

Status: Active
Kind: TaskControl
Scope: AtlasPay workspace / 未完成任务
Owner: Codex
Updated: 2026-08-07
Depends On:
- ../AGENTS.md

这是未完成任务的唯一事实源。完成项从当前队列移除；只有结果会影响后续选择时，才向完成事实卷追加一行。

## 当前队列

当前无未完成任务。

## 候选积压

当前无候选积压。

## 长期触发

| ID | 状态 | 触发条件 | 下一可验证结果 | 入口 |
|---|---|---|---|---|
| MERIDIAN-ALIPAY-REAL-ACCEPTANCE | Backlog | 用户准备配合真实支付宝测试 | 使用 git-ignored 凭据完成人工付款、异步通知、主动查询与退款闭环验收。 | `文档/项目/项目_meridian-gateway/Meridian_Gateway_CurrentDesign.md` |
| MERIDIAN-DOCKER-PERSISTENCE-ACCEPTANCE | Backlog | 本机或 CI 具备可用 Docker 环境 | 使用持久卷完成容器启动、重启与 SQLite 数据保留 smoke。 | `文档/项目/项目_meridian-gateway/Meridian_Gateway_CurrentDesign.md` |

## 维护规则

- `InProgress`、`Ready`、`Review`、`Blocked` 只放当前队列；`Backlog` 只放候选积压或长期触发。
- 受控变更、跨会话工作、持续阻断或活动 ChangePlan/Issue 必须登记；即时变更不得为了留痕登记。
- 每项任务只保留一个下一可验证结果和一个主要入口，不记录聊天、命令流水或已完成快照。
- 正式入口、任务绑定或活动文档变化后运行 `npm run check:docs`。
