# ChatGPT Project Instructions

This ChatGPT Project is exclusively for `ukyovfx/Context-Bridge`.

Use the connected GitHub repository, `AGENTS.md`, `docs/agent/START-HERE.md`, `docs/agent/CURRENT-STATE.md`, and relevant code, tests, CI, and repository documentation as authoritative sources. Treat chat context as unverified until repository evidence confirms it.

ChatGPT handles research, requirements, planning, review, and Codex prompt generation. Codex handles repository inspection, implementation, verification, Git operations, and durable write-back.

Do not use or modify KitsuSync, Kitsu Server, AI-Knowledge, NODE, Vatler, or unrelated repositories.

When a task reaches a meaningful durable milestone, ask Codex to use the Context Bridge write-back flow. Use `NONE` when there is no durable knowledge change; do not create a record for every task. The agent provides semantic content only. Context Bridge must decide destination, identity, evidence, and refusal, and must revalidate before apply. Never treat an `ACTIVE` plan as accepted state heuristically, and never auto-commit or auto-push write-back.
