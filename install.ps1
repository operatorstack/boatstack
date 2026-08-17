$ErrorActionPreference = "Stop"

# Boatstack bootstrap trust boundary. The script installs a verified runtime;
# the kernel owns every subsequent repository mutation.

$Repository = if ($env:BOATSTACK_REPO) { $env:BOATSTACK_REPO } else { (Get-Location).Path }
$Version = if ($env:BOATSTACK_VERSION) { $env:BOATSTACK_VERSION } else { "latest" }
$Mode = if ($env:BOATSTACK_MODE) { $env:BOATSTACK_MODE } else { "install" }
$Actor = if ($env:BOATSTACK_ACTOR) { $env:BOATSTACK_ACTOR } else { "" }
$InstallDir = if ($env:BOATSTACK_INSTALL_DIR) { $env:BOATSTACK_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Boatstack\bin" }
$BoatstackHome = if ($env:BOATSTACK_HOME) { $env:BOATSTACK_HOME } else { Join-Path $env:LOCALAPPDATA "Boatstack" }

if ($Mode -notin @("install", "update", "hydrate")) { throw "Boatstack supports BOATSTACK_MODE=install, update, or hydrate" }
if ($Mode -ne "hydrate" -and -not $Actor) {
  throw "BOATSTACK_HUMAN_ACTOR_REQUIRED: install and update require an explicit BOATSTACK_ACTOR"
}
if ($Mode -eq "hydrate") {
  if ($Version -eq "latest") { throw "BOATSTACK_RUNTIME_PIN_INVALID: hydrate requires an exact BOATSTACK_VERSION" }
  if (-not $env:BOATSTACK_EXPECTED_RUNTIME_SHA256 -or $env:BOATSTACK_EXPECTED_RUNTIME_SHA256 -notmatch '^[0-9A-Fa-f]{64}$') {
    throw "BOATSTACK_RUNTIME_PIN_INVALID: hydrate requires an exact BOATSTACK_EXPECTED_RUNTIME_SHA256"
  }
}
$RepositoryOutput = & git -C $Repository rev-parse --show-toplevel
$RepositoryStatus = $LASTEXITCODE
if ($RepositoryStatus -ne 0 -or -not $RepositoryOutput) { throw "Boatstack installation requires a Git repository" }
$Repository = ($RepositoryOutput -join "`n").Trim()
$DefaultBranch = $null
if ($Mode -ne "hydrate") {
  $CurrentBranchOutput = & git -C $Repository symbolic-ref --quiet --short HEAD
  $CurrentBranchStatus = $LASTEXITCODE
  if ($CurrentBranchStatus -ne 0 -or -not $CurrentBranchOutput) { throw "Boatstack installation requires an attached branch" }
  $CurrentBranch = ($CurrentBranchOutput -join "`n").Trim()
  $RemoteDefaultOutput = & git -C $Repository symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>$null
  $RemoteDefaultStatus = $LASTEXITCODE
  $DefaultBranch = if ($RemoteDefaultStatus -eq 0 -and $RemoteDefaultOutput) {
    (($RemoteDefaultOutput -join "`n").Trim() -replace '^origin/', '')
  } else {
    $CurrentBranch
  }
  & git check-ref-format --branch $DefaultBranch | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "Boatstack could not resolve a valid default branch" }
}
$Architecture = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()) {
  "x64" { "amd64" }
  "arm64" { "arm64" }
  default { throw "unsupported architecture" }
}
$Asset = "boatstack-helper_windows_$Architecture.exe"
$Temporary = Join-Path ([System.IO.Path]::GetTempPath()) ("boatstack-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $Temporary | Out-Null

try {
  $Candidate = Join-Path $Temporary $Asset
  if ($env:BOATSTACK_BINARY) {
    if (-not $env:BOATSTACK_BINARY_SHA256) { throw "BOATSTACK_BINARY_SHA256 is required with BOATSTACK_BINARY" }
    Copy-Item -LiteralPath $env:BOATSTACK_BINARY -Destination $Candidate
    $Expected = $env:BOATSTACK_BINARY_SHA256.ToLowerInvariant()
  } else {
    $Base = if ($Version -eq "latest") {
      "https://github.com/operatorstack/boatstack/releases/latest/download"
    } else {
      "https://github.com/operatorstack/boatstack/releases/download/$Version"
    }
    try {
      Invoke-WebRequest -UseBasicParsing -Uri "$Base/$Asset" -OutFile $Candidate
      $ChecksumPath = Join-Path $Temporary "$Asset.sha256"
      Invoke-WebRequest -UseBasicParsing -Uri "$Base/$Asset.sha256" -OutFile $ChecksumPath
    } catch {
      throw "BOATSTACK_RUNTIME_ARTIFACT_UNAVAILABLE: version=$Version asset=$Asset"
    }
    $Expected = ((Get-Content -LiteralPath $ChecksumPath -Raw).Trim() -split '\s+')[0].ToLowerInvariant()
  }
  $Actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Candidate).Hash.ToLowerInvariant()
  if ($Actual -ne $Expected) {
    throw "BOATSTACK_RUNTIME_ARTIFACT_CHECKSUM_MISMATCH: version=$Version asset=$Asset expected=$Expected actual=$Actual"
  }
  if ($env:BOATSTACK_EXPECTED_RUNTIME_SHA256 -and $Actual -ne $env:BOATSTACK_EXPECTED_RUNTIME_SHA256.ToLowerInvariant()) {
    throw "BOATSTACK_RUNTIME_ARTIFACT_CHECKSUM_MISMATCH: version=$Version asset=$Asset expected=$($env:BOATSTACK_EXPECTED_RUNTIME_SHA256) actual=$Actual"
  }

  $CandidateVersionOutput = & $Candidate version
  if ($LASTEXITCODE -ne 0 -or -not $CandidateVersionOutput) { throw "Boatstack runtime did not report its version identity" }
  $CandidateVersion = ($CandidateVersionOutput -join "`n").Trim()
  $SafeVersion = [regex]::Replace($CandidateVersion, '[^A-Za-z0-9._-]', '-')
  if (-not $SafeVersion -or $SafeVersion -ne $CandidateVersion) { throw "Boatstack runtime reported an invalid version identity" }
  if ($Mode -eq "hydrate" -and $Version -ne "latest" -and $CandidateVersion -ne $Version) {
    throw "Boatstack runtime version mismatch: requested=$Version actual=$CandidateVersion"
  }
  $RuntimeDirectory = Join-Path $BoatstackHome ("runtimes\$SafeVersion-$Actual")
  New-Item -ItemType Directory -Force -Path $RuntimeDirectory | Out-Null
  $Runtime = Join-Path $RuntimeDirectory "boatstack-runtime.exe"
  if (Test-Path -LiteralPath $Runtime) {
    $Installed = (Get-FileHash -Algorithm SHA256 -LiteralPath $Runtime).Hash.ToLowerInvariant()
    if ($Installed -ne $Actual) { throw "Boatstack immutable runtime store collision" }
  } else {
    $StagedRuntime = Join-Path $RuntimeDirectory (".boatstack-runtime-" + [guid]::NewGuid().ToString("N"))
    try {
      Copy-Item -LiteralPath $Candidate -Destination $StagedRuntime
      $StagedHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $StagedRuntime).Hash.ToLowerInvariant()
      if ($StagedHash -ne $Actual) { throw "Boatstack staged runtime checksum mismatch" }
      try {
        New-Item -ItemType HardLink -Path $Runtime -Target $StagedRuntime -ErrorAction Stop | Out-Null
      } catch {
        if (-not (Test-Path -LiteralPath $Runtime)) { throw }
        $Installed = (Get-FileHash -Algorithm SHA256 -LiteralPath $Runtime).Hash.ToLowerInvariant()
        if ($Installed -ne $Actual) { throw "Boatstack immutable runtime store collision" }
      }
    } finally {
      Remove-Item -LiteralPath $StagedRuntime -Force -ErrorAction SilentlyContinue
    }
  }
  $Runtime = (Resolve-Path -LiteralPath $Runtime).Path

  if ($Mode -eq "install") {
    $ConfigSource = $env:BOATSTACK_CONFIG
    if (-not $ConfigSource) {
      $ConfigSource = Join-Path $Temporary "project.json"
      $Config = [ordered]@{
        schema_version = 4
        identity = [ordered]@{ human = [ordered]@{ kind = "literal"; value = $Actor } }
        project = [ordered]@{ name = "repository"; default_branch = $DefaultBranch; commands = [ordered]@{} }
        policy = [ordered]@{ plan_approval = "human"; visual_evidence = "optional" }
        hosts = @("cli", "cursor", "codex", "claude", "gemini", "mcp")
        projections = @("codex", "claude", "cursor", "gemini")
      }
      $ConfigText = $Config | ConvertTo-Json -Depth 4 -Compress
      [System.IO.File]::WriteAllText($ConfigSource, $ConfigText, [System.Text.UTF8Encoding]::new($false))
    }
    & $Runtime init --repo $Repository --human $Actor --param "config_path=$ConfigSource" --format text
  } elseif ($Mode -eq "update") {
    $AcceptProgramChange = @()
    if ($env:BOATSTACK_ACCEPT_PROGRAM_CHANGE -eq "true") {
      $AcceptProgramChange = @("--accept-program-change")
    }
    & $Runtime update --repo $Repository --human $Actor `
      --param "runtime_sha256=$Actual" @AcceptProgramChange --format json
  }
  if ($Mode -ne "hydrate" -and $LASTEXITCODE -ne 0) { throw "Boatstack kernel rejected installation" }

  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  $Launcher = Join-Path $InstallDir "boatstack.exe"
  if ($Mode -ne "hydrate" -or -not (Test-Path -LiteralPath $Launcher)) {
    $StagedLauncher = Join-Path $InstallDir (".boatstack-" + [guid]::NewGuid().ToString("N") + ".exe")
    Copy-Item -LiteralPath $Candidate -Destination $StagedLauncher
    Move-Item -LiteralPath $StagedLauncher -Destination $Launcher -Force
  }
  Write-Host "Boatstack installed at $Runtime"
  if ($Mode -ne "hydrate") {
    Write-Host "Review and commit $Repository\.boatstack\project.json, $Repository\.boatstack\runtime.json, and the generated host projections"
  }
  Write-Host "Run: $Launcher doctor --repo `"$Repository`" --format text"
} finally {
  Remove-Item -LiteralPath $Temporary -Recurse -Force -ErrorAction SilentlyContinue
}
