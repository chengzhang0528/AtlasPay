# Detailed Backend Test Playbook

Use this reference when a change needs multiple test layers.

## Layer Selection

| Layer | Proves | Typical assertions |
|---|---|---|
| Unit | Pure logic, guards, mappings, state transitions | value, exception, DTO, rendered plan |
| Integration | Module/DI, adapter, service composition, persistence | injected behavior, persisted state, rollback |
| Contract | Endpoint and serialization contract | status, stable error, headers, payload shape |
| Real DB | Provider SQL, schema/seed, concurrency, real predicates | rows, side effects, transaction/provider behavior |
| Real host/smoke | Startup, config, middleware, full API/CLI path | health, end-to-end response, observable workflow |

Use the lowest layer that proves the behavior. Add a higher layer only for risk the lower layer cannot observe.

## Coverage Prompts

- Normal path plus relevant reject, missing-context, invalid-input, or concurrency path.
- Denied writes prove no mutation or forbidden audit payload.
- Contract tests assert stable status/error/header/body shape, not incidental formatting snapshots.
- Real tests use real auth/context and migration/seed order.
- Regression tests name escaped behavior and remain deterministic.

## Real Dependency Lifecycle

Use an isolated name such as `test_<project>_<date>_<time>_<random>` and respect provider limits. Before create or drop, confirm the name belongs to this run.

1. Inject connection sources through environment variables or ignored local config.
2. Create the isolated database/schema and apply migration/seed explicitly.
3. Start the runner or host and execute assertions.
4. On success, clean only workflow-owned temporary state.
5. On failure, record the database/report name and minimal reproduction before cleanup.

Never print passwords or tokens. Retention is allowed only when a current runbook defines ownership and deletion rules; otherwise leave cleanup explicit and conservative.

## Failure Handling

Keep the narrowest failing command and assertion. Classify the owner as production code, test code, seed/configuration, or documentation. Rerun after the fix. If unfinished, add or update one actionable task row.
