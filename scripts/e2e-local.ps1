[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("contextbridge-e2e-" + [guid]::NewGuid().ToString('N'))
$binary = Join-Path $testRoot 'contextbridge.exe'
$registryHome = Join-Path $testRoot 'registry-home'
$dryProject = Join-Path $testRoot 'dry-run-project'
$project = Join-Path $testRoot 'local-project'
$existingRepository = Join-Path $testRoot 'existing-repository'

try {
    New-Item -ItemType Directory -Path $testRoot | Out-Null
    Push-Location $repoRoot
    try {
        go build -o $binary ./cmd/contextbridge
    }
    finally {
        Pop-Location
    }

    $before = @(Get-ChildItem -LiteralPath $testRoot -Force | Select-Object -ExpandProperty Name)
    & $binary init dry-run-project --root $testRoot --profile openai --local-only --dry-run | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'dry-run failed' }
    if (Test-Path -LiteralPath $dryProject) { throw 'dry-run created its target' }
    $after = @(Get-ChildItem -LiteralPath $testRoot -Force | Select-Object -ExpandProperty Name)
    if (Compare-Object $before $after) { throw 'dry-run mutated the projects root' }

    $env:GIT_AUTHOR_NAME = 'Context Bridge E2E'
    $env:GIT_AUTHOR_EMAIL = 'contextbridge-e2e@example.invalid'
    $env:GIT_COMMITTER_NAME = $env:GIT_AUTHOR_NAME
    $env:GIT_COMMITTER_EMAIL = $env:GIT_AUTHOR_EMAIL
    $env:CONTEXTBRIDGE_HOME = $registryHome
    & $binary init local-project --root $testRoot --profile openai --local-only | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'local init failed' }

    & $binary doctor --project $project | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'doctor failed on a fresh project' }

    $manifest = Get-Content -LiteralPath (Join-Path $project '.contextbridge\manifest.json') -Raw | ConvertFrom-Json
    foreach ($item in $manifest.files) {
        $path = Join-Path $project ($item.path -replace '/', '\')
        $actual = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $item.sha256) { throw "manifest hash mismatch: $($item.path)" }
    }
    if ($manifest.schema_version -ne 2) { throw 'init did not generate Manifest V2' }
    if ($manifest.files.path -contains 'docs/agent/CURRENT-STATE.md') { throw 'Manifest V2 hashed mutable CURRENT-STATE' }

    New-Item -ItemType Directory -Path $existingRepository | Out-Null
    & git -C $existingRepository init -b main | Out-Host
    & git -C $existingRepository remote add origin https://github.com/ukyovfx/context-bridge-e2e-fixture.git
    Set-Content -LiteralPath (Join-Path $existingRepository 'content.txt') -Value 'existing repository'
    & git -C $existingRepository add content.txt
    & git -C $existingRepository commit -m 'Existing repository fixture' | Out-Host
    $beforeRegistration = & git -C $existingRepository status --porcelain=v2 --branch
    & $binary registry register $existingRepository --dry-run | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'registry registration dry-run failed' }
    if (Test-Path -LiteralPath $registryHome) { throw 'registry dry-run created registry home' }
    $afterDryRegistration = & git -C $existingRepository status --porcelain=v2 --branch
    if (($beforeRegistration -join "`n") -ne ($afterDryRegistration -join "`n")) { throw 'registry dry-run changed repository evidence' }

    & $binary registry register $existingRepository | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'registry registration failed' }
    $afterRegistration = & git -C $existingRepository status --porcelain=v2 --branch
    if (($beforeRegistration -join "`n") -ne ($afterRegistration -join "`n")) { throw 'registry registration changed repository evidence' }
    & $binary guard --project existing-repository --workspace $existingRepository | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'guard rejected registered repository' }

    $handoff = & $binary handoff existing-repository --task 'e2e handoff' --agent codex --json
    if ($LASTEXITCODE -ne 0) { throw 'handoff failed' }
    $handoffResult = $handoff | ConvertFrom-Json
    if ($handoffResult.status -notin @('ready', 'ready_with_warnings')) { throw 'handoff omitted a ready terminal status' }
    if (-not $handoffResult.handoff.guard.allowed) { throw 'handoff guard did not pass' }
    if ($handoffResult.handoff.task_intent -ne 'e2e handoff') { throw 'handoff task intent mismatch' }

    $registryBeforeDiscovery = (Get-FileHash -LiteralPath (Join-Path $registryHome 'registry-v1.json') -Algorithm SHA256).Hash
    $discovery = & $binary discover --root $testRoot --json
    if ($LASTEXITCODE -ne 0) { throw 'discovery failed' }
    $discoveryResult = $discovery | ConvertFrom-Json
    if ($discoveryResult.status -notin @('success', 'success_with_warnings', 'partial', 'no_candidates')) { throw 'discovery omitted a valid terminal status' }
    if ($discoveryResult.requested_root -ne $testRoot) { throw 'discovery omitted the requested root' }
    $registryAfterDiscovery = (Get-FileHash -LiteralPath (Join-Path $registryHome 'registry-v1.json') -Algorithm SHA256).Hash
    if ($registryBeforeDiscovery -ne $registryAfterDiscovery) { throw 'discovery mutated registry' }

    Add-Content -LiteralPath (Join-Path $project 'docs\agent\CURRENT-STATE.md') -Value "`nstale-e2e"
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $doctorOutput = & $binary doctor --project $project 2>&1
    $staleExitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousErrorActionPreference
    if ($staleExitCode -eq 0) { throw 'doctor accepted stale CURRENT-STATE' }
    if (($doctorOutput -join "`n") -notmatch 'CURRENT-STATE has uncommitted content changes') { throw 'doctor did not classify changed CURRENT-STATE' }

    Write-Host 'local E2E passed: zero-mutation dry-runs, Manifest V2 hashes, registry, guard, discovery, and stale CURRENT-STATE detection'
}
finally {
    Remove-Item Env:CONTEXTBRIDGE_HOME -ErrorAction SilentlyContinue
    if (Test-Path -LiteralPath $testRoot) {
        Remove-Item -LiteralPath $testRoot -Recurse -Force
    }
}
