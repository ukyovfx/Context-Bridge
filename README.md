# Context Bridge

GPT ↔ Codex Development Context Bridge

Context Bridge is a deterministic, local-first CLI for creating new development repositories with durable Markdown context for GPT planning and Codex execution.

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
contextbridge version
```

`setup` performs read-only prerequisite and personal GitHub account checks. It never stores credentials or tokens.

`init` refuses existing target paths. It creates and commits the local project before creating a private GitHub repository. The owner must exactly match the authenticated personal GitHub account. `--local-only` stops after the local commit and is intended for tests and intentionally local projects.

Configuration is supplied by flags or environment variables:

- `CONTEXTBRIDGE_PROJECTS_ROOT`
- `CONTEXTBRIDGE_GITHUB_OWNER`

`--dry-run` prints the immutable plan and never applies it. Safety failures cannot be overridden.

`doctor` validates a project created by Context Bridge, including every hash in `.contextbridge/manifest.json`. A changed `docs/agent/CURRENT-STATE.md` is reported explicitly as stale.

## V1 boundaries

V1 has no AI/model calls, secret persistence, destructive Git commands, organization support, existing-repository adoption, automatic AI-Knowledge writes, GUI, MCP, vector database, or Obsidian plugin. Obsidian is only a passive Markdown viewer.
