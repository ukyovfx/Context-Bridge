# Context Bridge 30-Day Pilot

The pilot compares context quality per model credit while keeping all
measurements local and human-reviewable. It does not send telemetry, create a
dashboard, or store personal Brain content in this repository.

## Four-week sequence

### Week 1: baseline

Run representative tasks with Context Bridge only. Resolve the project,
verify Guard, inspect the instruction chain, and capture the JSON handoff.

### Week 2: Repowise companion

Use Repowise only as an external retrieval companion. Keep the same task set
and Context Bridge checks. Record retrieval provenance and any repeated or
irrelevant reads. Do not integrate Repowise internals into Context Bridge.

### Week 3: Markdown Brain

Add a small personal `HOT.md` and `INDEX.md` outside the repository. Load only
the notes selected by the task. Keep repository evidence authoritative and
record any conflict instead of merging it silently.

### Week 4: comparison

Compare the three conditions: baseline, baseline plus retrieval companion, and
baseline plus retrieval companion plus Brain. Review quality per credit, not
token count alone, and record whether a human had to correct context.

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
