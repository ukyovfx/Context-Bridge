# Context Bridge 30-Day Pilot

The pilot compares context quality per model credit while keeping all
measurements local and human-reviewable. It does not send telemetry, create a
dashboard, or store personal Brain content in this repository.

## Controlled sequence

### Week 1: baseline

Run representative tasks with Context Bridge only. Resolve the project,
verify Guard, inspect the instruction chain, and capture the JSON handoff.

### Week 2: Repowise companion (incomplete)

Use Repowise only as an external retrieval companion. Keep the same task set
and Context Bridge checks. Record retrieval provenance and any repeated or
irrelevant reads. Do not integrate Repowise internals into Context Bridge.

The current CLI retrieval work proves indexing, provenance, and retrieval
operation, but efficiency benefit is not yet proven. Complete one final small,
source-only A/B benchmark before making a GO/NO-GO retrieval decision.

### After the retrieval decision

Stop feature work and use Context Bridge in real development. A Brain pilot,
Serena comparison, autonomous Repowise MCP, Obsidian sync, or external-write
automation requires observed recurring pain and a new explicit decision.

## Local measurement record

For each task, record locally (outside this repository):

- condition and task identifier;
- total model/context tokens, tool calls, repository file reads, and repeated
  reads when the companion or client exposes them;
- time to first useful edit;
- verification result and test success;
- human correction or rework;
- stale-context incidents and context-selection precision;
- manual maintenance time.

Use the same task prompts, repository revision, and verification contract for
each condition. Context Bridge supplies identity, Guard, instruction and
handoff evidence; the external companion supplies retrieval evidence; the
operator supplies model-token, correction, and maintenance observations.
Never commit raw transcripts, local paths, credentials, Brain contents, or
private operational logs.

## Readiness and stop rules

The pilot is ready when the same registered project can produce a passing
Guarded handoff in all applicable conditions, the companion reports source and
revision provenance, and the Brain can be removed without affecting the repo
or Context Bridge. Stop and investigate if identity changes, authority is
ambiguous, provenance is missing, sensitive content is selected, or a tool
attempts an unapproved write.
