[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("contextbridge-e2e-" + [guid]::NewGuid().ToString('N'))
$binary = Join-Path $testRoot 'contextbridge.exe'
$dryProject = Join-Path $testRoot 'dry-run-project'
$project = Join-Path $testRoot 'local-project'

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

    Add-Content -LiteralPath (Join-Path $project 'docs\agent\CURRENT-STATE.md') -Value "`nstale-e2e"
    $previousErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $doctorOutput = & $binary doctor --project $project 2>&1
    $staleExitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousErrorActionPreference
    if ($staleExitCode -eq 0) { throw 'doctor accepted stale CURRENT-STATE' }
    if (($doctorOutput -join "`n") -notmatch 'stale CURRENT-STATE') { throw 'doctor did not classify stale CURRENT-STATE' }

    Write-Host 'local E2E passed: zero-mutation dry-run, init, manifest hashes, and stale CURRENT-STATE detection'
}
finally {
    if (Test-Path -LiteralPath $testRoot) {
        Remove-Item -LiteralPath $testRoot -Recurse -Force
    }
}
