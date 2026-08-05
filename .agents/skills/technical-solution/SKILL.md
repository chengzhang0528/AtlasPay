---
name: technical-solution
description: Use after align-solution-direction when producing or revising a reviewer-first technical solution, implementation, refactor, integration, migration plan, staged engineering scheme, or coding handoff. Require grounded facts, explicit boundaries, observable success, stop conditions, verification, and non-goals.
---

# Technical Solution

## Gate

First use `../align-solution-direction/SKILL.md`. Carry only its passed or corrected scope, ownership, and review choices into this skill. If its preflight stops for a material decision, do not create a coding-ready solution.

Use this skill for a requested solution or controlled technical change under WF-0002. Immediate changes use the request, targeted fact discovery, source, types, and tests directly. Produce a coding-ready handoff only after approval or an explicit implementation request.

## Load

Read root entries, `文档/TASK_CONTROL.md`, `文档/工作流/WORKFLOW_CONTRACT.md`, WF-0002, the matched project entry, `references/solution-template.md`, and the actual durable formal owner selected by the task. Do not create a placeholder CurrentDesign when another owner or source already carries the facts.

## Review-First Format

Lead with one decision table:

| Capability / outcome | Can do now | Change type | Frontend | API / service owner | Data/schema impact | Review choices |
|---|---|---|---|---|---|---|

Use generic change types such as `frontend only`, `existing API wiring`, `new application API`, `shared package contract`, `schema/migration`, or `do not build`. Group related actions by capability, state the recommended order and the prerequisite that matters, and separate ready work from decisions that block it.

Every unresolved product rule becomes two or three labelled choices with one recommendation. State `no table change`, `reuse existing table`, `schema migration likely`, or `new table likely` for every capability. Name an existing table only when verified.

List rejected or non-goal legacy behavior explicitly so review does not reopen it accidentally.

## Coding-Ready Supplement

Include Goal and Scope, Facts and Sources, Plan and Change Boundary, Agent Constraints, Success Criteria, Stop Implementation Conditions, Verification, and Open Questions/Non-Goals. Separate verified facts from assumptions and state phase boundaries, prerequisites, consumer impact, and downstream handoff.

## Coding-Ready Gate

Do not hand the plan to coding unless allowed and prohibited surfaces are explicit; critical behavior is grounded; success is observable through named UI actions, tests, APIs, CLI output, artifacts, or database checks; validation commands are concrete; missing facts, unavailable access, unauthorized expansion, and unresolved product choices are stop conditions; and non-goals prevent scope drift.

Before coding a controlled technical change, register the unfinished task and save exactly one `Kind: ChangePlan`, `Workflow: WF-0002` document under the workspace or project `推进中/` directory. The plan must depend on an actual durable formal owner and state its A-to-B delta. CurrentDesign is required only when capability design changes.

## Closeout

Keep non-blocking questions separate from stop conditions. Before reporting completion, execute WF-0004 conditionally: reconcile changed owners, run `npm run check:docs` and `npm run check:skills` when applicable, delete completed activity documents, and archive only a registered result that affects later choices.
