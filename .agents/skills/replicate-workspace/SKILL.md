---
name: replicate-workspace
description: Use when initializing or repairing a document-driven workspace control plane and its grounded human entry from an existing AGENTS, TASK_CONTROL, StructureContract, Workflow, and stable skill model. Do not infer product design, stack, APIs, source skeletons, services, or implementation directories.
---

# Replicate Workspace

## Principle

Replicate only the operating model. Treat the reference workspace as governance evidence, never as the target's product truth. Keep the agent control plane, human task surface, and `.agents/skills/` method surface distinct.

## Isolation Gate

Before any write:

1. Resolve canonical reference and target roots with Git and `Resolve-Path`; stop if equal.
2. Capture scoped status baselines for both roots, list exact target-owned paths, and hash every colliding reference path plus every source skill being adapted. Keep the reference read-only.
3. Normalize every destination and assert it starts with the canonical target root. For Windows cross-root patches use absolute forward-slash paths and never mix roots in one patch.
4. Write one small target file first; immediately assert it exists under the target root, reference status/hashes are unchanged, and target status matches the expected path.

Preserve target `.git`, source roots, current tasks, user content, and concurrent edits. Never use reset, checkout, whole-commit revert, or a post-start commit as a substitute for the baseline.

## Control Plane

Create only the minimal `AGENTS.md`, `文档/` control plane, and `.agents/skills/` skills needed by verified triggers. Add a project entry only when a real project ID and source root are known. Add README and `/人类-文档/` only when a real human task or source entry exists. Never copy product contracts, credentials, logs, or generated artifacts.

## Stable Skill Migration

1. Classify each source skill by stable cross-project method versus project-specific product, service, provider, or deployment knowledge.
2. Copy only stable methods into `.agents/skills/<skill-name>/`; adapt references from `skills/` to `.agents/skills/` and remove reference-project names.
3. Preserve `SKILL.md`, optional `agents/openai.yaml`, and only required `scripts/`, `references/`, or `assets/`. Do not create skill README, changelog, installation guide, empty directory, or task report.
4. Validate each skill's frontmatter, directory name, UI metadata, local links, resource files, and scripts. Run the workspace `check:skills` and focused tests.
5. Record only the durable routing rule in `AGENTS.md` or `WORKSPACE_STRUCTURE.md`; keep one-time migration order in an Active ChangePlan and delete it at closeout.

## Workflow

Use one main Workflow. Controlled initialization or skill/checker migration requires one task row and, for technical changes, one Active WF-0002 ChangePlan depending on a durable owner. Execute the target WF-0004 after all checks pass; remove the migration task and plan, and archive one result only when it affects later choices.

## Stop Conditions

Stop when a path escapes the target, a reference baseline changes, an owner is ambiguous, a skill depends on target-specific facts not present, a required external authority is unavailable, or migration would change product/source behavior without authorization.

## Closeout

Report target root, control-plane and skill files, task/plan lifecycle, document moves or deletions, skill/script/doc validation, target status, and reference baseline preservation. Report any reference repair separately.
