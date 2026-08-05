---
name: align-solution-direction
description: Use before producing or materially revising a solution, technical solution, architecture proposal, implementation, refactor, integration, migration plan, rollout plan, or reviewer decision. Calibrate value, reachability, ownership, compatibility, cost, verification, and reviewability before planning.
---

# Align Solution Direction

## Goal

Prevent direction rework without adding a second plan narrative. Decide what belongs in the current phase, what to remove or defer, and whether a missing choice truly blocks the solution.

If direction is explicit, approved, and supported by current facts, pass silently. After a pass or correction, use `../technical-solution/SKILL.md` only when a technical solution or coding-ready plan is required.

## Establish Authority

1. Use the current explicit user decision as the desired outcome.
2. Use source, types, migrations, tests, and the routed formal owner as current-state evidence and hard constraints.
3. Keep earlier explicit decisions locked unless the current request supersedes them or verified evidence makes them impossible.
4. Treat conversation history as decision evidence, never as product or code truth.

Read root entries, the current task, the matched project entry, and only sources needed to resolve direction-changing facts. Follow the workspace Workflow only for workspace changes or cross-session recovery.

## Preflight

1. State the observable result needed now and the unfinished delta. Remove completed work and process history from active scope.
2. Prove value and reachability. Prefer reuse, deletion, configuration, or `do not build` when they meet the result with less lasting cost. Keep user acceptance scenarios only when a supported entrypoint can reach them; move fault injection into focused tests.
3. Preserve ownership. Require evidence before adding a table, API, DTO, cache, audit path, worker, container, or UI state. Decide placement from isolation, lifecycle, query, and consumer needs only when current flows make it direction-changing.
4. Make compatibility and cost explicit. Require backward compatibility only for a released contract, supported upgrade path, retained data, or explicit promise. Reject speculative scale machinery and low-value permanent complexity.
5. Choose the fastest verification that covers the retained risk: focused tests, build, lint, API/database checks, or minimal smoke. Use a full suite only when the workspace gate requires it.

## Decide

- **Silent pass**: continue without repeating the direction brief.
- **Correct and continue**: remove unnecessary, unreachable, duplicated, mistimed, or misowned scope; show only changed decisions as `item / action / reason`.
- **Stop for decision**: stop only when alternatives materially change product semantics, security, persisted data, public compatibility, or ownership. Present two or three exclusive choices and recommend one with its consequence.

Do not repeat the impact matrix, file plan, or verification detail owned by `technical-solution`.

## Durable Outputs

Keep an active plan limited to the unfinished A-to-B change, current stop conditions, and acceptance route. Delete it after the phase is complete. Keep each durable document responsible for one lifecycle purpose; link optional concerns instead of embedding them.
