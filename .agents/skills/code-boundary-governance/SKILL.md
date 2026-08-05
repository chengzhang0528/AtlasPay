---
name: code-boundary-governance
description: Use when reviewing or changing backend Repository, Manager, AppService, Controller, Endpoint, DI, application contracts, data-access, or provider boundaries. Trigger for architecture reviews, refactors, dependency-injection issues, or suspected duplicated infrastructure.
---

# Code Boundary Governance

## Goal

Place each behavior at the lowest correct owner without leaking transport, product orchestration, persistence, provider, or shared-infrastructure concerns across layers.

## Load

1. Read root entries, the matched project entry, current task, and touched code/tests.
2. Read `references/layer-boundary-checklist.md` for the general review.
3. Stop scanning when the public surface, one request path, dependency registrations, tests, and current owner are clear.

## Workflow

1. Identify the public surface and trace one request through endpoint/controller, application service, manager/domain service, repository/adapter, and provider when those layers exist.
2. Classify each dependency as `allowed`, `thin adapter`, `boundary leak`, `responsibility drift`, or `infrastructure duplication`.
3. Move only misplaced behavior: protocol to transport, product use case to application service, reusable invariant to domain service, persistence mapping to repository/adapter, provider mechanics to infrastructure, and cross-cutting infrastructure to its shared owner.
4. Identify consumers before changing a public contract, permission, schema, provider contract, or shared package.
5. Add the smallest architecture, contract, integration, or real-database guard that proves the repaired boundary.

## Hard Gates

- Public contracts contain DTOs and application interfaces, not repositories, stores, contexts, dialects, or internal query plans.
- HTTP or transport code does not perform business orchestration or inject provider connections for ordinary workflows.
- Application services do not emit SQL or own provider/query/security algorithms.
- Required security, runtime, and persistence dependencies fail fast.
- Provider-neutral contracts do not expose SQL fragments, quoted identifiers, connection types, or provider syntax.
- Product-specific context stays in structured filters, facts, grants, predicates, or thin adapters.

## Verify and Close

Run scoped static scans plus affected architecture/build/test/real-database checks and `git diff --check`. Report the request path, classifications, files changed, validations, and only unfinished task IDs.
