# Context Bridge Agent Instructions

## Scope

This repository is exclusively for `ukyovfx/Context-Bridge`.

## Routing

Read `docs/agent/START-HERE.md` first, then load only the documentation relevant to the task. Treat code, tests, CI, and runtime evidence as technical truth.

## Safety

- Keep the V1 core deterministic and free of model or API calls.
- Preserve Plan/apply separation and zero-mutation dry runs.
- Never weaken non-overridable path, repository, account, or secret-safety aborts.
- Do not add existing-repository adoption, organization support, destructive Git operations, automatic AI-Knowledge writes, a GUI, MCP, a vector database, or an Obsidian plugin in V1.
- Stop before production writes, permission changes, secret handling, history rewriting, or unresolved high-impact decisions.

## Verification

Run `go test ./...`, `gofmt`, `go vet ./...`, and `powershell -File scripts/e2e-local.ps1` for applicable changes. Never claim a check that was not run.

## Durable state

Update `docs/agent/CURRENT-STATE.md` only when verified durable project state changes.
