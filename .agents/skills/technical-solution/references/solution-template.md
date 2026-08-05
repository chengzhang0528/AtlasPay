# Review-First Technical Solution Template

Use this short outline by default. Do not expand it into a file-by-file recipe unless the reviewer approves the direction or explicitly asks for a coding-ready supplement.

## 1. Review Summary

State the recommended decision, scope, and whether the plan is ready to code.

## 2. Decision Table

| Capability / outcome | Can do now | Change type | Frontend | API / service owner | Data/schema impact | Review choices |
|---|---|---|---|---|---|---|

Use only evidence needed to support each decision. For data/schema impact, say `no table change`, `reuse existing table`, `schema migration likely`, or `new table likely`; name a table only when verified. Every unresolved product rule must become two or three labelled choices with one recommendation.

## 3. Recommended Order and Boundaries

- Ordered slices with the one prerequisite that matters
- Explicit rejected/non-goal behavior
- Questions that require reviewer direction

## 4. Evidence and Verification

- Key verified sources and material assumptions
- Proportionate validation for each approved slice

---

# Coding-Ready Supplement

Add these sections only after approval or an explicit implementation request:

1. Goal and Scope
2. Facts and Sources
3. Plan and Change Boundary
4. Agent Constraints
5. Success Criteria
6. Stop Implementation Conditions
7. Verification
8. Open Questions and Non-Goals

Use exact paths, routes, contracts, tables, jobs, and owners only when verified. State observable UI, backend, API, CLI, artifact, or database outcomes; concrete validation commands; and all exclusions.
