# Generated from operatorstack/intelligence-flow.
$ErrorActionPreference = "Stop"

$repository = "operatorstack/boatstack"
$version = if ($env:BOATSTACK_VERSION) { $env:BOATSTACK_VERSION } else { "latest" }
$targetRepo = if ($env:BOATSTACK_REPO) { $env:BOATSTACK_REPO } else { (Get-Location).Path }
$mode = if ($env:BOATSTACK_MODE) { $env:BOATSTACK_MODE } else { "install" }
if ($mode -notin @("install", "update")) {
    throw "BLOCKED: BOATSTACK_MODE must be install or update"
}

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    throw "BLOCKED: Git is required because Boatstack operates on reviewable repository state"
}

$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
$arch = switch ($architecture) {
    "x64" { "amd64" }
    "arm64" { "arm64" }
    default { throw "BLOCKED: unsupported Windows architecture: $architecture" }
}

$asset = "boatstack-helper_windows_${arch}.exe"
if ($mode -eq "update") {
    $currentHelper = Join-Path $targetRepo ".product-loop/bin/boatstack-helper.exe"
    if (-not (Test-Path -PathType Leaf $currentHelper)) {
        throw "BLOCKED: current Boatstack helper is missing; repair the installation before updating"
    }
    & $currentHelper doctor --repo $targetRepo
    if ($LASTEXITCODE -ne 0) {
        throw "Current Boatstack installation must pass doctor before updating"
    }
}
$base = if ($version -eq "latest") {
    "https://github.com/$repository/releases/latest/download"
} else {
    "https://github.com/$repository/releases/download/$version"
}

$temporary = Join-Path ([System.IO.Path]::GetTempPath()) ("boatstack-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $temporary | Out-Null
try {
    $binary = Join-Path $temporary $asset
    $checksum = "$binary.sha256"
    Write-Host "Downloading verified Boatstack helper for windows/$arch..."
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $binary
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset.sha256" -OutFile $checksum
    $expected = ((Get-Content -Raw $checksum).Trim() -split "\s+")[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 $binary).Hash.ToLowerInvariant()
    if ($expected -ne $actual) {
        throw "BLOCKED: Boatstack binary checksum mismatch"
    }

    $commandName = if ($mode -eq "update") { "update" } else { "init" }
    $arguments = @($commandName, "--repo", $targetRepo, "--binary", $binary)
    if ($mode -eq "install" -and $env:BOATSTACK_INTEGRATIONS) {
        $arguments += @("--integrations", $env:BOATSTACK_INTEGRATIONS)
    }
    if ($env:BOATSTACK_YES -eq "1") {
        $arguments += "--yes"
    }
    & $binary @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Boatstack initialization failed with exit code $LASTEXITCODE"
    }
} finally {
    Remove-Item -Recurse -Force $temporary -ErrorAction SilentlyContinue
}
