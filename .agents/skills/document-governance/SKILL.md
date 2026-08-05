---
name: document-governance
description: Use for document governance, AGENTS and Workflow control planes, StructureContract changes, stale report cleanup, task alignment, document Kind classification, or reducing unnecessary agent hot-path context.
---

# Document Governance

## Goal

Keep only current facts, runnable entrypoints, necessary boundaries, and unresolved evidence. Documentation is a control plane, not a process archive.

## Load

1. Read root `AGENTS.md` and `文档/TASK_CONTROL.md`.
2. For a workspace change, read `文档/工作流/WORKFLOW_CONTRACT.md` and select one main Workflow; read WF-0004 before closeout.
3. Read the matched project entry and only the ProductContract, CurrentDesign, Runbook, or skill it routes to.
4. Read `文档/WORKSPACE_STRUCTURE.md` only when creating, moving, deleting, or classifying documents, or when ownership is unclear.

## Workflow

1. Classify every in-scope document by authoritative owner and primary consumer; choose keep, simplify, merge, move, link, or delete.
2. Put cross-project human tasks in `/人类-文档/`, project guidance beside its source, and agent routing or reusable methods in AGENTS, Workflow, or `.agents/skills/`.
3. Keep shared facts in their sole ProductContract, CurrentDesign, Decision, Runbook, Material, or source owner; expose them through links instead of copies.
4. Preserve the sole source of a current contract, design, decision, runnable procedure, safety constraint, material, or unresolved Issue. Delete absorbed plans, reports, placeholders, and stale navigation without tombstones.
5. Classify execution strength only through `WORKFLOW_CONTRACT.md`. Put controlled changes, blockers, and every active ChangePlan/Issue in `TASK_CONTROL.md`.
6. Repair inbound links, `Depends On`, source comments, task entries, and skill references before deleting or moving a path.

## Writing Rules

- Keep only current boundary, logic, invariant, failure/compatibility behavior, verification, and the next executable condition.
- Keep agent entries as short conditional routers plus non-negotiable gates. Do not put human tutorials into default agent context.
- Product behavior belongs to ProductContract; capability design to CurrentDesign; irreversible choices to Decision; repeated operations to Runbook; specialist methods to `.agents/skills/`.
- ChangePlan records one unfinished A-to-B change and is deleted after verified reconciliation. Issue exists only while a cross-session problem remains.
- Read and write UTF-8. Never copy secrets, logs, customer data, or generated artifacts.

## Validate and Close

When formal documents or governance entrypoints change, run `npm run check:docs`, `npm run check:skills` when skills changed, targeted inbound-reference scans, and scoped `git diff --check`. Execute WF-0004 conditionally: reconcile real long-lived facts, remove activity entries only when they exist, and archive only a registered result that affects later choices.
