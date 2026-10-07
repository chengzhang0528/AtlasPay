# AtlasPay 工作空间智能体入口

Status: Active
Scope: AtlasPay workspace
Owner: Codex
Updated: 2026-10-07
Depends On:
- none

本文件只定义最短进入路径和全局门禁。当前任务、目录与知识位置、治理动作分别以 `文档/TASK_CONTROL.md`、`文档/WORKSPACE_STRUCTURE.md` 和 `文档/工作流/WORKFLOW_CONTRACT.md` 为准；产品行为和现有实现以目标源码、类型、迁移与自动化测试为准。

## 交互约束

- 不发送非必要过程消息。
- 涉及代码时，先一次性完成范围内的代码变更和白盒测试，期间不得重启正在运行的服务；仅在后续验证确需重启时使用仓库内已验证的受管入口快速重启并立即验证。
- 同一错误连续出现两次时，先联网查证并列出 3-5 种可行修复，再选择符合当前边界的最高效方案，不继续盲试。

## 最短启动路径

1. 先读 `文档/TASK_CONTROL.md`，确认是否已有同目标未完成任务；是否登记由 Workflow 的执行强度决定。
2. 再读命中项目的 `文档/项目/项目_<id>/AGENTS.md`；没有已验证项目 ID 和源码根时不得创建项目入口。
3. 请求会改变工作空间或需跨会话继续时，读 `文档/工作流/WORKFLOW_CONTRACT.md` 并只选择一个主 Workflow；所有变更收口前读取 `文档/工作流/WF-0004-任务收口.md`。
4. 涉及代码时，读取目标源码根 `AGENTS.md`、类型与测试；先用 `rg` 查现有实现，不默认读取面向人的 README 或遍历整个 `文档/`。
5. 创建、移动、删除文档或无法判断知识位置时才读 `文档/WORKSPACE_STRUCTURE.md`；归档只供追溯，不是当前规范来源。

## 专业技能路由

| 触发 | 必读 |
|---|---|
| 解决方案、架构、实施、接入、迁移或重构方案 | 先读 `.agents/skills/align-solution-direction/SKILL.md`；需要编码级方案再读 `.agents/skills/technical-solution/SKILL.md` |
| 文档、入口、控制面或任务状态治理 | `.agents/skills/document-governance/SKILL.md` |
| 面向开发者的人类文档或 README | `.agents/skills/human-documentation/SKILL.md` |
| 后端分层测试或真实数据库 API 验证 | `.agents/skills/backend-detailed-testing/SKILL.md`；命中真实数据库再读 `.agents/skills/backend-realdb-api-testing/SKILL.md` |
| Repository、Manager、AppService、Controller、Endpoint、DI 或数据访问边界 | `.agents/skills/code-boundary-governance/SKILL.md` |
| 复刻或修复文档驱动工作空间 | `.agents/skills/replicate-workspace/SKILL.md` |
| 独立工作完成、验证通过且改动归属精确 | `.agents/skills/git-closeout/SKILL.md` |

## 事实源优先级

- 待实现目标：用户当前要求；已登记任务再结合 `TASK_CONTROL.md` 与对应 Active ChangePlan。
- 已有行为：源码、类型、数据库迁移和自动化测试。
- 产品、设计和部署：职责唯一的 ProductContract、CurrentDesign、Decision 与 Runbook。
- 历史：Git 与完成事实归档，只供追溯，不覆盖当前来源。

来源冲突时停止扩张范围，查明并更新唯一事实所有者；不得用旧任务、评审或报告推断当前业务规则。

## 工程与文档门禁

- 对用户与智能体自己的关键前提保持独立判断；按[方向对齐方法](.agents/skills/align-solution-direction/SKILL.md)核实事实与推理，直接说明有依据的错误、遗漏及其影响，不把目标、偏好或假设当作事实。
- 调用函数、组件、脚本或 API 前先查源码、类型、AGENTS 或正式文档；不凭经验编造契约。
- 公共包、DTO、数据库、权限和跨项目契约变更前识别全部消费者；保持最小改动，不夹带重构或无关格式化。
- 改动后运行与影响范围匹配的测试、构建、lint、smoke 或 doctor；已选择验证失败不得报告完成。
- 全量测试仅在用户、CI 或发布门禁明确要求，或影响消费者无法可靠枚举时运行；否则以定向测试、类型检查、构建、守卫和 smoke 覆盖风险，并在汇报中说明未选全量测试的原因。
- 只有受控变更、跨会话恢复、持续阻断或活动文档存在时登记 `TASK_CONTROL.md`；即时变更不为留痕创建任务、方案或归档项。
- 受控技术变更编码前必须有 WF-0002 绑定的 Active ChangePlan，明确基线、目标、边界、停止条件、可观察成功标准，并依赖至少一个实际长期事实所有者。
- 仅在存在长期事实增量时更新对应正式文档；受控变更完成后删除活动方案，不保留 Done 方案、重复索引、聊天过程或源码可直接检索的清单。
- 正式文档必须含 `Status`、`Kind`、`Scope`、`Owner`、`Updated`、`Depends On`，位置和生命周期只由 `文档/WORKSPACE_STRUCTURE.md` 定义。
- 正式文档、治理入口、任务绑定或活动文档变化时运行 `npm run check:docs`；纯源码、测试或样式即时变更不运行该检查。
- `.agents/skills/` 只保存稳定、可重复、可验证的方法；技能变化后运行 `npm run check:skills` 和命中的脚本测试，不把单次任务或产品事实写入技能。
- 独立工作完成且必要验证通过后，只通过 `.agents/skills/git-closeout/scripts/run-git-closeout.mjs` 精确暂存、提交、拉取检查和推送本任务文件。默认使用当前 `main`，发现并发改动或分叉即停止；除非用户明确指定，否则不创建分支或 worktree。

## 安全红线

- 不写入或提交真实密钥、连接串、API Key、JWT Secret、签名 URL、私钥、密码、客户原始数据、日志或生成物。
- 本机凭据只进入 git-ignored 配置；文档使用配置键名和占位符。
- 不提交 IDE 状态、`.agent` 运行状态、`node_modules`、`dist`、`bin`、`obj`、`.venv` 或 `target`。
- 不覆盖目标中已有或并发产生的改动；不使用 `reset --hard` 或 `checkout --` 清理他人工作。

## 收口汇报

最终汇报先给出唯一结论：`通过` 或 `不通过`。逐项列出验收结果与证据，并说明改动文件、受影响项目、已运行和未运行的必要验证及原因；只有实际触发任务控制或文档生命周期变化时才报告其增删、合并、归档或替换情况。任一验收失败、活动方案未关闭、命中的必要验证未执行或仍有阻断时，整体结论必须为 `不通过` 并给出落到具体文件或契约的修复建议。
