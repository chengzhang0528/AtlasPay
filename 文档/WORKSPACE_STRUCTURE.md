# AtlasPay 工作空间结构与知识放置契约

Status: Active
Kind: StructureContract
Scope: AtlasPay workspace / 目录与知识放置
Owner: Codex
Updated: 2026-08-05
Depends On:
- ../AGENTS.md

本文是 AtlasPay 目录职责、文档类型和知识放置规则的唯一当前契约。根 `AGENTS.md` 负责准入、工程与安全门禁；本文只回答知识放在哪里、何时创建和何时删除，不推断产品、技术栈、服务或源码边界。

## 当前根目录职责

| 路径 | 当前职责 | 边界 |
|---|---|---|
| `.git/` | 版本历史与并发事实 | 不作为当前产品规范 |
| `.agents/` | Codex 工作空间级稳定技能与其必要脚本、引用和 UI 元数据 | 不保存产品事实、单次任务过程或项目专属方法 |
| `package.json` | 工作空间级可执行治理命令 | 不声明尚不存在的服务命令 |
| `文档/` | 任务、治理、长期事实与活动推进 | 不保存源码镜像、聊天、日志或生成物 |

尚无经源码验证的项目 ID、源码根或人类任务入口，因此当前不得创建 `文档/项目/项目_<id>/`、根 README 或 `人类-文档/`。发现真实项目后，在同一任务中增加项目 AgentEntry 和实际需要的正式文档位置；人类入口只从已验证命令、源码入口和任务创建。

## 受控文档位置

以下声明同时供人和 `check-docs.mjs` 读取。目录项以 `/` 结尾；逗号表示允许的 Markdown `Kind`。未声明位置不得新增正式 Markdown。

- `TASK_CONTROL.md` -> `TaskControl`
- `WORKSPACE_STRUCTURE.md` -> `StructureContract`
- `工作流/` -> `WorkflowContract,Workflow`
- `项目/项目_meridian-gateway/` -> `AgentEntry,ProductContract,CurrentDesign,Decision,Runbook,Material`
- `工作空间/` -> `ProductContract,CurrentDesign,Decision,Runbook,Material`
- `工作空间/归档/` -> `Archive`

## 文档状态协议

- `TaskControl`: `Active`
- `StructureContract`: `Active`
- `WorkflowContract`: `Active`
- `Workflow`: `Active`
- `AgentEntry`: `Active`
- `ProductContract`: `Active`
- `CurrentDesign`: `Active`
- `Decision`: `Proposed,Accepted,Rejected,Superseded`
- `Runbook`: `Active`
- `ChangePlan`: `Draft,Active`
- `Issue`: `Active`
- `Material`: `Active`
- `Archive`: `Active`

正式文档禁止以 `Done` 或 `Completed` 保存任务快照。`Active` 表示当前可消费事实或活动；`Draft` 不授权编码；决策结果只使用 Decision 的允许状态。

## 生命周期

| Kind | 产生与更新 | 退出 |
|---|---|---|
| `TaskControl` | 初始化一次；未完成目标、状态、验收或入口变化时更新 | 永不删除；完成项移除 |
| `StructureContract` | 建立控制面时创建；类型、位置或生命周期变化时更新 | 完整迁移到明确替代契约后才可删除 |
| `WorkflowContract/Workflow` | 稳定治理触发、Action、输入输出或停止条件变化时更新 | 工作流消失或合并后删除，不保留执行记录 |
| `AgentEntry` | 真实项目需要独立路由时创建 | 项目完全退出后删除 |
| `ProductContract` | 稳定产品对象、用户行为或业务规则缺少唯一所有者时创建 | 产品范围退出后迁移仍有效事实并删除 |
| `CurrentDesign` | 跨文件能力无法从源码低风险重建时创建 | 能力退出或合并后迁移唯一事实并删除 |
| `Decision` | 长期选择与后果需要追溯时创建 | 不删除；被取代时标记 `Superseded` |
| `Runbook` | 重复操作且执行错误有风险时创建 | 对应运行方式退出后删除 |
| `ChangePlan` | 已登记受控任务准备进入一次 A→B 技术变化时创建，绑定 WF-0002 并依赖实际长期所有者 | 验证通过且长期事实归位后删除 |
| `Issue` | 跨会话问题或阻断需要恢复入口时创建，绑定 WF-0003 | 阻断消失并更新任务后删除 |
| `Material` | 当前正式文档需要不可低成本重建的原始证据时创建 | 无当前消费方时删除 |
| `Archive` | 初始化时创建完成事实卷 | 过长时封卷，不承担当前规范 |

活动目录只在存在真实 ChangePlan 或 Issue 时声明和保留；项目级正式位置也只随真实项目与文档创建，不使用占位文件维持空目录。未来重新创建活动方案时，先在同一受控变更中声明实际的 `推进中/` 位置。

## Codex 技能边界

- 工作空间级技能位于 `.agents/skills/<skill-name>/`，目录名与 `SKILL.md` 的 `name` 一致，并只使用小写字母、数字和连字符。
- 每个技能必须包含 `SKILL.md`；`agents/openai.yaml` 保存 Codex UI 元数据，`scripts/`、`references/`、`assets/` 只在方法实际需要时创建。
- 技能只保存跨任务稳定、可重复、可验证的方法。产品规则属于 ProductContract，能力当前实现属于 CurrentDesign，单次执行顺序属于 ChangePlan，源码行为属于源码与测试。
- 不创建技能 README、安装指南、变更日志、空资源目录或参考项目专属副本；新增或更新技能后运行 `npm run check:skills` 与命中脚本测试。

## 放置与验证规则

- 正常路径是 `根 AGENTS -> TASK_CONTROL -> 项目 AGENTS -> 一个命中文档或方法 -> 源码与测试`；改变工作空间时再读取一个主 Workflow，收口读取 WF-0004。
- 产品行为属于 ProductContract，能力当前实现属于 CurrentDesign，长期不可逆选择属于 Decision，重复运行操作属于 Runbook，源码可查事实属于源码、类型、迁移和测试。
- 长期文档必须无法从源码或可靠正式来源低成本重建，生命周期长于当前任务，并有唯一所有者与更新触发。
- 移动或删除文档前修复所有入站链接、`Depends On`、任务入口和源码引用；`文档/` 下禁止 README。
- `文档/check-docs.mjs` 读取本文校验位置、Kind、状态、Workflow/Action、任务与活动绑定、依赖、链接、UTF-8、秘密模式和热路径篇幅。
- `文档/check-skills.mjs` 校验 `.agents/skills/` 的目录名、frontmatter、UI 元数据、允许资源和本地引用。
- 正式文档或治理入口变化后运行 `npm run check:docs`；技能变化后运行 `npm run check:skills`；校验失败不得收口。

## 非目标

本文不维护源码文件清单、代码架构细节、产品规则、逐文档索引、聊天过程或测试输出，也不批准新增服务、源码根、部署单元或外部依赖。
