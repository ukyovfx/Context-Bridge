# Context Bridge Agent Instructions

## Scope

This repository is exclusively for `ukyovfx/Context-Bridge`.

## Routing

Read `docs/agent/START-HERE.md` first, then load only the documentation relevant to the task. Treat code, tests, CI, and runtime evidence as technical truth.

Before writing in a registered workspace, run and obey Workspace Guard. Stop on `WRONG_WORKSPACE`; there is no force override.

## Safety

- Keep the V1 core deterministic and free of model or API calls.
- Preserve Plan/apply separation and zero-mutation dry runs.
- Never weaken non-overridable path, repository, account, or secret-safety aborts.
- Do not expand beyond the implemented V1 core: Project/Repository/Workspace identity, exact Git-state binding, context integrity, Guard, verified handoff, and controlled project-truth write-back. Existing-repository integration is explicit and safety-gated. Do not add organization support, destructive Git operations, automatic AI-Knowledge writes, a GUI, MCP, a vector database, or an Obsidian plugin.
- Stop before production writes, permission changes, secret handling, history rewriting, or unresolved high-impact decisions.

## Verification

Run `go test ./...`, `gofmt`, `go vet ./...`, and `powershell -File scripts/e2e-local.ps1` for applicable changes. Never claim a check that was not run.

## Durable state

Update `docs/agent/CURRENT-STATE.md` only when verified durable project state changes.

## Knowledge write-back

At the meaningful end of a task, the agent may propose one write-back class: `NONE`, `ACTIVE`, `DURABLE_RECORD`, or `ACCEPTED_STATE`. Use `NONE` when no durable project knowledge changed; do not write a record for every task. The agent supplies only the semantic summary, status, blocker, or next action. Context Bridge owns project routing, identity, evidence, and refusal decisions. Use `contextbridge writeback plan` before `writeback apply`; apply revalidates the workspace and never commits or pushes. `ACCEPTED_STATE` requires fresh canonical identity, a passing Guard, unchanged repository identity, a known default-branch HEAD, resolved verification instructions, passing required commands, and no contradictions. If those conditions are not met, remain `ACTIVE` or refuse. `knowledge doctor` is read-only.
