# Context Bridge Architecture Contract

This document defines the boundary for the post-V1 pilot. It does not add a
runtime dependency, a new state store, or an automatic synchronization path.

## Lean development direction

V1.2 scope is frozen for verification and release. No new retrieval, Brain, or
GitHub features are committed to V1.2. After release, complete one final
controlled Repowise source-only A/B benchmark, make a GO/NO-GO retrieval
decision, and then use Context Bridge in real development.

No feature without observed pain: add a subsystem only after a recurring
real-world problem is observed and the smallest existing mechanism cannot
solve it. Before proposing a feature, ask whether Core, Git, `AGENTS.md`,
repository documentation, or `rg` already solves the problem; whether it
belongs in Core; whether an optional adapter, policy, or documentation is
enough; and whether measured benefit exceeds maintenance and complexity cost.

Repowise remains optional pilot tooling. There is no autonomous MCP work or
Serena comparison now. If the final small-repository benchmark shows no
material benefit, default to `rg` plus `AGENTS.md` and repository-native docs;
Repowise may remain a future adapter for large or complex repositories.

Do not implement a Brain subsystem now. `HOT.md` may be used manually when
useful, but `INDEX.md`, Obsidian integration or synchronization, automation,
and vector retrieval require observed need first. External Write Preflight is
design guidance only; do not implement a GitHub write adapter, policy engine,
or persistent state machine without repeated external-write mistakes or proof
that policy and instructions are insufficient.

The same gate explicitly postpones Serena, autonomous Repowise MCP, Obsidian
sync, Brain automation, vector or semantic-memory infrastructure, generic agent
orchestration, task-management/PKM features, and generic “AI Development OS”
functionality. Optional retrieval, personal-context, and external-write
helpers must remain removable and must not become Context Bridge core
dependencies.

## Authority and roles

- Git and GitHub are authoritative for project and repository truth.
- Context Bridge owns Project, Repository, and machine-local Workspace identity,
  Guard, exact Git-state verification, context-integrity checks, and verified
  handoff.
- A repository retrieval/index companion is an optional external reader. It may
  improve retrieval efficiency, but it is not a source of repository truth.
- A plain-Markdown Brain is optional personal or cross-project context. Brain
  content is lower authority than repository evidence and may reference a
  repository; repository files must never depend on it.
- Obsidian is an optional human UI for Markdown. It is not a Context Bridge
  dependency and Context Bridge does not manage a vault or plugin.

The Brain and any retrieval companion must be removable without affecting
Context Bridge, a repository, its registry, or its write-back flow.

## Context selection order

Every pilot comparison preserves this order:

1. Project, Repository, and Workspace identity.
2. Exact Git binding: branch, HEAD, dirty state, and relevant probe evidence.
3. Authority of each source.
4. Relevance to the task.
5. Freshness.
6. Sensitivity and privacy.
7. Token-budget selection.

Conflicts are reported with their sources and are not silently merged. A
retrieval result or Brain note cannot override Guard, repository identity,
source/tests/CI evidence, or explicit write-back gates.

## External retrieval companion boundary

The future companion interface is capability-based and vendor-neutral. A
companion may advertise capabilities such as `retrieve`, `provenance`, and
`freshness`; Context Bridge does not depend on a product name or internal API.

An adapter request contains only the user-approved task, portable repository
identity, exact Git-state summary, and explicitly allowed retrieval scope. A
result must identify its source repository, path or symbol, revision when
known, retrieval time, and any uncertainty. Results are advisory context and
must remain distinguishable from repository truth.

The adapter boundary must be replaceable by another companion without changing
Project/Repository/Workspace identity, Guard, manifests, registry state,
write-back, or the CLI contract. No companion may write to a repository,
Context Bridge state, or the Brain through this boundary. Repowise is the
current optional pilot companion; another adapter is not justified until
observed pain and a measured need establish it.

## Brain convention

The minimum machine-facing convention is:

- `HOT.md`: very small, always-eligible personal context.
- `INDEX.md`: compact map from topics or projects to deeper notes.
- Deeper notes: loaded only when the task makes them relevant.

Brain content is optional, local, human-maintained, and lower authority than
the repository. It must not be copied into a repository, required for Guard,
or treated as accepted project state. Personal Brain write-back remains
human-approved. Context Bridge does not create, locate, synchronize, or commit
an Obsidian vault.

## Existing Context Bridge boundary

Use `handoff --json`, `instructions --explain --json`, `doctor`, `guard`, and
`knowledge doctor` as the existing read-only evidence surfaces. Use the
existing `NONE` / `ACTIVE` / `DURABLE_RECORD` / `ACCEPTED_STATE` write-back
flow for project truth. No new memory database, vector index, task system,
agent orchestration, or automatic Brain synchronization is part of this pilot.
