---
name: git-closeout
description: Use automatically after one independent workspace work item is complete, all selected validations pass, task and document lifecycle cleanup is finished, and changed files have exact ownership. Run the child-process closeout script to stage, commit, synchronize, and push only those files.
---

# Git Closeout

## Goal

Turn one verified independent result into one exact commit on the current branch and push it without absorbing another task's changes. This is a completion gate, not a recovery, merge, release, or history-rewrite tool.

Use `.agents/skills/git-closeout/scripts/run-git-closeout.mjs` as the only path for `git add`, `git commit`, `git fetch`, and `git push`. Invoke it with Node so Git operations run in a dedicated child process. Do not reproduce those write operations manually.

## Decide the Scenario

| Scenario | Action |
|---|---|
| Complete work, validations passed, lifecycle cleanup complete, exact ownership | Run `commit-push` before final report. |
| Complete work with no repository change | Return `skip_no_changes`; do not create an empty commit. |
| User explicitly forbids commit or push | Skip and report the override. |
| Validation failed, required validation missing, or Active ChangePlan/task remains | Stop before Git mutation. |
| Unrelated disjoint changes are unstaged | Keep them unstaged and pass owned files individually. |
| Owned file mixes tasks or ownership is uncertain | Stop; do not stage the whole file or guess hunks. |
| Any content was already staged | Stop; do not unstage, amend, or include it. |
| Branch, linked worktree, or remote target differs from default | Stop unless the current request explicitly authorizes that existing target. Never create one. |
| Local and remote history are not synchronized | Stop; do not merge, rebase, reset, or force push. |
| The repository is completely new and both local HEAD and remote target branch are absent | The target runner may create the first exact commit only when the remote has no other heads; otherwise stop. |

## Build the Owned Path Set

1. Start from files created, changed, deleted, or renamed for this work item.
2. Include lifecycle edits such as task removal, plan deletion, and durable owner update.
3. Review `git diff -- <path>` and inspect untracked file content for every selected file.
4. Pass files individually with repeated `--path`; never pass directories, globs, `.`, or the repository root.
5. Never include secrets, credentials, logs, generated output, dependency directories, IDE state, or agent runtime state.

Any pre-existing staged file is an ambiguous ownership state and must stop the run.

## Run the Child Process

```powershell
node .agents/skills/git-closeout/scripts/run-git-closeout.mjs `
  --mode commit-push `
  --repo . `
  --message "chore: close out verified workspace change" `
  --verification "npm run test:docs; npm run check:docs; npm run check:skills; git diff --check -- <owned paths>" `
  --path AGENTS.md `
  --path .agents/skills/git-closeout/SKILL.md
```

The default target is `origin/main`, and the checkout must be the primary checkout of `main`. Only an explicitly requested existing branch or linked worktree may be passed through the corresponding option. The runner never creates a branch or worktree.

Use `--mode check` to diagnose readiness without staging or committing. A successful check is not completion; rerun `commit-push` after all lifecycle edits are final.

## Interpret the Result

- Exit `0`: `pushed`, `ready`, or `skip_no_changes`.
- Exit `2`: controlled stop before commit, such as invalid ownership, existing staged content, wrong checkout, or divergence.
- Exit `3`: a local commit exists but synchronization or push did not complete; report its SHA and do not rewrite history.
- Exit `1`: invalid invocation or unexpected runner failure.

Report completion only for `pushed`, explicit `skip_no_changes`, or a user override.

## Verification Contract

Maintain scenario tests in `.agents/skills/git-closeout/scripts/run-git-closeout.test.mjs` covering exact commit/push with unrelated changes, empty-remote first commit, no-change skip, missing verification, pre-existing staged content, non-default branch, remote-ahead/divergent history, unsafe/non-file paths, and credential redaction. Run the focused tests, `npm run check:skills`, required document gate, and scoped whitespace check before using this skill.
