# Generated from operatorstack/intelligence-flow.
[CmdletBinding()]
param(
    [switch]$Repair,
    [switch]$AllowDowngrade
)
$ErrorActionPreference = "Stop"

$repository = "operatorstack/boatstack"
$version = if ($env:BOATSTACK_VERSION) { $env:BOATSTACK_VERSION } else { "latest" }
$targetRepo = if ($env:BOATSTACK_REPO) { $env:BOATSTACK_REPO } else { (Get-Location).Path }
$mode = if ($env:BOATSTACK_MODE) { $env:BOATSTACK_MODE } else { "install" }
$repairRequested = $Repair -or $env:BOATSTACK_REPAIR -eq "1"
$downgradeRequested = $AllowDowngrade -or $env:BOATSTACK_ALLOW_DOWNGRADE -eq "1"
if ($mode -notin @("install", "update")) {
    throw "BLOCKED: BOATSTACK_MODE must be install or update"
}

$existingGeneratedLock = Test-Path -PathType Leaf (Join-Path $targetRepo ".product-loop/generated.lock.json")
$existingHelper = (Test-Path -PathType Leaf (Join-Path $targetRepo ".product-loop/bin/boatstack-helper")) -or (Test-Path -PathType Leaf (Join-Path $targetRepo ".product-loop/bin/boatstack-helper.exe"))
if ($mode -eq "install" -and ($existingGeneratedLock -or $existingHelper)) {
    if ($repairRequested) {
        $mode = "update"
        Write-Host "Existing Boatstack installation detected; preserving its configuration and using update repair semantics."
    } else {
        throw "BLOCKED: Boatstack is already installed; use BOATSTACK_MODE=update, or add -Repair when owned control state prevents updating"
    }
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

    if ($mode -eq "update") {
        $currentHelper = Join-Path $targetRepo ".product-loop/bin/boatstack-helper.exe"
        if (Test-Path -PathType Leaf $currentHelper) {
            try {
                & $currentHelper doctor --repo $targetRepo
                if ($LASTEXITCODE -ne 0) {
                    Write-Warning "Current Boatstack doctor reported drift; the verified target helper will classify whether it is safely repairable."
                }
            } catch {
                Write-Warning "Current Boatstack doctor reported drift; the verified target helper will classify whether it is safely repairable."
            }
        } else {
            Write-Warning "Current Boatstack helper is missing; the verified target helper will classify whether it is safely repairable."
        }
    }

    $commandName = if ($mode -eq "update") { "update" } else { "init" }
    $arguments = @($commandName, "--repo", $targetRepo, "--binary", $binary)
    if ($mode -eq "install" -and $env:BOATSTACK_INTEGRATIONS) {
        $arguments += @("--integrations", $env:BOATSTACK_INTEGRATIONS)
    }
    if ($env:BOATSTACK_YES -eq "1") {
        $arguments += "--yes"
    }
    if ($repairRequested) {
        $arguments += "--repair"
    }
    if ($downgradeRequested) {
        $arguments += "--allow-downgrade"
    }
    & $binary @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "Boatstack initialization failed with exit code $LASTEXITCODE"
    }
} finally {
    Remove-Item -Recurse -Force $temporary -ErrorAction SilentlyContinue
}
