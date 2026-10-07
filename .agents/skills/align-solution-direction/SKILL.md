---
name: align-solution-direction
description: Use before producing or materially revising a solution, technical solution, architecture proposal, implementation, refactor, integration, migration plan, rollout plan, or reviewer decision. Calibrate value, reachability, ownership, compatibility, cost, verification, and reviewability before planning. Independently check user and agent premises, missing variables, costs, and reasoning bias.
---

# Align Solution Direction

## Goal

Prevent direction rework without adding a second plan narrative. Decide what belongs in the current phase, what to remove or defer, and whether a missing choice truly blocks the solution. Judge user and agent claims by their evidence, regardless of who proposed them. Agree when supported and explain disagreement when warranted; do not manufacture objections to appear independent.

If direction is explicit, approved, and supported by current facts, pass silently. After a pass or correction, use `../technical-solution/SKILL.md` only when a technical solution or coding-ready plan is required.

## Establish Authority

1. Use the current explicit user decision as the desired outcome. Preserve the user's goals and valid preferences while verifying factual or causal claims embedded in the request.
2. Use source, types, migrations, tests, and the routed formal owner as current-state evidence and hard constraints.
3. Keep earlier explicit decisions locked unless the current request supersedes them or verified evidence makes them impossible.
4. Treat conversation history as decision evidence, never as product or code truth.
5. Before a consequential decision, check user and agent premises for factual errors, logical leaps, and missing information that could change the decision. Verify discoverable facts before asking the user. Ask only when alternatives materially change the outcome or ownership and cannot be resolved from current authority.
6. For consequential claims about data, people, versions, or conclusions, use traceable sources appropriate to the claim. Check freshness and scope; prefer source, types, and tests for code facts and authoritative primary sources for external facts. Distinguish observations, estimates, and inferences. Mark unresolved facts as uncertain rather than treating them as true or false.

Read root entries, the current task, the matched project entry, and only sources needed to resolve direction-changing facts. Follow the workspace Workflow only for workspace changes or cross-session recovery.

## Preflight

Actively identify omitted variables, consumers, dependencies, and implementation, maintenance, or operating costs that could change the decision. Consider relevant counterevidence and plausible alternatives. Check for anchoring, selective evidence, sunk-cost reasoning, or unjustified generalization when the argument supplies concrete evidence. Explain the reasoning error without attributing a psychological trait to the user.

1. State the observable result needed now and the unfinished delta. Remove completed work and process history from active scope.
2. Prove value and reachability. Prefer reuse, deletion, configuration, or `do not build` when they meet the result with less lasting cost. Keep user acceptance scenarios only when a supported entrypoint can reach them; move fault injection into focused tests.
3. Preserve ownership. Require evidence before adding a table, API, DTO, cache, audit path, worker, container, or UI state. Decide placement from isolation, lifecycle, query, and consumer needs only when current flows make it direction-changing.
4. Make compatibility and cost explicit. Require backward compatibility only for a released contract, supported upgrade path, retained data, or explicit promise. Reject speculative scale machinery and low-value permanent complexity.
5. Choose the fastest verification that covers the retained risk: focused tests, build, lint, API/database checks, or minimal smoke. Use a full suite only when the workspace gate requires it.

## Decide

- **Silent pass**: continue without repeating the direction brief.
- **Correct and continue**: correct faulty factual premises and inferences, remove unnecessary, unreachable, duplicated, mistimed, or misowned scope; show only changed decisions as `item / action / reason`. Identify the error directly, cite the relevant evidence, explain its concrete consequence, and offer a correction within the authorized outcome before continuing.
- **Stop for decision**: stop only when alternatives materially change product semantics, security, persisted data, public compatibility, or ownership. Present two or three exclusive choices and recommend one with its consequence.

Do not repeat the impact matrix, file plan, or verification detail owned by `technical-solution`.

Scope correctness, feasibility, risk severity, and completion claims to the available evidence, assumptions, and conditions. Separate observations from forecasts. Do not turn a sample into an exhaustive conclusion or a hypothetical risk into an observed incident.

## Durable Outputs

Keep an active plan limited to the unfinished A-to-B change, current stop conditions, and acceptance route. Delete it after the phase is complete. Keep each durable document responsible for one lifecycle purpose; link optional concerns instead of embedding them.
