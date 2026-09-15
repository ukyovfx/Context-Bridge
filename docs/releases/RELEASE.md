# Release process

Release artifacts are produced only from an explicit `vMAJOR.MINOR.PATCH`
tag, or from an explicitly marked local test build. A publishable build must
have a clean working tree and `HEAD` must point at the requested tag.

On Windows, run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/release.ps1 -Version v1.2.0
```

The script runs verification, builds Windows amd64 and Linux amd64 binaries,
creates portable archives, and writes one output directory under `dist`. It
never creates or pushes tags, uploads releases, changes PATH, or modifies
Context Bridge state.

For local testing only, an untagged or dirty checkout must be explicitly
marked:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/release.ps1 `
  -Version v1.2.0-test.1 -AllowDirty -AllowUntaggedSource
```

The output contains the two platform archives, `SHA256SUMS`, and
`release-manifest.json`. The checksum file covers only the distributable
archives. The manifest contains version, source commit, Go version, target
OS/architecture, and archive hashes; it omits timestamps.

Archives contain only the executable, `LICENSE`, and short `README.txt`
installation guidance. No profiles, registries, secrets, repositories, or
workspace files are included.

The GitHub Actions release workflow is intentionally separate from normal CI
and remains a future publishing step. Hosted Actions availability is not
assumed; the local release script is first-class.

The Windows installer is the primary Windows distribution path. It is
currently unsigned because no real signing identity is configured. When Inno
Setup is installed explicitly, its `ISCC.exe` compiler is available on PATH (or at
`C:\Program Files (x86)\Inno Setup 6\ISCC.exe`), and `-IncludeInstaller` is
added to the local release command, it creates
`contextbridge-VERSION-windows-amd64-setup.exe`. It installs per-user
to `%LOCALAPPDATA%\Programs\ContextBridge`, adds only that directory to the
user PATH, and never touches `%LOCALAPPDATA%\ContextBridge`,
`CONTEXTBRIDGE_HOME`, repositories, or workspaces. Uninstall removes only the
installer-owned files and PATH entry. Portable ZIP remains available as the
no-installer fallback. Users should verify `SHA256SUMS`; no signing claim is
made until a real certificate and timestamp service are configured.

The isolated installer check is run with:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test-installer.ps1 `
  -InstallerPath .\dist\release-v1.2.0-test.1\contextbridge-v1.2.0-test.1-windows-amd64-setup.exe
```

The Packaging / Onboarding V1 completion audit verified the portable release
artifacts, CLI onboarding, and actual per-user installer locally with Inno Setup
7.1.0. The audit covered clean install, fresh-shell PATH/version resolution,
setup, new, integrate, ChatGPT bootstrap, same-version reinstall, preservation,
and uninstall. A separate-version upgrade fixture was not required by the
current test harness; reinstall idempotency was verified.

Signing is optional and external. `-Sign` requires an Authenticode-compatible
`signtool.exe` plus either `CONTEXTBRIDGE_SIGN_THUMBPRINT` or
`CONTEXTBRIDGE_SIGN_CERTIFICATE`. A stable signed release additionally
requires `-SignedRelease`, `-IncludeInstaller`, and
`CONTEXTBRIDGE_SIGN_TIMESTAMP_URL`. The script signs and verifies
`contextbridge.exe` before installer creation, then signs and verifies the
final installer. Missing tools, certificates, timestamps, signatures,
checksums, or manifest entries fail closed. No certificate, password, or
secret is stored in the repository.

No public release upload or publication is performed by the local script. The
GitHub workflow verifies tagged or manually selected versions and retains
artifacts for review; it does not create a public release. Winget remains
deferred.
