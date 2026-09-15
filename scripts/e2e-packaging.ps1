[CmdletBinding()]
param([string]$InstallerPath = '')

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$root = Join-Path $env:TEMP ("contextbridge-packaging-e2e-" + [guid]::NewGuid().ToString('N'))
$binary = Join-Path $root 'contextbridge.exe'
$fakeBin = Join-Path $root 'fake-bin'
$state = Join-Path $root 'state'
$workspace = Join-Path $root 'workspace'
$existing = Join-Path $root 'existing'
$gitConfig = Join-Path $root 'gitconfig'
$localGitIdentityBefore = @(& git -C $repoRoot config --local --get-regexp '^user\.(name|email)$' 2>$null)

try {
    New-Item -ItemType Directory -Path $root | Out-Null
    New-Item -ItemType Directory -Path $fakeBin,$state,$workspace,$existing | Out-Null
    Set-Content -LiteralPath (Join-Path $fakeBin 'gh.cmd') -Value @'
@echo off
if "%1"=="api" if "%2"=="user" (
  echo {"login":"e2e-user","type":"User"}
  exit /b 0
)
echo HTTP 404 1>&2
exit /b 1
'@ -NoNewline
    $env:PATH = $fakeBin + ';' + [Environment]::GetEnvironmentVariable('PATH','Process')
    $env:CONTEXTBRIDGE_HOME = $state
    $env:CODEX_HOME = Join-Path $root 'codex-home'
    $env:GIT_CONFIG_GLOBAL = $gitConfig
    $env:GIT_CONFIG_NOSYSTEM = '1'
    $env:GIT_AUTHOR_NAME = 'Context Bridge Packaging E2E'
    $env:GIT_AUTHOR_EMAIL = 'contextbridge-packaging@example.invalid'
    $env:GIT_COMMITTER_NAME = $env:GIT_AUTHOR_NAME
    $env:GIT_COMMITTER_EMAIL = $env:GIT_AUTHOR_EMAIL
    & git config --global user.name 'Context Bridge Packaging E2E'
    & git config --global user.email 'contextbridge-packaging@example.invalid'
    Push-Location $repoRoot
    try { go build -trimpath -buildvcs=false -ldflags '-s -w -X main.version=v1.2.0-packaging-e2e' -o $binary .\cmd\contextbridge }
    finally { Pop-Location }
    if ($LASTEXITCODE -ne 0) { throw 'standalone binary build failed' }
    $version = & $binary version
    if ($version -ne 'contextbridge v1.2.0-packaging-e2e') { throw 'standalone version check failed' }
    $env:PATH = (Split-Path $binary) + ';' + $env:PATH
    if ((& powershell -NoProfile -Command 'contextbridge version') -ne $version) { throw 'fresh-shell PATH check failed' }

    & $binary setup --workspace-root $workspace --codex-bootstrap --confirm
    if ($LASTEXITCODE -ne 0) { throw 'setup failed' }
    & $binary new E2ENew
    if ($LASTEXITCODE -ne 0) { throw 'new failed' }
    if (-not (Test-Path (Join-Path $workspace 'active\E2ENew\.contextbridge\manifest.json'))) { throw 'new manifest missing' }

    & git -C $existing init -b main | Out-Null
    & git -C $existing remote add origin https://github.com/e2e-user/existing.git
    Set-Content -LiteralPath (Join-Path $existing 'README.md') -Value 'existing'
    & git -C $existing add README.md
    & git -C $existing commit -m initial | Out-Null
    & $binary integrate $existing --confirm | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'integrate failed' }
    $bootstrap = & $binary bootstrap chatgpt existing --json | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or $bootstrap.guard -ne 'PASS' -or $bootstrap.bootstrap_readiness -notmatch 'READY') { throw 'ChatGPT bootstrap failed' }
    if (-not (Test-Path (Join-Path $existing '.contextbridge\manifest.json'))) { throw 'integrated manifest missing' }

    if ($InstallerPath -ne '') {
        & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $repoRoot 'scripts\test-installer.ps1') -InstallerPath $InstallerPath
        if ($LASTEXITCODE -ne 0) { throw 'installer E2E failed' }
    } else {
        Write-Host 'installer E2E skipped: pass -InstallerPath to test a compiled Inno Setup installer'
    }
    $localGitIdentityAfter = @(& git -C $repoRoot config --local --get-regexp '^user\.(name|email)$' 2>$null)
    if (Compare-Object $localGitIdentityBefore $localGitIdentityAfter) { throw 'packaging E2E modified repository-local Git author config' }
    Write-Host 'packaging E2E passed: standalone binary, fresh-shell PATH, setup, new, integrate, and ChatGPT bootstrap'
}
finally {
    Remove-Item Env:CONTEXTBRIDGE_HOME,Env:CODEX_HOME,Env:GIT_CONFIG_GLOBAL,Env:GIT_CONFIG_NOSYSTEM,Env:GIT_AUTHOR_NAME,Env:GIT_AUTHOR_EMAIL,Env:GIT_COMMITTER_NAME,Env:GIT_COMMITTER_EMAIL -ErrorAction SilentlyContinue
    if (Test-Path $root) { Remove-Item -LiteralPath $root -Recurse -Force }
}
