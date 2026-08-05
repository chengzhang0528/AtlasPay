---
name: backend-detailed-testing
description: Use when a backend change needs executed unit, integration, contract, real-database, real-host, smoke, or regression evidence rather than an abstract test plan. Trigger for missing coverage, escaped regressions, or choosing the smallest evidence that proves a backend behavior.
---

# Backend Detailed Testing

## Goal

Prove changed behavior with the cheapest high-signal layer, escalating to real dependencies only when wiring, persistence, authentication, provider, or runtime risk requires it.

## Load

Read root entries, the matched project entry, changed contracts and code, existing tests/runners/fixtures, and the matched runbook. Read `references/detailed-test-playbook.md` when more than one layer is needed. Use `backend-realdb-api-testing` for assertion-backed real-database API verification.

## Workflow

1. Define changed behavior, public/data/security boundary, regression risk, and observable assertion.
2. Inventory existing test entrypoints with `rg`; extend the existing style instead of creating a parallel harness.
3. Select layers: unit for pure logic, integration for composition/persistence, contract for transport shape, real DB for provider/schema/transaction behavior, and real host for startup/middleware/full flow.
4. Run the narrowest tests first. Fix failures and rerun the exact failing command before widening.
5. Use isolated, workflow-owned dependencies for real tests. Record variable names rather than secrets and preserve failed database/report identity before cleanup.
6. Run the final scoped suite plus the smallest broader guard that protects consumers.

## Safety

- Do not claim a real test when dependencies are mocked or side effects are not asserted.
- Do not weaken assertions to make a suite pass.
- Do not delete databases, reports, or logs without proving the current workflow owns them.
- Put unresolved repair work in `文档/TASK_CONTROL.md`, not a one-off process document.

## Closeout

Report scope, exact commands, results, changed tests/runners, retained evidence, skipped coverage with reason, residual risk, and remaining task IDs. Run `git diff --check` for repository changes.
