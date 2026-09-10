# Knowledge Write-back V1

Knowledge write-back is an explicit, deterministic, local-only path for recording verified development context. It is not an agent launcher, model call, automatic router, commit hook, or Git publishing workflow.

## Agent contract

At the meaningful end of work, an agent may provide semantic content and choose one class:

- `NONE`: no durable change. The plan and apply path write zero bytes.
- `ACTIVE`: unfinished work, blocker, or next action. The record is routed to `docs/agent/plans/active/`.
- `DURABLE_RECORD`: a verified decision or audit. It is routed once to `docs/agent/decisions/` or `docs/agent/audits/`.
- `ACCEPTED_STATE`: a verified state update. It may update `docs/agent/CURRENT-STATE.md` only after every acceptance gate passes.

Use `NONE` when the task produced no durable project knowledge. Do not write back every task. The agent must not choose a destination path, assert accepted state, or supply credentials and raw remote URLs.

## Commands

```text
contextbridge writeback plan <project> --class none|active|durable_record|accepted_state --summary "..." [--json]
contextbridge writeback apply --proposal PATH [--json]
contextbridge knowledge doctor --project PATH [--json]
```

`plan` is read-only and emits a deterministic proposal. `apply` re-probes the registered canonical workspace and revalidates the proposal identity and Git worktree precondition. It writes only Context Bridge knowledge files; it never commits, pushes, fetches, pulls, repairs, prunes, garbage-collects, or updates the Git index.

## Acceptance gate

`ACCEPTED_STATE` requires all of the following at apply time: canonical workspace identity is fresh, Workspace Guard passes, repository identity is unchanged, the canonical default branch is checked out, HEAD is known, Git evidence has no errors, the verification contract is resolved, all required verification commands pass, and `CURRENT-STATE` provenance is valid and fresh with no contradiction. An `ACTIVE` proposal is never promoted heuristically. If the gate fails, the result is downgraded to `ACTIVE` or refused.

`knowledge doctor` performs read-only checks for missing routing files, malformed write-back metadata, duplicate write-back IDs, size-policy violations, and current-state provenance problems. It does not judge semantic correctness.
