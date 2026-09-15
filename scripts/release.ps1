param(
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [switch]$AllowDirty,
    [switch]$AllowUntaggedSource,
    [switch]$IncludeInstaller,
    [switch]$Sign,
    [switch]$SignedRelease,
    [string]$SignToolPath = $env:CONTEXTBRIDGE_SIGNTOOL,
    [string]$SignThumbprint = $env:CONTEXTBRIDGE_SIGN_THUMBPRINT,
    [string]$SignCertificate = $env:CONTEXTBRIDGE_SIGN_CERTIFICATE,
    [string]$SignTimestampUrl = $env:CONTEXTBRIDGE_SIGN_TIMESTAMP_URL
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $repoRoot

function Fail([string]$Message) { throw $Message }

if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$') {
    Fail "Invalid release version: $Version"
}
$status = @(git status --porcelain)
if ($status.Count -gt 0 -and -not $AllowDirty) {
    Fail 'Source tree is dirty. Use a clean tagged checkout for a publishable release.'
}
$head = (git rev-parse HEAD).Trim()
if ([string]::IsNullOrWhiteSpace($head)) { Fail 'Unable to resolve source commit.' }
$tag = (git tag --points-at HEAD | Where-Object { $_ -eq $Version } | Select-Object -First 1)
if (-not $AllowUntaggedSource -and $tag -ne $Version) {
    Fail "HEAD is not tagged $Version. Use a tagged checkout or explicitly mark a local test build."
}
if ($SignedRelease -and (-not $Sign -or -not $IncludeInstaller)) {
    Fail '-SignedRelease requires both -Sign and -IncludeInstaller.'
}
$signTool = $null
if ($Sign) {
    if ([string]::IsNullOrWhiteSpace($SignToolPath)) { $SignToolPath = 'signtool.exe' }
    $signCommand = Get-Command $SignToolPath -ErrorAction SilentlyContinue
    if (-not $signCommand) { Fail "Signing requested but Authenticode tool was not found: $SignToolPath" }
    $signTool = $signCommand.Path
    if ([string]::IsNullOrWhiteSpace($SignThumbprint) -and [string]::IsNullOrWhiteSpace($SignCertificate)) {
        Fail 'Signing requested but no certificate thumbprint or certificate path was supplied.'
    }
    if ($SignedRelease -and [string]::IsNullOrWhiteSpace($SignTimestampUrl)) {
        Fail 'Signed release requires CONTEXTBRIDGE_SIGN_TIMESTAMP_URL.'
    }
}
$isccPath = $null
if ($IncludeInstaller) {
    $isccCommand = Get-Command iscc -ErrorAction SilentlyContinue
    if (-not $isccCommand) { $isccCommand = Get-Command 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe' -ErrorAction SilentlyContinue }
    if (-not $isccCommand) { Fail 'Inno Setup ISCC.exe is required for -IncludeInstaller; install it explicitly and retry.' }
    $isccPath = $isccCommand.Path
}

function Invoke-AuthenticodeSign([string]$Path) {
    $arguments = @('sign', '/fd', 'SHA256')
    if (-not [string]::IsNullOrWhiteSpace($SignTimestampUrl)) { $arguments += @('/tr', $SignTimestampUrl, '/td', 'SHA256') }
    if (-not [string]::IsNullOrWhiteSpace($SignThumbprint)) { $arguments += @('/sha1', $SignThumbprint) }
    elseif (-not [string]::IsNullOrWhiteSpace($SignCertificate)) { $arguments += @('/f', $SignCertificate) }
    else { $arguments += '/a' }
    $arguments += $Path
    & $signTool @arguments *> $null
    if ($LASTEXITCODE -ne 0) { Fail "Authenticode signing failed for $Path" }
}

function Assert-AuthenticodeSignature([string]$Path) {
    & $signTool verify /pa /all $Path *> $null
    if ($LASTEXITCODE -ne 0) { Fail "Authenticode verification failed for $Path" }
}

function Assert-ReleaseIntegrity([string]$Output, [string]$ReleaseVersion, [bool]$RequireInstaller) {
    $manifestPath = Join-Path $Output 'release-manifest.json'
    $checksumsPath = Join-Path $Output 'SHA256SUMS'
    if (-not (Test-Path $manifestPath) -or -not (Test-Path $checksumsPath)) { Fail 'Release integrity files are missing.' }
    $manifest = Get-Content $manifestPath -Raw | ConvertFrom-Json
    if ($manifest.version -ne $ReleaseVersion) { Fail 'Release manifest version mismatch.' }
    $checksumMap = @{}
    foreach ($line in Get-Content $checksumsPath) {
        $parts = $line -split '  ', 2
        if ($parts.Count -ne 2) { Fail 'Malformed SHA256SUMS entry.' }
        $checksumMap[$parts[1]] = $parts[0]
        $actual = (Get-FileHash (Join-Path $Output $parts[1]) -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $parts[0].ToLowerInvariant()) { Fail "Checksum mismatch: $($parts[1])" }
    }
    foreach ($artifact in $manifest.artifacts) {
        if (-not $checksumMap.ContainsKey($artifact.filename) -or $checksumMap[$artifact.filename].ToLowerInvariant() -ne $artifact.sha256.ToLowerInvariant()) { Fail "Manifest/checksum mismatch: $($artifact.filename)" }
    }
    if ($RequireInstaller) {
        $installerName = "contextbridge-$ReleaseVersion-windows-amd64-setup.exe"
        if (-not (Test-Path (Join-Path $Output $installerName))) { Fail 'Signed release installer is missing.' }
        if (-not ($manifest.artifacts.filename -contains $installerName)) { Fail 'Signed release installer is absent from the manifest.' }
    }
}

Write-Host 'Running release verification...'
$env:GOCACHE = Join-Path $env:TEMP 'contextbridge-release-gocache'
go test ./...
go vet ./...
$formatted = @(gofmt -l .)
if ($formatted.Count -gt 0) { Fail ("gofmt required: " + ($formatted -join ', ')) }
git diff --check
powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $repoRoot 'scripts/e2e-local.ps1')

$output = Join-Path $repoRoot ("dist\release-" + $Version)
if (Test-Path -LiteralPath $output) { Remove-Item -LiteralPath $output -Recurse -Force }
$stage = Join-Path $env:TEMP ("contextbridge-release-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $stage | Out-Null
try {
    $windowsBinary = Join-Path $stage 'contextbridge.exe'
    $linuxBinary = Join-Path $stage 'contextbridge'
    $ldflags = "-s -w -X main.version=$Version"
    go build -trimpath -buildvcs=false -ldflags $ldflags -o $windowsBinary .\cmd\contextbridge
    if ($LASTEXITCODE -ne 0) { Fail 'Windows build failed.' }
    $env:GOOS = 'linux'; $env:GOARCH = 'amd64'
    go build -trimpath -buildvcs=false -ldflags $ldflags -o $linuxBinary .\cmd\contextbridge
    if ($LASTEXITCODE -ne 0) { Fail 'Linux build failed.' }
    Remove-Item Env:GOOS,Env:GOARCH -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Path $output -Force | Out-Null
    if ($Sign) {
        Invoke-AuthenticodeSign $windowsBinary
        Assert-AuthenticodeSignature $windowsBinary
    }
    if ($IncludeInstaller) {
        Copy-Item -LiteralPath (Join-Path $repoRoot 'LICENSE') -Destination (Join-Path $stage 'LICENSE')
        $appVersion = $Version.Substring(1)
        & $isccPath "/DAppVersion=$appVersion" "/DAppVersionTag=$Version" "/DSourceDir=$stage" "/DOutputDir=$output" (Join-Path $repoRoot 'installer\contextbridge.iss')
        if ($LASTEXITCODE -ne 0) { Fail 'Installer build failed.' }
        $installer = Join-Path $output ("contextbridge-" + $Version + "-windows-amd64-setup.exe")
        if (-not (Test-Path -LiteralPath $installer)) { Fail 'Installer compiler did not produce the expected artifact.' }
        if ($Sign) {
            Invoke-AuthenticodeSign $installer
            Assert-AuthenticodeSignature $installer
        }
        go run .\cmd\contextbridge-release -version $Version -commit $head -go-version (go version) -windows-binary $windowsBinary -linux-binary $linuxBinary -installer $installer -output $output
    } else {
        go run .\cmd\contextbridge-release -version $Version -commit $head -go-version (go version) -windows-binary $windowsBinary -linux-binary $linuxBinary -output $output
    }
    if ($LASTEXITCODE -ne 0) { Fail 'Release packaging failed.' }
    Assert-ReleaseIntegrity $output $Version $SignedRelease
    Write-Host "Release artifacts written to $output"
} finally {
    Remove-Item Env:GOOS,Env:GOARCH -ErrorAction SilentlyContinue
    if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
}
