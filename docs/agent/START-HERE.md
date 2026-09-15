# Context Bridge Agent Start Here

Context Bridge is a deterministic, local-first bridge for carrying development context between GPT planning and Codex implementation.

## Read in this order

1. `AGENTS.md`
2. `docs/agent/CURRENT-STATE.md`
3. The relevant active plan under `docs/agent/plans/active/`
4. `docs/agent/MANIFEST.md` when identity or generated-file provenance is relevant
5. `docs/agent/KNOWLEDGE-WRITEBACK.md` when durable knowledge routing is relevant
6. Relevant code, tests, CI, and repository documentation

## V1 boundaries and product direction

V1.2 is a Windows-first CLI. Its core scope is Project, Repository, and Workspace identity, exact Git-state binding, context integrity, Guard, verified handoff, and controlled project-truth write-back. It makes no model or model-provider API calls. Obsidian may display generated Markdown passively, but Context Bridge does not integrate with or write through an Obsidian plugin.

The CLI creates new projects and supports explicit, confirmed integration and migration plans for existing repositories. Remote creation is limited to the authenticated user's personal GitHub account and always creates a private repository after the local project has been created and committed. Adoption, upgrade, rebind, and external writes are never automatic.

Before Context Bridge-mediated writes in a registered workspace, run `contextbridge guard`; `WRONG_WORKSPACE` is a hard stop with no force override.

No feature without observed pain. V1.2 scope is frozen for verification and release; it gains no new retrieval, Brain, or GitHub features. After release, complete one final controlled Repowise source-only A/B benchmark, make a GO/NO-GO retrieval decision, then use Context Bridge in real development. Add a subsystem only after a recurring real-world problem is observed and the smallest existing mechanism cannot solve it.

Future proposals must ask whether the problem recurs, whether Core/Git/AGENTS.md/repository docs/`rg` already solve it, whether it belongs in Core, whether an optional adapter or policy is sufficient, and whether the measured benefit exceeds maintenance and complexity cost.

## Verification route

- Unit tests: `go test ./...`
- Formatting: `gofmt -l .`
- Static checks: `go vet ./...`
- Local E2E: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/e2e-local.ps1`

## Knowledge write-back

Write back only at the meaningful end of work. Use `NONE` when there is no durable change; do not write every task. The agent supplies semantic content, while Context Bridge owns routing, identity, evidence, and refusal. Plan before apply, and expect apply to revalidate. `ACTIVE` records go under `docs/agent/plans/active/`; durable decisions or audits go under the matching `docs/agent/` directory without duplicate copies. `ACCEPTED_STATE` is allowed only when the fresh canonical Guard, repository identity, default branch, HEAD, verification contract, required commands, and current-state provenance all pass. Never commit or push automatically. `knowledge doctor` is read-only.
