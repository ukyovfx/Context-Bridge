# Context Bridge

GPT ↔ Codex Development Context Bridge

Context Bridge is a deterministic, local-first CLI for creating development repositories with durable Markdown context and for verifying project, repository, and local workspace identity before Context Bridge-mediated writes.

## Install

Go 1.23 or newer is required to build from source.

```powershell
go install github.com/ukyovfx/Context-Bridge/cmd/contextbridge@latest
```

## Commands

```text
contextbridge setup [--dry-run]
contextbridge init <project> [--root PATH] [--owner USER] [--profile core|openai] [--local-only] [--dry-run]
contextbridge doctor [--project PATH]
contextbridge registry list
contextbridge registry show <project>
contextbridge registry register <path> [--role canonical|alternate] [--dry-run]
contextbridge guard --project <id-or-alias> [--workspace <id-or-path>]
contextbridge discover --root <path> [--json]
contextbridge handoff <project> --task "<intent>" [--agent codex|claude|cursor] [--json]
contextbridge instructions --explain [--project PATH] [--agent codex|claude|cursor] [--json]
contextbridge adopt <path> [--dry-run] [--confirm] [--json]
contextbridge upgrade <path> [--dry-run] [--confirm] [--json]
contextbridge rebind <project> --workspace <path> [--dry-run] [--confirm] [--json]
contextbridge version
```

## Quick start

```powershell
go install github.com/ukyovfx/Context-Bridge/cmd/contextbridge@v1.2.0-rc.1
contextbridge version
contextbridge setup
contextbridge registry register C:\path\to\existing-repository
contextbridge handoff MyProject --task "Review the current implementation"
```

Use `init <project>` instead of `registry register` for a new Context Bridge project. Normal handoff use requires only the project name and task. `PASS` means the verified identity checks succeeded; `WARNING` means the handoff is usable but evidence or observability is incomplete; `BLOCKED` means Context Bridge stopped before producing a runnable handoff and reports the safe next action.

`setup` performs read-only prerequisite and personal GitHub account checks. It never stores credentials or tokens.

`init` refuses existing target paths. It creates and commits the local project before creating a private GitHub repository. The owner must exactly match the authenticated personal GitHub account. `--local-only` stops after the local commit and is intended for tests and intentionally local projects.

Configuration is supplied by flags or environment variables:

- `CONTEXTBRIDGE_PROJECTS_ROOT`
- `CONTEXTBRIDGE_GITHUB_OWNER`

`--dry-run` prints the immutable plan and never applies it. Safety failures cannot be overridden.

`registry` stores machine-local project, repository, and workspace identity in `%LOCALAPPDATA%\ContextBridge\registry-v1.json`. `CONTEXTBRIDGE_HOME` overrides that location for tests and controlled environments. Registering an existing repository is machine-local only and does not modify that repository.

`guard` re-probes the selected workspace and compares its canonical path, Git root, git-dir, git-common-dir, normalized primary remote, and branch policy against the registry. Any missing, ambiguous, or changed identity fails closed as `WRONG_WORKSPACE`; there is no force override. This guarantee applies to Context Bridge-mediated mutations and does not physically prevent unrelated software from writing directly.

Guarantee boundary: Context Bridge binds logical projects to verified repository/workspace identity, fails closed on mismatches for Context Bridge-mediated operations, and produces reproducible, explainable handoffs and verification context. It does not sandbox agents, guarantee agent compliance, prevent all secret leakage, or replace Git and agent-native security controls.

`discover` scans only an explicit bounded root, does not follow symlinks or Windows reparse points, does not fetch, and never changes repositories or the registry. The default bounds are depth 8, 256 repositories, 100,000 entries, and 15 seconds. Every completed scan emits a terminal status: `success`, `success_with_warnings`, `partial`, `no_candidates`, or `failed`. Unique-work and cross-clone results are explicitly local-evidence-only.

JSON discovery always includes the requested root, terminal status, candidate and skipped counts, traversal-limit state, warnings, and errors. Human output always ends with the same terminal summary. Stable scan reasons include `ACCESS_DENIED`, `REPARSE_POINT_SKIPPED`, `MAX_DEPTH_REACHED`, `ENTRY_LIMIT_REACHED`, `PROBE_FAILED`, `GIT_REPOSITORY_INACCESSIBLE`, `GIT_PROBE_FAILED`, `ROOT_NOT_FOUND`, and `TIME_LIMIT_REACHED`.

`doctor` validates Manifest V1 or V2 generated-file provenance and reports `CURRENT-STATE` content integrity, basis validity, and basis freshness separately. A metadata-only state commit may remain fresh; later product changes make the recorded basis stale. Missing or invalid provenance is reported as unverified, not as failed code verification.

`handoff` resolves a registered project by exact ID, display name, or alias, selects its registered canonical workspace, re-probes it, and runs the Workspace Guard before emitting a deterministic, read-only handoff. The handoff contains portable project/repository identity, canonical workspace facts, context entrypoint references, task intent, derived verification requirements, accepted-state evidence, and thin adapter hints for Codex, Claude, or Cursor. It never persists a Task or AgentSession, starts an agent, changes the registry, or changes the workspace. Guard failure is a hard `WRONG_WORKSPACE` stop with no runnable handoff. If durable verification instructions are absent, the handoff reports `UNVERIFIED` and `NO_VERIFICATION_CONTRACT` rather than inventing commands.

`instructions --explain` inspects effective instruction sources and safely observable agent environment metadata for Codex, Claude Code, or Cursor. It reports ordered source paths, sizes, SHA-256 hashes, repository containment, basis-change evidence where available, configuration scope, and readiness warnings without copying instruction contents or changing agent configuration. `doctor --agent` adds the same read-only diagnostics to the existing project doctor flow.

`adopt` explicitly adds a Context Bridge Manifest V2 to an existing branch-attached Git repository without changing Git history or the registry. `upgrade` explicitly migrates a Context Bridge Manifest V1 to V2, preserving the old manifest as `.contextbridge/manifest.json.v1.bak`. `rebind` explicitly updates only the machine-local canonical workspace record after a legitimate move. All three commands print a plan, require `--confirm` for mutation, re-probe immediately before mutation, and reject identity conflicts. `--dry-run` never prompts and never mutates.

## Identity model

- Project: portable `prj_<UUIDv4>` identity.
- Repository: portable `repo_<UUIDv4>` plus normalized primary-remote identity and canonical branch.
- Workspace: machine-local `ws_<UUIDv4>` plus canonical filesystem and Git topology evidence.

Topology (`MAIN_WORKTREE`, `LINKED_WORKTREE`, `INDEPENDENT_CLONE`) and registry role (`CANONICAL`, `MANAGED_WORKSPACE`, `REGISTERED_ALTERNATE`, `UNREGISTERED`) remain separate. Secondary remotes are inventoried but do not affect the primary-remote guard.

Manifest V2 never stores local workspace paths and does not use the mutable `CURRENT-STATE` content hash as semantic freshness proof. Manifest V1 is read-compatible and is never silently rewritten. See `docs/agent/MANIFEST.md`.

## V1.2 boundaries

V1.2 has no persistent Task or AgentSession model, managed worktree lifecycle, cleanup or deletion execution, agent launcher, daemon, dashboard/TUI, PR or issue orchestration, Codex internal-state parsing, automatic agent-configuration writes, AI/model/API calls, secret persistence, destructive Git commands, organization support, automatic AI-Knowledge writes, GUI, MCP, vector database, or Obsidian plugin. Obsidian is only a passive Markdown viewer. `adopt`, `upgrade`, and `rebind` remain explicit plan/apply operations; none is automatic.

Hosted CI is currently externally blocked by a GitHub billing or spending-limit issue. Local verification remains authoritative for this release-candidate decision; hosted CI is not claimed as passing.
