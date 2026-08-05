# AtlasPay 根任务总控

Status: Active
Kind: TaskControl
Scope: AtlasPay workspace / 未完成任务
Owner: Codex
Updated: 2026-08-05
Depends On:
- ../AGENTS.md

这是未完成任务的唯一事实源。完成项从当前队列移除；只有结果会影响后续选择时，才向完成事实卷追加一行。

## 当前队列

| ID | 状态 | 范围 | 下一可验证结果 | 入口 |
|---|---|---|---|---|
| ATLASPAY-PROJECT-BASELINE-001 | Ready | workspace | 从实际提交的源码与构建入口识别首个项目 ID、源码根、消费者和定向验证命令，再建立项目级入口；不得预设技术栈。 | `AGENTS.md` |
| MERIDIAN-GATEWAY-MVP-001 | InProgress | meridian-gateway / V0.1-V0.3 | 单进程服务通过定向测试、构建和本机 UI/API smoke；真实支付宝自动支付仅以沙箱或正式凭据验收。 | `文档/工作空间/推进中/CP-0001-Meridian-Gateway-MVP.md` |

## 候选积压

当前无候选积压。

## 长期触发

当前无长期触发项。

## 维护规则

- `InProgress`、`Ready`、`Review`、`Blocked` 只放当前队列；`Backlog` 只放候选积压或长期触发。
- 受控变更、跨会话工作、持续阻断或活动 ChangePlan/Issue 必须登记；即时变更不得为了留痕登记。
- 每项任务只保留一个下一可验证结果和一个主要入口，不记录聊天、命令流水或已完成快照。
- 正式入口、任务绑定或活动文档变化后运行 `npm run check:docs`。
