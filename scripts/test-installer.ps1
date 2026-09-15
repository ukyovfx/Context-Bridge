param(
    [Parameter(Mandatory = $true)]
    [string]$InstallerPath
)

$ErrorActionPreference = 'Stop'
$installer = (Resolve-Path $InstallerPath).Path
$root = Join-Path $env:TEMP ("contextbridge-installer-test-" + [guid]::NewGuid().ToString('N'))
$installDir = Join-Path $root 'install'
$stateDir = Join-Path $root 'state'
$workspaceRoot = Join-Path $root 'workspace'
$existingRepo = Join-Path $root 'existing-repository'
$fakeBin = Join-Path $root 'fake-bin'
$codexHome = Join-Path $root 'codex-home'
$gitConfig = Join-Path $root 'gitconfig'
New-Item -ItemType Directory -Path $stateDir | Out-Null
$oldHome = $env:CONTEXTBRIDGE_HOME
$oldCodexHome = $env:CODEX_HOME
$oldPath = $env:PATH
$oldGitConfigGlobal = $env:GIT_CONFIG_GLOBAL
$oldGitConfigNoSystem = $env:GIT_CONFIG_NOSYSTEM
$oldGitAuthorName = $env:GIT_AUTHOR_NAME
$oldGitAuthorEmail = $env:GIT_AUTHOR_EMAIL
$oldGitCommitterName = $env:GIT_COMMITTER_NAME
$oldGitCommitterEmail = $env:GIT_COMMITTER_EMAIL
$oldUserPath = (Get-ItemProperty 'HKCU:\Environment' -Name Path -ErrorAction SilentlyContinue).Path
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$localGitIdentityBefore = @(& git -C $repoRoot config --local --get-regexp '^user\.(name|email)$' 2>$null)
$env:CONTEXTBRIDGE_HOME = $stateDir
try {
    New-Item -ItemType Directory -Path $workspaceRoot,$existingRepo,$fakeBin,$codexHome | Out-Null
    Set-Content -LiteralPath (Join-Path $fakeBin 'gh.cmd') -Value @'
@echo off
if "%1"=="api" if "%2"=="user" (
  echo {"login":"installer-e2e","type":"User"}
  exit /b 0
)
echo HTTP 404 1>&2
exit /b 1
'@ -NoNewline
    Set-Content -LiteralPath (Join-Path $codexHome 'AGENTS.md') -Value 'Unrelated Codex instructions must survive.' -NoNewline
    $env:PATH = $fakeBin + ';' + $env:PATH
    $env:CODEX_HOME = $codexHome
    $env:GIT_CONFIG_GLOBAL = $gitConfig
    $env:GIT_CONFIG_NOSYSTEM = '1'
    $env:GIT_AUTHOR_NAME = 'Context Bridge Installer E2E'
    $env:GIT_AUTHOR_EMAIL = 'contextbridge-installer@example.invalid'
    $env:GIT_COMMITTER_NAME = $env:GIT_AUTHOR_NAME
    $env:GIT_COMMITTER_EMAIL = $env:GIT_AUTHOR_EMAIL
    & git config --global user.name $env:GIT_AUTHOR_NAME
    & git config --global user.email $env:GIT_AUTHOR_EMAIL
    $exe = Join-Path $installDir 'contextbridge.exe'
    $installEntry = $installDir.TrimEnd('\')
    $install = {
        $process = Start-Process -FilePath $installer -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=$installDir") -Wait -PassThru
        if ($process.ExitCode -ne 0) { throw "installer failed: $($process.ExitCode)" }
        if (-not (Test-Path $exe)) { throw 'installed executable missing' }
        $installedVersion = & $exe version
        if ($installedVersion -notmatch '^contextbridge v') { throw "unexpected version: $installedVersion" }
        $installedPath = (Get-ItemProperty 'HKCU:\Environment' -Name Path -ErrorAction SilentlyContinue).Path
        $installedMatches = @($installedPath -split ';' | Where-Object { $_.TrimEnd('\') -ieq $installEntry })
        if ($installedMatches.Count -ne 1) { throw 'installer PATH entry was not added exactly once' }
    }
    & $install

    # A new shell receives the persisted user PATH, not this process's PATH.
    $freshShellVersion = & powershell -NoProfile -Command "`$env:PATH = [Environment]::GetEnvironmentVariable('Path','User') + ';' + [Environment]::GetEnvironmentVariable('Path','Machine'); contextbridge version"
    if ($freshShellVersion -notmatch '^contextbridge v') { throw 'fresh-shell PATH did not resolve contextbridge' }

    & $exe setup --workspace-root $workspaceRoot --codex-bootstrap --confirm
    if ($LASTEXITCODE -ne 0) { throw 'installed setup failed' }
    if ((Get-Content -LiteralPath (Join-Path $codexHome 'AGENTS.md') -Raw) -notmatch 'Unrelated Codex instructions must survive\.') { throw 'setup overwrote unrelated Codex instructions' }
    if ((Get-Content -LiteralPath (Join-Path $codexHome 'AGENTS.md') -Raw) -notmatch 'CONTEXT BRIDGE') { throw 'setup did not add the managed Codex block' }

    & $exe new AuditTest
    if ($LASTEXITCODE -ne 0) { throw 'installed new failed' }
    $newProject = Join-Path $workspaceRoot 'active\AuditTest'
    if (-not (Test-Path (Join-Path $newProject '.contextbridge\manifest.json'))) { throw 'installed new did not create a manifest' }

    & git -C $existingRepo init -b main | Out-Null
    & git -C $existingRepo remote add origin https://github.com/installer-e2e/existing.git
    Set-Content -LiteralPath (Join-Path $existingRepo 'README.md') -Value 'existing repository'
    & git -C $existingRepo add README.md
    & git -C $existingRepo commit -m 'Initial existing repository' | Out-Null
    & $exe integrate $existingRepo --confirm
    if ($LASTEXITCODE -ne 0) { throw 'installed integrate failed' }
    if (-not (Test-Path (Join-Path $existingRepo '.contextbridge\manifest.json'))) { throw 'installed integrate did not create a manifest' }
    $bootstrap = & $exe bootstrap chatgpt existing-repository --json | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or $bootstrap.guard -ne 'PASS' -or $bootstrap.bootstrap_readiness -notmatch 'READY') { throw 'installed ChatGPT bootstrap failed' }

    # Reinstalling the same version must not duplicate the managed PATH entry or
    # disturb state owned by the CLI.
    $sentinel = Join-Path $stateDir 'user-data-sentinel.txt'
    Set-Content -LiteralPath $sentinel -Value 'preserve me' -NoNewline
    & $install
    if ((Get-Content -LiteralPath $sentinel -Raw) -ne 'preserve me') { throw 'reinstall modified user state' }
    if (-not (Test-Path (Join-Path $newProject '.contextbridge\manifest.json'))) { throw 'reinstall removed project state' }
    if ((Get-Content -LiteralPath (Join-Path $codexHome 'AGENTS.md') -Raw) -notmatch 'Unrelated Codex instructions must survive\.') { throw 'reinstall modified unrelated Codex instructions' }
    & $exe setup --dry-run *> $null
    if ($LASTEXITCODE -ne 0) { throw 'setup dry-run smoke test failed' }
    $marker = Join-Path $installDir '.contextbridge-path-owned'
    if (-not (Test-Path $marker)) { throw 'PATH ownership marker missing' }
    $uninstaller = Join-Path $installDir 'unins000.exe'
    $uninstallProcess = Start-Process -FilePath $uninstaller -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') -Wait -PassThru
    if ($uninstallProcess.ExitCode -ne 0) { throw "uninstaller failed: $($uninstallProcess.ExitCode)" }
    if (-not (Test-Path $stateDir)) { throw 'state directory was removed' }
    if (-not (Test-Path $sentinel) -or (Get-Content -LiteralPath $sentinel -Raw) -ne 'preserve me') { throw 'user state was removed' }
    $afterUninstallPath = (Get-ItemProperty 'HKCU:\Environment' -Name Path -ErrorAction SilentlyContinue).Path
    if ($afterUninstallPath -ne $oldUserPath) { throw 'user PATH was not restored' }
    if (Test-Path $exe) { throw 'uninstall did not remove the installed executable' }
    if (-not (Test-Path (Join-Path $newProject '.contextbridge\manifest.json'))) { throw 'uninstall removed project state' }
    if ((Get-Content -LiteralPath (Join-Path $codexHome 'AGENTS.md') -Raw) -notmatch 'Unrelated Codex instructions must survive\.') { throw 'uninstall modified unrelated Codex instructions' }
    $freshShellCommand = "`$env:PATH = [Environment]::GetEnvironmentVariable('Path','User') + ';' + [Environment]::GetEnvironmentVariable('Path','Machine'); if (Get-Command contextbridge -ErrorAction SilentlyContinue) { exit 1 }"
    $freshShellResult = & powershell -NoProfile -Command $freshShellCommand
    if (-not [string]::IsNullOrWhiteSpace(($freshShellResult -join ' '))) { throw "fresh shell still resolves contextbridge after uninstall: $($freshShellResult -join ' ')" }
    $localGitIdentityAfter = @(& git -C $repoRoot config --local --get-regexp '^user\.(name|email)$' 2>$null)
    if (Compare-Object $localGitIdentityBefore $localGitIdentityAfter) { throw 'installer E2E modified repository-local Git author config' }
} finally {
    if ($null -eq $oldHome) { Remove-Item Env:CONTEXTBRIDGE_HOME -ErrorAction SilentlyContinue } else { $env:CONTEXTBRIDGE_HOME = $oldHome }
    if ($null -eq $oldCodexHome) { Remove-Item Env:CODEX_HOME -ErrorAction SilentlyContinue } else { $env:CODEX_HOME = $oldCodexHome }
    $env:PATH = $oldPath
    if ($null -eq $oldGitConfigGlobal) { Remove-Item Env:GIT_CONFIG_GLOBAL -ErrorAction SilentlyContinue } else { $env:GIT_CONFIG_GLOBAL = $oldGitConfigGlobal }
    if ($null -eq $oldGitConfigNoSystem) { Remove-Item Env:GIT_CONFIG_NOSYSTEM -ErrorAction SilentlyContinue } else { $env:GIT_CONFIG_NOSYSTEM = $oldGitConfigNoSystem }
    if ($null -eq $oldGitAuthorName) { Remove-Item Env:GIT_AUTHOR_NAME -ErrorAction SilentlyContinue } else { $env:GIT_AUTHOR_NAME = $oldGitAuthorName }
    if ($null -eq $oldGitAuthorEmail) { Remove-Item Env:GIT_AUTHOR_EMAIL -ErrorAction SilentlyContinue } else { $env:GIT_AUTHOR_EMAIL = $oldGitAuthorEmail }
    if ($null -eq $oldGitCommitterName) { Remove-Item Env:GIT_COMMITTER_NAME -ErrorAction SilentlyContinue } else { $env:GIT_COMMITTER_NAME = $oldGitCommitterName }
    if ($null -eq $oldGitCommitterEmail) { Remove-Item Env:GIT_COMMITTER_EMAIL -ErrorAction SilentlyContinue } else { $env:GIT_COMMITTER_EMAIL = $oldGitCommitterEmail }
    if (Test-Path $root) { Remove-Item -LiteralPath $root -Recurse -Force }
}
