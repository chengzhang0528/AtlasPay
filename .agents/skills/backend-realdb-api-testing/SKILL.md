---
name: backend-realdb-api-testing
description: Use when backend APIs, package integrations, permissions, metadata, runtime CRUD/query, migrations, or provider behavior must be verified against a real database rather than mocks. Trigger for real database tests, assertion-backed API evidence, response plus side-effect checks, or production bugs missed by mocks.
---

# Backend Real Database API Testing

## Goal

Run the real host against an isolated database, assert transport behavior and persisted side effects, and retain enough safe evidence to reproduce failures.

## Load

Read root entries, the target project entry, current API/security/data contracts, the existing runner, and its runbook. Use `backend-detailed-testing` for the broader layer selection.

## Workflow

1. Run package or library tests for shared query, permission, audit, runtime, and provider behavior before app scenarios when those layers exist.
2. Obtain the management connection from an environment variable or ignored local config; never commit or print it.
3. Create a uniquely named database, apply foundation then product migration/seed, start the real host, and record the database name.
4. Cover relevant allow, deny, invalid-input, missing-context, tenant, concurrency, and audit paths; stop expanding when the changed risk is proven.
5. Assert status, stable error/header/body shape, data visibility, and database side effects. A denied write must prove no forbidden mutation.
6. Fail the runner on any assertion. Drop the database only after success; on failure retain the name and minimum reproduction according to the runbook.
7. Generate only the report format and location defined by the current runner or runbook.

## Gates

- A response recorder without assertions is not a test.
- Mocked repositories or in-memory fallbacks cannot support a real-database claim.
- Reports contain no passwords, tokens, API keys, or unmasked secrets.
- Test-only fixture writes stay inside the isolated runner and do not become production patterns.
- Cleanup touches only artifacts proven to belong to this workflow.

## Verify and Close

Run the narrow package, app unit/contract, real-database runner, and `git diff --check` checks selected by the changed risk. Report database lifecycle, report path, exact commands/results, skipped checks, residual risk, and remaining task IDs.
