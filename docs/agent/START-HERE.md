# Context Bridge Agent Start Here

Context Bridge is a deterministic, local-first bridge for carrying development context between GPT planning and Codex implementation.

## Read in this order

1. `AGENTS.md`
2. `docs/agent/CURRENT-STATE.md`
3. The relevant active plan under `docs/agent/plans/active/`
4. `docs/agent/MANIFEST.md` when identity or generated-file provenance is relevant
5. Relevant code, tests, CI, and repository documentation

## V1 boundaries

V1 is a Windows-first CLI. It makes no model or model-provider API calls. Obsidian may display generated Markdown passively, but Context Bridge does not integrate with or write through an Obsidian plugin.

The CLI creates new projects only. It does not adopt or migrate existing repositories. Remote creation is limited to the authenticated user's personal GitHub account and always creates a private repository after the local project has been created and committed.

V1.1 adds machine-local registration of existing repositories without modifying them. It does not upgrade or adopt their repository contents. Before Context Bridge-mediated writes in a registered workspace, run `contextbridge guard`; `WRONG_WORKSPACE` is a hard stop with no force override.

## Verification route

- Unit tests: `go test ./...`
- Formatting: `gofmt -l .`
- Static checks: `go vet ./...`
- Local E2E: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/e2e-local.ps1`
