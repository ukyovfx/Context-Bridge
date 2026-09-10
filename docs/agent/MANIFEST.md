# Manifest and Identity

`.contextbridge/manifest.json` records portable project and repository identity plus generated-template provenance. It never records a machine-local workspace path.

## Manifest V2

- `project.id` is a `prj_<UUIDv4>` identifier.
- `repository_identity.id` is a `repo_<UUIDv4>` identifier.
- `repository_identity.identity` is the normalized primary-remote host, meaningful non-default port, and case-preserved repository path.
- `repository_identity.primary_remote_name` is normally `origin`.
- `repository_identity.canonical_branch` records the expected canonical branch.
- `files` contains SHA-256 provenance for immutable generated templates and labels each entry with `kind`.

`docs/agent/CURRENT-STATE.md` is intentionally excluded from V2 file hashes. Its content integrity, basis validity, and basis freshness are evaluated separately from its front matter and Git evidence.

Manifest V1 remains readable. Context Bridge does not silently rewrite V1 manifests or automatically upgrade existing repositories.

## Machine-local registry

Workspace IDs (`ws_<UUIDv4>`), canonical paths, Git roots, git-dir values, and git-common-dir values live only in `%LOCALAPPDATA%\ContextBridge\registry-v1.json`, or under `CONTEXTBRIDGE_HOME` when explicitly overridden. Registry writes validate the complete document and use an exclusive lock, same-directory temporary file, safe replacement, and last-known-valid backup.
