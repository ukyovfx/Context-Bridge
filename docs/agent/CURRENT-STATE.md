---
contextbridge_state_schema: 1
basis_branch: main
basis_commit: d29f9f3441f6a920ac7b34dd370bfcdf295468b0
basis_date: 2026-09-15T11:11:33Z
---

# Context Bridge Current State

Status: Packaging / Onboarding V1 frozen after completion audit; installer, portable distribution, CLI onboarding, and hosted CI verified

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
- V1.2.x human summaries with explicit PASS/WARNING/BLOCKED terminal labels, deduplicated warning metadata, clear provenance wording, explicit absent Claude/Cursor configuration diagnostics, and distinct Git target/probe/access failure reasons
- Shared read-only Git probe classification is used by migration, guard, handoff, doctor, instructions, and discovery paths; it distinguishes genuine non-Git targets from inaccessible or failed Git probes without changing Git configuration
- Minimal deterministic Knowledge Write-back V1 with `NONE`, `ACTIVE`, `DURABLE_RECORD`, and gated `ACCEPTED_STATE` classes; immutable JSON plan/apply separation, identity and worktree precondition revalidation, secret-like content refusal, canonical Markdown routing, and read-only `knowledge doctor`
- Packaging / Onboarding V1 Phase 1 machine bootstrap: setup now previews and explicitly applies one machine-local workspace profile plus optional Codex global router bootstrap, preserving unrelated instruction content and refusing override or managed-block conflicts
- Codex instruction diagnostics now match Codex precedence: one non-empty global override or base file first, followed by one non-empty project instruction file per directory from repository root toward the current directory
- Read-only agent diagnostics now report workspace profile status, Codex global router status, conflicts/warnings, and an exact safe next action
- Packaging / Onboarding V1 Phase 2 `new <project>` command: profile-rooted local-first creation under `active`, existing scaffold reuse, automatic project/repository/canonical-workspace registration, explicit `--github` opt-in, and zero-mutation dry-run
- Packaging / Onboarding V1 Phase 3 `integrate <path>` flow: read-only probe and context inspection, one plan, conservative additive routing, existing-repository manifest adoption, canonical registration, immediate identity revalidation, and idempotent dry-run/apply behavior
- Packaging / Onboarding V1 Phase 4 read-only `bootstrap chatgpt <project>` output: registered-project resolution, fresh Guard/readiness checks, portable repository identity and entrypoints, explicit GitHub visibility limits, local dirty/unpushed warnings, deterministic human/JSON output, and reuse of the generated ChatGPT instruction template
- Packaging / Onboarding V1 Phase 5 distribution: per-user Windows installer definition, portable ZIP packaging, preserved user PATH management, data-preserving uninstall, deterministic archive timestamps, release checksums/manifest, and GitHub Actions artifact-retention workflow
- Packaging / Onboarding V1 Phase 5 disposable packaging E2E: standalone binary, fresh-shell version check, setup/profile creation, Codex isolation, new project, existing-project integration, and ChatGPT bootstrap

## Verified on Windows

- `gofmt -l .`: passed with no output
- `go test ./...`: passed
- `go vet ./...`: passed
- `scripts/e2e-local.ps1`: passed
- E2E proved init and registration dry-runs, existing-repository registration, discovery, and guard behavior without unintended repository or registry mutation
- Discovery anomaly regression coverage proves empty, partial, bounded, access-limited, reparse-skipped, warning, and JSON terminal states
- Disposable read-only discovery fixtures returned the documented terminal states and preserved repository and registry state
- Read-only handoff, instruction, doctor, guard, migration, and knowledge-doctor checks passed in disposable fixtures; unverified provenance and missing optional knowledge directories were reported as warnings
- Handoff regression coverage proves exact/alias resolution, ambiguity, missing canonical workspace/path, guard failure, read-only behavior, deterministic output, remote redaction, stale state, missing verification contract, and thin agent adapters
- Instruction diagnostics regression coverage proves nested instruction chains, override files, size/truncation risk, isolated CODEX_HOME, Claude imports/local files/rules, Cursor rules, redaction, unknown agents, and zero-mutation behavior
- Disposable provider diagnostics: Codex Guard and readiness warnings were reported without changing agent configuration; Claude and Cursor diagnostics completed with no discovered instruction sources
- Actual KitsuSync migration planning was read-only: adopt produced a Manifest V2 plan, upgrade reported `MANIFEST_CONFLICT` because no Context Bridge manifest exists, and same-path rebind reported `ALREADY_REBOUND`; no apply was run
- Migration regression coverage proves adopt of clean/dirty repositories, owned-file conflict, V1-to-V2 upgrade, recoverable backup, repeated no-op, unknown-newer refusal, rebind move, unrelated repository rejection, confirmation, registry-only mutation, and identity-change abort
- UX/diagnostics regression coverage proves concise human output, warning severity/action mapping, warning deduplication, absent Claude/Cursor configuration reporting, and inaccessible-Git versus non-Git migration probe classification
- Packaged binary success-path smoke timing was measured in a trusted temporary fixture: version 20.6 ms median, guard 674.3 ms, handoff 1316.1 ms, doctor --agent codex 2183.7 ms, and instructions --explain --agent codex 1122.7 ms; five runs each, all exit 0
- The prior `v1.2.0-rc.1` release-candidate notes remain historical; no stable tag or GitHub release has been created
- Unit and integration tests cover wrong path/Git root/git-dir/common-dir/remote/branch, linked worktrees, independent clones, detached HEAD, local-only unique evidence, and identity change between plan and apply
- Knowledge Write-back regression coverage proves NONE zero-byte behavior, active and durable routing, deterministic JSON, malformed proposal refusal, secret-like content refusal, identity-change abort, accepted-state downgrade and successful promotion, no automatic Git mutation, and read-only knowledge diagnostics
- Packaging / Onboarding regression coverage proves setup dry-run zero mutation, explicit confirmation, idempotency, temporary Codex HOME isolation, preservation of existing global instructions, managed-block conflict refusal, stale-plan refusal, Codex precedence ordering, and profile/router diagnostics
- New-project regression coverage proves configured active-root resolution, exact target paths, dry-run zero mutation, local registration, existing-target refusal, invalid-profile refusal, and explicit GitHub planning
- Existing-project integration regression coverage proves clean and dirty repositories, AGENTS preservation, existing-route no-op, instruction conflicts, existing/missing manifests, dry-run zero mutation, repeated no-op, identity-change refusal, and registry-backed canonical registration
- ChatGPT bootstrap regression coverage proves registered-project and alias resolution, Guard and canonical-workspace refusal, GitHub and local-only readiness, deterministic human/JSON output, zero mutation, template consistency, path redaction, and concise copyable output
- Local release verification produced Windows and Linux portable archives plus `SHA256SUMS` and `release-manifest.json`; the local host did not have Inno Setup, so installer compilation and installer E2E remain unverified here
- Packaged binary smoke proved registry resolution, NONE plan/apply, ACTIVE plan/apply, canonical active-plan routing, and machine-readable knowledge-doctor warnings in a disposable local fixture
- Final Windows installer audit passed with Inno Setup 7.1.0: clean per-user install, fresh-shell PATH/version resolution, setup with isolated Codex bootstrap, `new`, `integrate`, ChatGPT bootstrap, same-version reinstall, state preservation, PATH de-duplication, uninstall, repository preservation, unrelated instruction preservation, and fresh-shell non-resolution after uninstall
- Final release artifacts include the Windows installer, Windows portable ZIP, Linux archive, `SHA256SUMS`, and `release-manifest.json`; independent hashes matched, manifest version was `v1.2.0-phase5-test.1`, and the portable binary reported the same version
- Final installer audit found no repository/temp path, signing-secret marker, or private-key marker in the installer payload; the installer metadata reported product version `1.2.0-phase5-test.1`
- Hosted CI run `34961905308` passed on release basis commit `d29f9f3441f6a920ac7b34dd370bfcdf295468b0`; unit tests, formatting, static checks, and Local E2E all completed successfully

## Remaining risk

- No stable tag or GitHub Release has been published yet; release publication remains an explicit approval step.
- The installer is unsigned because no signing identity is configured.
- V1.1 guards Context Bridge-mediated mutations; external tools can still bypass Context Bridge and write directly.
