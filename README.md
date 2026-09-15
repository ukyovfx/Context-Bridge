# Context Bridge

GPT ↔ Codex Development Context Bridge

Context Bridge is a deterministic, local-first CLI for creating development repositories with durable Markdown context and for verifying project, repository, and local workspace identity before Context Bridge-mediated writes.

## Install

The primary Windows release is a per-user installer. Download
`contextbridge-VERSION-windows-amd64-setup.exe`, run it without administrator
privileges, open a new terminal, and run:

```powershell
contextbridge version
contextbridge setup
```

The installer places the standalone binary in
`%LOCALAPPDATA%\Programs\ContextBridge` and adds only that directory to the
user PATH. It does not modify projects, registries, workspace profiles, or
Codex instructions. Reinstalling or upgrading is idempotent.

Portable Windows installation is also available as
`contextbridge-VERSION-windows-amd64.zip`: extract it, add its directory to
your user PATH, open a new terminal, and run `contextbridge setup`. Linux users
can use `contextbridge-VERSION-linux-amd64.tar.gz` similarly.

The portable archives do not contain user state. `%LOCALAPPDATA%\ContextBridge`
and `CONTEXTBRIDGE_HOME` remain separate from the installed binary. Replacing
or deleting the binary does not delete profiles, registries, repositories, or
workspace files.

The installer is currently unsigned because no signing identity is configured.
Verify the published SHA-256 checksums before use. Self-update and winget
distribution are not implemented yet.

For developer builds, Go 1.23 or newer is required.

```powershell
go install github.com/ukyovfx/Context-Bridge/cmd/contextbridge@latest
```

## Commands

```text
contextbridge setup [--workspace-root PATH] [--codex-bootstrap] [--confirm] [--dry-run]
contextbridge init <project> [--root PATH] [--owner USER] [--profile core|openai] [--local-only] [--dry-run]
contextbridge new <project> [--profile core|openai] [--github] [--owner USER] [--dry-run]
contextbridge integrate <path> [--confirm] [--dry-run] [--json]
contextbridge bootstrap chatgpt <project> [--json]
contextbridge doctor [--project PATH]
contextbridge inspect [PATH] [--json]
contextbridge preview [PATH] [--json]
contextbridge apply <PATH> --confirm [--json]
contextbridge registry list
contextbridge registry show <project>
contextbridge registry register <path> [--role canonical|alternate] [--dry-run]
contextbridge guard --project <id-or-alias> [--workspace <id-or-path>]
contextbridge discover --root <path> [--json]
contextbridge handoff <project> --task "<intent>" [--agent codex|claude|cursor] [--credit-state STATE] [--json]
contextbridge instructions --explain [--project PATH] [--agent codex|claude|cursor] [--json]
contextbridge adopt <path> [--dry-run] [--confirm] [--json]
contextbridge upgrade [--check|--dry-run] | <path> [--dry-run] [--confirm] [--json]
contextbridge rebind <project> --workspace <path> [--dry-run] [--confirm] [--json]
contextbridge writeback plan <project> --class none|active|durable_record|accepted_state --summary "..." [--json]
contextbridge writeback apply --proposal PATH [--json]
contextbridge knowledge doctor --project PATH [--json]
contextbridge version
```

## Quick start

```powershell
go install github.com/ukyovfx/Context-Bridge/cmd/contextbridge@v1.2.0
contextbridge version
contextbridge setup
contextbridge registry register C:\path\to\existing-repository
contextbridge handoff MyProject --task "Review the current implementation"
```

Use `new <project>` after `setup` for the normal local-first flow. It creates the project under the configured `<workspace-root>/active/<project>`, commits the existing minimal scaffold, and registers it automatically. Use `--github` only when a private GitHub repository should also be created and pushed. `init <project>` remains available for explicit roots and backward compatibility. Normal handoff use requires only the project name and task. `PASS` means the verified identity checks succeeded; `WARNING` means the handoff is usable but evidence or observability is incomplete; `BLOCKED` means Context Bridge stopped before producing a runnable handoff and reports the safe next action.

`setup` performs read-only prerequisite and personal GitHub account checks. On first use it asks for or accepts one workspace root (Windows defaults to `C:\AI-Workspace`), previews the local profile and missing `active`, `worktrees`, `archive`, and `private` directories, and creates only the exact reviewed changes after explicit approval. `--dry-run` prints the same preview and performs no writes. Add `--codex-bootstrap` to preview and optionally install a minimal, clearly marked Context Bridge block in `~/.codex/AGENTS.md` or `$CODEX_HOME/AGENTS.md`; existing instructions are preserved and an override or malformed managed block stops the bootstrap. Setup never stores credentials or tokens.

`init` refuses existing target paths. It creates and commits the local project before creating a private GitHub repository. The owner must exactly match the authenticated personal GitHub account. `--local-only` stops after the local commit and is intended for tests and intentionally local projects.

`integrate <path>` brings an existing branch-attached Git repository under Context Bridge without moving it. It shows one plan, preserves existing repository-native context, adds only a minimal additive AGENTS route when needed, creates a Manifest V2 through the existing adoption rules, and registers the canonical workspace. Repository file changes require `--confirm`; registration-only changes retain the existing registry safety semantics. `--dry-run` is read-only, and conflicts or identity changes fail closed.

`bootstrap chatgpt <project>` emits deterministic, read-only ChatGPT Project Instructions for a registered canonical workspace after a fresh Guard check. The output is portable and copyable: GitHub-connected ChatGPT can see only pushed repository state, so local dirty or unpushed changes require a handoff or pasted context. Local-only projects are reported as unavailable to connected ChatGPT when Guard cannot verify a remote identity. Review the generated text because ChatGPT Project Instructions apply inside that project and may override global custom instructions.

Upgrade by running the newer per-user installer over the existing installation;
projects and Context Bridge user state are preserved. To uninstall, use
Windows Apps & features or the installed uninstaller; only the installed
binary, installer-owned files, and managed user PATH entry are removed.
Developer installation with `go install` is an advanced/source-build option,
not the normal user path.

Configuration is supplied by flags or environment variables:

- `CONTEXTBRIDGE_PROJECTS_ROOT`
- `CONTEXTBRIDGE_GITHUB_OWNER`

`--dry-run` prints the immutable plan and never applies it. Safety failures cannot be overridden.

Context Bridge itself is installed independently and may live anywhere on `PATH`.
An optional machine-local `workspace-profile-v1.json` lives in the existing
Context Bridge home (`%LOCALAPPDATA%\\ContextBridge` on Windows, or the
platform-specific home resolved by Context Bridge). The profile contains only
`schema_version` and one user-selected absolute `workspace_root`; use a
placeholder such as `<workspace-root>`, never a personal path. Context Bridge
derives `<workspace-root>/active`, `<workspace-root>/worktrees`,
`<workspace-root>/archive`, and `<workspace-root>/private` in memory.
Only `doctor --project` reads the profile for advisory placement checks. The
user workspace is separate from Context Bridge's local configuration/state, and
secrets or credentials must remain outside both. The profile is never copied
into repositories, manifests, handoffs, write-back files, or generated
documentation.

`inspect`, `preview`, `apply`, and `doctor <path>` are the minimal static context
workflow. `inspect` classifies repository-native context without executing
repository code. `preview` emits the exact three-file greenfield scaffold and
never writes. `apply` requires `--confirm`, rechecks the preview fingerprint,
uses exclusive file creation, and never overwrites or deletes files. The
greenfield scaffold includes a thin Context Bridge route in `AGENTS.md`.
Existing `AGENTS.md` content is preserved; only an additive route may be
proposed, and conflicting instructions are reported without modification.
Mature repositories are routed to their existing documented context system
instead of receiving a duplicate state system. `doctor <path>` is
read-only and reports routing conflicts, missing references, oversized routing
files, local-path/private-network/secret-like indicators, and missing
verification guidance without printing matched values. The v1 context scanner
intentionally inspects only a bounded set of known context-entry systems;
custom or non-listed systems require human review, and this is not arbitrary
repository semantic discovery. `upgrade --check` and `upgrade --dry-run` are
zero-mutation readiness checks only. Automatic binary replacement is
intentionally not implemented; a signed release source and rollback contract
remain v1.1 work. The existing `upgrade <path>` command remains the explicit
Manifest migration flow.

Codex integration is project-local by default: use `inspect`, then `preview` and
`apply <repo> --confirm` in each repository. For fresh Codex sessions, the
optional `setup --codex-bootstrap` flow adds only a managed global router block;
it never edits `config.toml` or unrelated instruction content. The read-only
`instructions --explain --agent codex` and `doctor --agent codex` diagnostics
report workspace profile status, global router status, conflicts, warnings, and
the exact safe next action. A missing Codex installation does not prevent
Context Bridge from operating or serving other providers.

The intended workflow is:

```text
ChatGPT Web research/design
  -> Context Bridge prompt/model policy
  -> Codex execution prompt
  -> AGENTS.md and repository-native context
  -> implementation and verification
```

ChatGPT Web supplies research, design, decomposition, review, and prompt
generation. Codex inspects, edits, tests, and reports repository-grounded
results. Context Bridge connects those workflows through local discovery and
safety checks; it does not execute an AI workflow or store prompts.

Example:

```text
contextbridge handoff MyProject --task "Fix the guard regression" --json
```

Copy `execution_prompt` to Codex and keep `model_recommendation` outside that
prompt. Credit adaptation is used only when trustworthy budget information is
provided through optional input; Context Bridge never scrapes or invents
remaining-credit values.

`registry` stores machine-local project, repository, and workspace identity in `%LOCALAPPDATA%\ContextBridge\registry-v1.json`. `CONTEXTBRIDGE_HOME` overrides that location for tests and controlled environments. Registering an existing repository is machine-local only and does not modify that repository.

`guard` re-probes the selected workspace and compares its canonical path, Git root, git-dir, git-common-dir, normalized primary remote, and branch policy against the registry. Any missing, ambiguous, or changed identity fails closed as `WRONG_WORKSPACE`; there is no force override. This guarantee applies to Context Bridge-mediated mutations and does not physically prevent unrelated software from writing directly.

Guarantee boundary: Context Bridge binds logical projects to verified repository/workspace identity, fails closed on mismatches for Context Bridge-mediated operations, and produces reproducible, explainable handoffs and verification context. It does not sandbox agents, guarantee agent compliance, prevent all secret leakage, or replace Git and agent-native security controls.

`discover` scans only an explicit bounded root, does not follow symlinks or Windows reparse points, does not fetch, and never changes repositories or the registry. The default bounds are depth 8, 256 repositories, 100,000 entries, and 15 seconds. Every completed scan emits a terminal status: `success`, `success_with_warnings`, `partial`, `no_candidates`, or `failed`. Unique-work and cross-clone results are explicitly local-evidence-only.

JSON discovery always includes the requested root, terminal status, candidate and skipped counts, traversal-limit state, warnings, and errors. Human output always ends with the same terminal summary. Stable scan reasons include `ACCESS_DENIED`, `REPARSE_POINT_SKIPPED`, `MAX_DEPTH_REACHED`, `ENTRY_LIMIT_REACHED`, `PROBE_FAILED`, `GIT_REPOSITORY_INACCESSIBLE`, `GIT_PROBE_FAILED`, `ROOT_NOT_FOUND`, and `TIME_LIMIT_REACHED`.

`doctor` validates Manifest V1 or V2 generated-file provenance and reports `CURRENT-STATE` content integrity, basis validity, and basis freshness separately. A metadata-only state commit may remain fresh; later product changes make the recorded basis stale. Missing or invalid provenance is reported as unverified, not as failed code verification.

`handoff` resolves a registered project by exact ID, display name, or alias, selects its registered canonical workspace, re-probes it, and runs the Workspace Guard before emitting a deterministic, read-only handoff. The handoff contains portable project/repository identity, canonical workspace facts, context entrypoint references, task intent, derived verification requirements, accepted-state evidence, thin adapter hints, a concise execution prompt, and a separate advisory model recommendation. It never persists a Task or AgentSession, starts an agent, changes the registry, or changes the workspace. Guard failure is a hard `WRONG_WORKSPACE` stop with no runnable handoff. If durable verification instructions are absent, the handoff reports `UNVERIFIED` and `NO_VERIFICATION_CONTRACT` rather than inventing commands.

The current default recommendation is `GPT-5.6 Luna` with `Medium` reasoning. Recommendation policy uses task risk and complexity roles rather than project state, and never bypasses Guard or approval boundaries. `--credit-state` is optional manual/adaptor input (`abundant`, `normal`, `constrained`, `critical`, or `unknown`); unknown is never guessed and behaves like normal.

`instructions --explain` inspects effective instruction sources and safely observable agent environment metadata for Codex, Claude Code, or Cursor. For Codex, the reported order matches Codex precedence: one non-empty global override or base file first, followed by one non-empty project instruction file per directory from repository root toward the current directory. It reports ordered source paths, sizes, SHA-256 hashes, repository containment, basis-change evidence where available, configuration scope, workspace profile status, global router status, and readiness warnings without copying instruction contents or changing agent configuration. `doctor --agent` adds the same read-only diagnostics to the existing project doctor flow.

`adopt` explicitly adds a Context Bridge Manifest V2 to an existing branch-attached Git repository without changing Git history or the registry. `upgrade` explicitly migrates a Context Bridge Manifest V1 to V2, preserving the old manifest as `.contextbridge/manifest.json.v1.bak`. `rebind` explicitly updates only the machine-local canonical workspace record after a legitimate move. All three commands print a plan, require `--confirm` for mutation, re-probe immediately before mutation, and reject identity conflicts. `--dry-run` never prompts and never mutates.

`writeback plan` and `writeback apply` provide the minimal explicit knowledge write-back flow. Use `NONE` when no durable knowledge changed; use `ACTIVE` for unfinished work, `DURABLE_RECORD` for one decision or audit, and `ACCEPTED_STATE` only when its full evidence gate passes. The flow is local-only, never commits or pushes, and does not write AI-Knowledge. `knowledge doctor` is a read-only Markdown metadata and provenance check. See `docs/agent/KNOWLEDGE-WRITEBACK.md`.

## Identity model

- Project: portable `prj_<UUIDv4>` identity.
- Repository: portable `repo_<UUIDv4>` plus normalized primary-remote identity and canonical branch.
- Workspace: machine-local `ws_<UUIDv4>` plus canonical filesystem and Git topology evidence.

Topology (`MAIN_WORKTREE`, `LINKED_WORKTREE`, `INDEPENDENT_CLONE`) and registry role (`CANONICAL`, `MANAGED_WORKSPACE`, `REGISTERED_ALTERNATE`, `UNREGISTERED`) remain separate. Secondary remotes are inventoried but do not affect the primary-remote guard.

Manifest V2 never stores local workspace paths and does not use the mutable `CURRENT-STATE` content hash as semantic freshness proof. Manifest V1 is read-compatible and is never silently rewritten. See `docs/agent/MANIFEST.md`.

## V1.2 boundaries

V1.2 has no persistent Task or AgentSession model, managed worktree lifecycle, cleanup or deletion execution, agent launcher, daemon, dashboard/TUI, PR or issue orchestration, Codex internal-state parsing, automatic agent-configuration writes, AI/model/API calls, secret persistence, destructive Git commands, organization support, automatic AI-Knowledge writes, GUI, MCP, vector database, or Obsidian plugin. Explicit `setup --codex-bootstrap` may manage only its marked global Context Bridge block; it does not modify `config.toml` or unrelated content. Obsidian is only a passive Markdown viewer. `adopt`, `upgrade`, and `rebind` remain explicit plan/apply operations; none is automatic.

Hosted CI passed for the reviewed release basis commit. No stable tag or GitHub Release has been published yet; publication remains an explicit release action.

## Pilot boundary

Context Bridge remains the identity, Guard, context-integrity, and verified-handoff layer. An external repository retrieval companion and an optional plain-Markdown Brain may improve context selection, but Git/GitHub remain authoritative and neither companion nor Brain is required project state. Obsidian is optional Markdown UI only. See [`docs/agent/CONTEXT-ARCHITECTURE.md`](docs/agent/CONTEXT-ARCHITECTURE.md) and [`docs/agent/PILOT.md`](docs/agent/PILOT.md) for the replaceable companion boundary, Brain convention, selection order, and 30-day local measurement plan.
