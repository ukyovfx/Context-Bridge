---
contextbridge_state_schema: 1
basis_branch: main
basis_commit: 2432e89e8d77f7f79ccba8049a121c0f9ac0e4d8
basis_date: 2026-09-10T07:48:26Z
---

# Context Bridge Current State

Status: V1.2 Phase 3 implemented, locally verified, and pushed; hosted CI blocked externally before steps started

## Implemented

- Portable Project and Repository identity plus machine-local Workspace identity
- Versioned JSON registry with full-document validation, exclusive writer lock, safe replacement, and last-known-valid backup
- Vendor-neutral HTTPS, SCP-style SSH, and `ssh://` remote normalization
- Shared read-only Git workspace probe, deterministic Workspace Guard, and bounded output-only discovery with explicit terminal reporting
- `registry list`, `registry show`, `registry register`, `guard`, and `discover` commands
- V1.2 Phase 1 read-only `handoff` routing by exact project ID/name/alias, canonical workspace lookup, fresh probe, mandatory guard, deterministic handoff, derived verification contract, and thin agent hints
- V1.2 Phase 2 read-only `instructions --explain` diagnostics for Codex, Claude Code, and Cursor, plus `doctor --agent` and minimal handoff warning integration
- V1.2 Phase 3 explicit `adopt`, `upgrade`, and `rebind` plan/apply flows with confirmation, immediate re-probe, identity conflict rejection, recoverable Manifest V1 backup, and registry-only rebind mutation
- Manifest V2 generated-template provenance with Manifest V1 read compatibility
- Separate CURRENT-STATE content integrity, basis validity, and basis freshness

## Verified on Windows

- `gofmt -l .`: passed with no output
- `go test ./...`: passed
- `go vet ./...`: passed
- `scripts/e2e-local.ps1`: passed
- E2E proved init and registration dry-runs, existing-repository registration, discovery, and guard behavior without unintended repository or registry mutation
- Discovery anomaly regression coverage proves empty, partial, bounded, access-limited, reparse-skipped, warning, and JSON terminal states
- Actual read-only scans: `C:\Users\mynti\Documents` returned `partial` with 2 candidates, 1 skipped directory, and `PROBE_EVIDENCE_INCOMPLETE`/`MAX_DEPTH_REACHED`; `C:\AI-Workspace` returned `success` with 3 candidates
- Actual read-only KitsuSync handoff: `KitsuSync-clean` resolved to its registered project, repository, canonical workspace, and `Guard allowed=true`; durable `AGENTS.md` verification instructions resolved to three commands; CURRENT-STATE evidence remained explicitly unverified because the workspace has no Context Bridge CURRENT-STATE provenance
- Handoff regression coverage proves exact/alias resolution, ambiguity, missing canonical workspace/path, guard failure, read-only behavior, deterministic output, remote redaction, stale state, missing verification contract, and thin agent adapters
- Instruction diagnostics regression coverage proves nested instruction chains, override files, size/truncation risk, isolated CODEX_HOME, Claude imports/local files/rules, Cursor rules, redaction, unknown agents, and zero-mutation behavior
- Actual KitsuSync diagnostics: Codex Guard `PASS` and `READY_WITH_WARNINGS` for global instruction presence and unobserved agent configuration; Claude and Cursor Guard `PASS` and `READY` with no discovered instruction sources
- Actual KitsuSync migration planning was read-only: adopt produced a Manifest V2 plan, upgrade reported `MANIFEST_CONFLICT` because no Context Bridge manifest exists, and same-path rebind reported `ALREADY_REBOUND`; no apply was run
- Migration regression coverage proves adopt of clean/dirty repositories, owned-file conflict, V1-to-V2 upgrade, recoverable backup, repeated no-op, unknown-newer refusal, rebind move, unrelated repository rejection, confirmation, registry-only mutation, and identity-change abort
- Unit and integration tests cover wrong path/Git root/git-dir/common-dir/remote/branch, linked worktrees, independent clones, detached HEAD, local-only unique evidence, and identity change between plan and apply

## GitHub verification

- Repository: `ukyovfx/Context-Bridge`
- Visibility: private
- Default branch: `main`
- Reviewed product basis: `2432e89e8d77f7f79ccba8049a121c0f9ac0e4d8`
- Remote `HEAD` and `refs/heads/main` matched the reviewed product basis after push

## External blocker

GitHub Actions run `34427978671` for the reviewed product basis completed with no workflow steps. Its check annotation reports failed recent account payments or a spending-limit issue. Hosted CI is therefore unverified; this is not evidence of a code-test failure.

## Remaining risk

- Resolve the GitHub billing or spending-limit issue and rerun hosted CI.
- V1.1 guards Context Bridge-mediated mutations; external tools can still bypass Context Bridge and write directly.
