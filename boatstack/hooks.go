package boatstack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const hookCommandMarker = ".product-loop/hooks/guard"

type HookDiagnostic struct {
	Host              string
	ContractStatus    string
	LiveEventObserved bool
}

func canonicalHookEvent(host string) ([]byte, error) {
	switch host {
	case "cursor":
		return []byte(`{"hook_event_name":"beforeShellExecution","command":"git status --short"}`), nil
	case "claude", "codex":
		return []byte(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status --short"}}`), nil
	case "gemini":
		return []byte(`{"hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"git status --short"}}`), nil
	default:
		return nil, fmt.Errorf("unsupported hook host %q; expected cursor, claude, codex, or gemini", host)
	}
}

func validateCanonicalHookOutput(host string, output []byte) error {
	if host == "cursor" {
		var decision map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(output), &decision); err != nil || stringValue(decision["permission"]) != "allow" {
			return fmt.Errorf("cursor hook diagnostic returned a malformed or non-allow response")
		}
		return nil
	}
	if host == "gemini" {
		var decision map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(output), &decision); err != nil || stringValue(decision["decision"]) != "allow" {
			return fmt.Errorf("gemini hook diagnostic returned a malformed or non-allow response")
		}
		return nil
	}
	if len(bytes.TrimSpace(output)) != 0 {
		return fmt.Errorf("%s hook diagnostic returned unexpected allow output", host)
	}
	return nil
}

var hookDiagnosticRunner = runInstalledHookDiagnostic

func runInstalledHookDiagnostic(ctx context.Context, repo, host string, input []byte) ([]byte, error) {
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		path := filepath.Join(repo, ".product-loop", "hooks", "guard.ps1")
		command = exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path, "-HostName", host)
	} else {
		path := filepath.Join(repo, ".product-loop", "hooks", "guard.sh")
		command = exec.CommandContext(ctx, "bash", path, host)
	}
	command.Dir = repo
	command.Stdin = bytes.NewReader(append(input, '\n'))
	channels, err := runCommandChannels(command)
	if err != nil {
		return channels.Stdout, commandFailure(channels, err)
	}
	return channels.Stdout, nil
}

// DiagnoseHook runs the installed guard with a canonical, read-only event. It
// proves the generated wrapper, shared runtime, decoder, and allow contract; it
// deliberately cannot observe the coding host's live event payload.
func DiagnoseHook(repoPath, hostName string) (HookDiagnostic, error) {
	host := strings.ToLower(strings.TrimSpace(hostName))
	input, err := canonicalHookEvent(host)
	if err != nil {
		return HookDiagnostic{}, err
	}
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return HookDiagnostic{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	output, runErr := hookDiagnosticRunner(ctx, repo, host, input)
	if ctx.Err() != nil {
		return HookDiagnostic{}, fmt.Errorf("%s hook diagnostic timed out", host)
	}
	if runErr != nil {
		detail := boundedObservation(strings.TrimSpace(string(output) + " " + runErr.Error()))
		return HookDiagnostic{}, fmt.Errorf("%s hook diagnostic failed: %s", host, detail)
	}
	if err := validateCanonicalHookOutput(host, output); err != nil {
		return HookDiagnostic{}, err
	}
	return HookDiagnostic{Host: host, ContractStatus: "PASS", LiveEventObserved: false}, nil
}

// runtimeHydrateCommandBash / runtimeHydrateCommandPowerShell are the single
// source of truth for the pinned, verified installer invocation that populates
// an absent shared-runtime slot. They mirror installationRepairRetryCommand but
// target the branch-free `hydrate` mode, so they are safe to run from any
// checkout on any branch without rewriting committed generated files. Each is
// used both as its guard's default auto-hydrate command and, embedded in the
// fail-closed deny message, as a human copy-paste self-heal. They are keyed by
// target shell (not the generating host's GOOS) because both guard scripts are
// generated on every platform. The bash form must contain no single quote: the
// guard wraps it in a single-quoted default.
func runtimeHydrateCommandBash(version string) string {
	return `BOATSTACK_MODE=hydrate BOATSTACK_VERSION=` + version + ` BOATSTACK_REPO="$PWD" /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/operatorstack/boatstack/` + version + `/install.sh)"`
}

func runtimeHydrateCommandPowerShell(version string) string {
	return `$env:BOATSTACK_MODE="hydrate"; $env:BOATSTACK_VERSION="` + version + `"; $env:BOATSTACK_REPO=(Get-Location).Path; irm https://raw.githubusercontent.com/operatorstack/boatstack/` + version + `/install.ps1 | iex`
}

func guardShellScript() []byte {
	return []byte(fmt.Sprintf(`#!/usr/bin/env bash
# Generated by Boatstack. Do not edit; change canonical source or .boatstack-project.json.
set -u

# A denial is a guardrail, not a crash. On a real terminal render a soft-coral
# badge; when stderr is piped/captured (a host UI, a log) emit the plain message
# unchanged. Honors NO_COLOR and BOATSTACK_COLOR=never.
bs_color=0
case "${BOATSTACK_COLOR:-auto}" in
  always|1|true|yes|on) bs_color=1 ;;
  never|0|false|no|off) bs_color=0 ;;
  *) if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then bs_color=1; fi ;;
esac
bs_deny() {
  if [ "$bs_color" = 1 ]; then
    printf '\033[48;2;229;146;128m\033[38;2;23;24;28m\033[1m ⊘ Blocked by Boatstack \033[0m %%s\n' "$1" >&2
  else
    printf '%%s\n' "$1" >&2
  fi
}

HOST="${1:-}"
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$ROOT" ]]; then
	exit 0
fi

# Repository presence is not engagement. This worktree-local lease probe runs
# before platform detection, runtime discovery, checksum work, or hydration.
# Missing, unsafe, stale-branch, and malformed evidence is inert here; the
# trusted helper validates the complete lease and delivery state when active.
GIT_DIR="$(git rev-parse --path-format=absolute --git-dir 2>/dev/null || true)"
LEASE="$GIT_DIR/boatstack/engagement.json"
if [[ -z "$GIT_DIR" || ! -f "$LEASE" || -L "$LEASE" ]]; then
	exit 0
fi
LEASE_BRANCH="$(sed -n 's/.*"branch"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$LEASE" | head -n 1)"
CURRENT_BRANCH="$(git branch --show-current 2>/dev/null || true)"
if [[ -z "$LEASE_BRANCH" || "$LEASE_BRANCH" != "$CURRENT_BRANCH" ]]; then
	exit 0
fi

COMMON="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null || true)"
if [[ -z "$COMMON" ]]; then
  bs_deny "Boatstack safety guard could not resolve the Git common directory; denying tool execution."
  exit 2
fi

case "$(uname -s)" in
  Darwin) OS_NAME="darwin"; EXTENSION="" ;;
  Linux) OS_NAME="linux"; EXTENSION="" ;;
  MINGW*|MSYS*|CYGWIN*) OS_NAME="windows"; EXTENSION=".exe" ;;
  *) bs_deny "Boatstack safety guard found an unsupported operating system; denying tool execution."; exit 2 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) bs_deny "Boatstack safety guard found an unsupported architecture; denying tool execution."; exit 2 ;;
esac

HELPER="$COMMON/boatstack/runtimes/%s/%s/${OS_NAME}-${ARCH}/boatstack-helper${EXTENSION}"
MANIFEST="$COMMON/boatstack/runtimes/%s/%s/${OS_NAME}-${ARCH}/runtime.lock.json"
HYDRATE_LOCK="$COMMON/boatstack/hydrate-%s.lock"
# Auto-hydrate a missing or incomplete shared-runtime slot. A teammate who pulls
# a version bump or clones fresh inherits the committed pointers (this guard's
# baked version path) but an empty, gitignored slot, so without this the very next
# tool call would hard-deny before any Go runs. On an incomplete slot we run the
# tag-pinned, checksum-verifying installer in branch-free hydrate mode, serialize
# clone-wide with an atomic mkdir lock, and bound the attempt. This is purely
# additive: the existing missing/symlink/checksum gates below stay authoritative
# and fail-closed, so a disabled, timed-out, or failed hydration simply denies.
# The entry test mirrors those gates (helper AND manifest present, non-symlink):
# an installer copies the helper before the manifest, so a peer arriving in that
# window must join the lock and wait, not skip the block and deny a half-slot.
if { [[ ! -x "$HELPER" || -L "$HELPER" || ! -f "$MANIFEST" || -L "$MANIFEST" ]]; } && [[ "${BOATSTACK_AUTO_HYDRATE:-1}" != "0" ]]; then
  mkdir -p "$COMMON/boatstack" 2>/dev/null || true
  if mkdir "$HYDRATE_LOCK" 2>/dev/null; then
    # Double-checked locking. A slow guard can reach this mkdir only after the
    # winner already hydrated and released the lock, so its mkdir succeeds too.
    # Re-test the slot now that we hold the lock and run the installer only if it
    # is still missing or incomplete, so exactly one hydration happens under
    # contention (redundant installer runs also widen the exec-time race window).
    if [[ ! -x "$HELPER" || -L "$HELPER" || ! -f "$MANIFEST" || -L "$MANIFEST" ]]; then
      (
        cd "$ROOT" || exit 0
        export BOATSTACK_MODE=hydrate
        export BOATSTACK_VERSION="%s"
        export BOATSTACK_REPO="$ROOT"
        HYDRATE_COMMAND="${BOATSTACK_HYDRATE_COMMAND:-}"
        if [[ -z "$HYDRATE_COMMAND" ]]; then
          HYDRATE_COMMAND='%s'
        fi
        if command -v timeout >/dev/null 2>&1; then
          timeout 8 /bin/bash -c "$HYDRATE_COMMAND"
        else
          /bin/bash -c "$HYDRATE_COMMAND"
        fi
      ) >&2 || true
    fi
    rmdir "$HYDRATE_LOCK" 2>/dev/null || true
  else
    # A peer holds the hydrate lock. Wait for the peer to finish — it removes the
    # lock only after its hydrate command returns — before inspecting the slot, so
    # a waiter never observes a half-written runtime (for example the helper copied
    # but the manifest not yet in place). A released lock means the slot is as
    # complete as it will get; the authoritative gates below then accept it or fail
    # closed. Bound the wait above the peer's own hydrate timeout so a slow but
    # succeeding peer still wins.
    for _ in $(seq 1 12); do
      [[ -d "$HYDRATE_LOCK" ]] || break
      sleep 1
    done
  fi
fi
# Both paths can become visible while the writer still owns the lock. Treat
# lock release, not path existence, as the shared-runtime publication point.
for _ in $(seq 1 12); do
  [[ -d "$HYDRATE_LOCK" ]] || break
  sleep 1
done
if [[ -d "$HYDRATE_LOCK" ]]; then
  bs_deny "Boatstack shared runtime hydration did not complete; denying tool execution."
  exit 2
fi
if [[ ! -x "$HELPER" ]]; then
  bs_deny "Boatstack shared runtime is missing; run the verified installer once from any checkout in this Git clone:"
  echo "  %s" >&2
  exit 2
fi
if [[ -L "$HELPER" || ! -f "$MANIFEST" || -L "$MANIFEST" ]]; then
  bs_deny "Boatstack shared runtime is unsafe or incomplete; rerun the verified tagged installer."
  exit 2
fi
EXPECTED="$(sed -n 's/.*"binary_sha256"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{64\}\)".*/\1/p' "$MANIFEST" | head -n 1)"
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "$HELPER" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "$HELPER" | awk '{print $1}')"
else
  bs_deny "Boatstack cannot verify the shared runtime checksum; denying tool execution."
  exit 2
fi
if [[ -z "$EXPECTED" || "$ACTUAL" != "$EXPECTED" ]]; then
  bs_deny "Boatstack shared runtime checksum is invalid; rerun the verified tagged installer."
  exit 2
fi

# Linux refuses to exec a file another process still holds open for writing
# (ETXTBSY, surfaced as exit 126). Under concurrent first use a peer guard can be
# finishing hydration at this instant, even though the writer replaces the binary
# atomically. Retry briefly, then hand off. A genuinely non-executable helper keeps
# returning 126 and the final status still propagates unchanged. Running the helper
# as a child (not exec) is required so a failed start is observable; stdio and the
# exit code pass through, and an ETXTBSY start never consumes stdin.
ATTEMPT=0
while :; do
  "$HELPER" bootstrap-safety-hook --host "$HOST" --repo "$ROOT"
  HELPER_STATUS=$?
  if [[ $HELPER_STATUS -eq 126 && $ATTEMPT -lt 30 ]]; then
    ATTEMPT=$((ATTEMPT + 1))
    sleep 0.1
    continue
  fi
  exit $HELPER_STATUS
done
`, Version, SourceCommit, Version, SourceCommit, Version, Version, runtimeHydrateCommandBash(Version), runtimeHydrateCommandBash(Version)))
}

func guardPowerShellScript() []byte {
	return []byte(fmt.Sprintf(`# Generated by Boatstack. Do not edit; change canonical source or .boatstack-project.json.
param([Parameter(Mandatory=$true)][string]$HostName)
$ErrorActionPreference = "Stop"

# A denial is a guardrail, not a crash. On a real console render a soft-coral
# badge; when stderr is redirected (a host UI, a log) emit the plain message
# unchanged. Honors NO_COLOR and BOATSTACK_COLOR=never. ESC via [char]27 (no
# backtick — this script is a Go raw string).
$bsColor = $false
switch ("$($env:BOATSTACK_COLOR)".ToLowerInvariant()) {
  { $_ -in 'always','1','true','yes','on' } { $bsColor = $true }
  { $_ -in 'never','0','false','no','off' } { $bsColor = $false }
  default { if ((-not [Console]::IsErrorRedirected) -and (-not $env:NO_COLOR)) { $bsColor = $true } }
}
function Bs-Deny($msg) {
  if ($bsColor) {
    $e = [char]27
    [Console]::Error.WriteLine("$e[48;2;229;146;128m$e[38;2;23;24;28m$e[1m ⊘ Blocked by Boatstack $e[0m $msg")
  } else {
    [Console]::Error.WriteLine($msg)
  }
}
$root = (& git rev-parse --show-toplevel 2>$null)
if (-not $root) {
	exit 0
}
$gitDir = (& git rev-parse --path-format=absolute --git-dir 2>$null)
if (-not $gitDir) { exit 0 }
$leasePath = Join-Path $gitDir "boatstack/engagement.json"
if (-not (Test-Path -LiteralPath $leasePath -PathType Leaf)) { exit 0 }
$leaseInfo = Get-Item -LiteralPath $leasePath
if ($leaseInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) { exit 0 }
try { $lease = Get-Content -LiteralPath $leasePath -Raw | ConvertFrom-Json } catch { exit 0 }
$currentBranch = (& git branch --show-current 2>$null)
if (-not $lease.branch -or $lease.branch -ne $currentBranch) { exit 0 }
$common = (& git rev-parse --path-format=absolute --git-common-dir 2>$null)
if (-not $common) {
  Bs-Deny "Boatstack safety guard could not resolve the Git common directory; denying tool execution."
  exit 2
}
$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
$arch = switch ($architecture) {
  "x64" { "amd64" }
  "arm64" { "arm64" }
  default {
    Bs-Deny "Boatstack safety guard found an unsupported architecture; denying tool execution."
    exit 2
  }
}
$helper = Join-Path $common "boatstack/runtimes/%s/%s/windows-$arch/boatstack-helper.exe"
$manifestPath = Join-Path $common "boatstack/runtimes/%s/%s/windows-$arch/runtime.lock.json"
$bsCommon = Join-Path $common "boatstack"
$hydrateLock = Join-Path $bsCommon "hydrate-%s.lock"
# Auto-hydrate a missing shared-runtime slot (see the bash guard for rationale):
# a teammate who pulls a version bump or clones fresh inherits the committed
# pointers but an empty, gitignored slot. On an absent slot we run the tag-pinned,
# checksum-verifying installer in branch-free hydrate mode, serialized clone-wide
# with an atomic directory lock. Purely additive: the gates below stay
# authoritative and fail-closed if hydration is disabled, fails, or is skipped.
if (((-not (Test-Path -LiteralPath $helper -PathType Leaf)) -or (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf))) -and $env:BOATSTACK_AUTO_HYDRATE -ne "0") {
  New-Item -ItemType Directory -Path $bsCommon -Force -ErrorAction SilentlyContinue | Out-Null
  $acquired = $false
  try { New-Item -ItemType Directory -Path $hydrateLock -ErrorAction Stop | Out-Null; $acquired = $true } catch { $acquired = $false }
  if ($acquired) {
    try {
      $env:BOATSTACK_MODE = "hydrate"
      $env:BOATSTACK_VERSION = "%s"
      $env:BOATSTACK_REPO = $root
      $hydrateCommand = $env:BOATSTACK_HYDRATE_COMMAND
      if (-not $hydrateCommand) {
        $hydrateCommand = 'irm https://raw.githubusercontent.com/operatorstack/boatstack/%s/install.ps1 | iex'
      }
      & powershell -NoProfile -Command $hydrateCommand 2>&1 | ForEach-Object { [Console]::Error.WriteLine($_) }
    } catch {
    } finally {
      Remove-Item -LiteralPath $hydrateLock -Recurse -Force -ErrorAction SilentlyContinue
    }
  } else {
    # Wait for the peer to release the lock (it does so only after its hydrate
    # command returns) before inspecting the slot, so a waiter never observes a
    # half-written runtime. The authoritative gates below then accept it or fail
    # closed. Bound the wait above the peer's own hydrate timeout.
    for ($i = 0; $i -lt 12; $i++) {
      if (-not (Test-Path -LiteralPath $hydrateLock)) { break }
      Start-Sleep -Seconds 1
    }
  }
}
# A ready-looking slot is not published until its writer releases the lock.
for ($i = 0; $i -lt 12 -and (Test-Path -LiteralPath $hydrateLock); $i++) { Start-Sleep -Seconds 1 }
if (Test-Path -LiteralPath $hydrateLock) {
  Bs-Deny "Boatstack shared runtime hydration did not complete; denying tool execution."
  exit 2
}
if (-not (Test-Path -LiteralPath $helper -PathType Leaf)) {
  Bs-Deny "Boatstack shared runtime is missing; run the verified installer once from any checkout in this Git clone:"
  [Console]::Error.WriteLine("  %s")
  exit 2
}
$helperInfo = Get-Item -LiteralPath $helper
if (($helperInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) -or -not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
  Bs-Deny "Boatstack shared runtime is unsafe or incomplete; rerun the verified tagged installer."
  exit 2
}
$manifestInfo = Get-Item -LiteralPath $manifestPath
if ($manifestInfo.Attributes -band [IO.FileAttributes]::ReparsePoint) {
  Bs-Deny "Boatstack shared runtime manifest is unsafe; rerun the verified tagged installer."
  exit 2
}
try {
  $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
  $actual = (Get-FileHash -LiteralPath $helper -Algorithm SHA256).Hash.ToLowerInvariant()
} catch {
  Bs-Deny "Boatstack could not verify the shared runtime; denying tool execution."
  exit 2
}
if (-not $manifest.binary_sha256 -or $actual -ne $manifest.binary_sha256.ToLowerInvariant()) {
  Bs-Deny "Boatstack shared runtime checksum is invalid; rerun the verified tagged installer."
  exit 2
}
& $helper bootstrap-safety-hook --host $HostName --repo $root
exit $LASTEXITCODE
`, Version, SourceCommit, Version, SourceCommit, Version, Version, Version, runtimeHydrateCommandPowerShell(Version)))
}

func hookCommand(host string) string {
	if host == "claude" {
		return `bash "${CLAUDE_PROJECT_DIR}/.product-loop/hooks/guard.sh" claude`
	}
	return `bash "$(git rev-parse --show-toplevel)/.product-loop/hooks/guard.sh" ` + host
}

func hookCommandWindows(host string) string {
	return `powershell -NoProfile -ExecutionPolicy Bypass -Command "$r = & git rev-parse --show-toplevel; & (Join-Path $r '.product-loop/hooks/guard.ps1') ` + host + `"`
}

func desiredHostHookForEvent(host, event string) map[string]any {
	switch host {
	case "cursor":
		entry := map[string]any{
			"command": hookCommand(host), "commandWindows": hookCommandWindows(host),
			"failClosed": true, "timeout": 10,
		}
		if event == "preToolUse" || event == "postToolUse" {
			entry["matcher"] = "Write|Edit|ApplyPatch|Create|Delete|Move|Rename"
		}
		return entry
	case "claude":
		return map[string]any{
			"matcher": "Bash|Shell|Write|Edit|ApplyPatch|Create|Delete|Move|Rename|mcp__.*",
			"hooks": []any{map[string]any{
				"type": "command", "command": hookCommand(host),
				"shell": "bash", "timeout": 10,
			}},
		}
	case "codex":
		return map[string]any{
			"matcher": "Bash|Shell|Write|Edit|ApplyPatch|Create|Delete|Move|Rename|mcp__.*",
			"hooks": []any{map[string]any{
				"type": "command", "command": hookCommand(host), "commandWindows": hookCommandWindows(host),
				"timeout": 10,
			}},
		}
	case "gemini":
		return map[string]any{
			"matcher": ".*", "sequential": true,
			"hooks": []any{map[string]any{
				"name": "boatstack-engagement-probe", "type": "command", "command": hookCommand(host),
				"timeout": 10000,
			}},
		}
	default:
		return map[string]any{}
	}
}

func hookEvents(host string) []string {
	if host == "cursor" {
		return []string{"preToolUse", "postToolUse", "postToolUseFailure", "beforeShellExecution", "afterShellExecution", "beforeMCPExecution", "afterMCPExecution"}
	}
	if host == "gemini" {
		return []string{"BeforeTool", "AfterTool"}
	}
	if host == "claude" {
		return []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"}
	}
	return []string{"PreToolUse", "PostToolUse"}
}

func desiredHostHook(host string) map[string]any {
	return desiredHostHookForEvent(host, hookEvents(host)[0])
}

func hookFragmentJSON(host string) ([]byte, error) {
	events := map[string]any{}
	for _, event := range hookEvents(host) {
		events[event] = desiredHostHookForEvent(host, event)
	}
	return GeneratedJSON(map[string]any{"schema_version": 1, "host": host, "events": events})
}

func hostHookConfigPath(repo, host string) string {
	switch host {
	case "cursor":
		return filepath.Join(repo, ".cursor", "hooks.json")
	case "claude":
		return filepath.Join(repo, ".claude", "settings.json")
	case "codex":
		return filepath.Join(repo, ".codex", "hooks.json")
	case "gemini":
		return filepath.Join(repo, ".gemini", "settings.json")
	default:
		return ""
	}
}

func HostHookPaths(adapters []string) []string {
	paths := []string{}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if contains(adapters, host) {
			paths = append(paths, filepath.ToSlash(strings.TrimPrefix(hostHookConfigPath("", host), string(filepath.Separator))))
		}
	}
	return paths
}

func containsBoatstackHook(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, hookCommandMarker)
	case []any:
		for _, item := range typed {
			if containsBoatstackHook(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if containsBoatstackHook(item) {
				return true
			}
		}
	}
	return false
}

func validateBoatstackHookEntry(host, event string, value any) error {
	entry, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s Boatstack hook for %s is not an object", host, event)
	}
	allowedOuter := map[string]bool{}
	if host == "cursor" {
		allowedOuter = map[string]bool{"command": true, "commandWindows": true, "failClosed": true, "timeout": true, "matcher": true}
		if stringValue(entry["command"]) == "" {
			return fmt.Errorf("cursor Boatstack hook for %s has no command", event)
		}
		if entry["failClosed"] != true {
			return fmt.Errorf("cursor Boatstack hook for %s must fail closed", event)
		}
	} else {
		allowedOuter = map[string]bool{"matcher": true, "hooks": true}
		if host == "gemini" {
			allowedOuter["sequential"] = true
		}
		if stringValue(entry["matcher"]) == "" {
			return fmt.Errorf("%s Boatstack hook for %s has no matcher", host, event)
		}
		handlers, ok := entry["hooks"].([]any)
		if !ok || len(handlers) != 1 {
			return fmt.Errorf("%s Boatstack hook for %s must contain exactly one command handler", host, event)
		}
		handler, ok := handlers[0].(map[string]any)
		if !ok || handler["type"] != "command" || stringValue(handler["command"]) == "" {
			return fmt.Errorf("%s Boatstack hook for %s has an invalid command handler", host, event)
		}
		allowedHandler := map[string]bool{"type": true, "command": true, "timeout": true, "statusMessage": true}
		if host == "claude" {
			allowedHandler["shell"] = true
			// Older installed Boatstack fragments relied on Claude's Bash default.
			// Accept that structurally during update preflight, while every newly
			// generated hook pins the documented shell explicitly.
			if handler["shell"] != nil && handler["shell"] != "bash" {
				return fmt.Errorf("claude Boatstack hook for %s must use the bash harness", event)
			}
		} else if host == "codex" {
			allowedHandler["commandWindows"] = true
		} else if host == "gemini" {
			allowedHandler["name"] = true
			allowedHandler["description"] = true
		}
		for key := range handler {
			if !allowedHandler[key] {
				return fmt.Errorf("%s Boatstack hook for %s contains unsupported handler field %s", host, event, key)
			}
		}
	}
	for key := range entry {
		if !allowedOuter[key] {
			return fmt.Errorf("%s Boatstack hook for %s contains unsupported field %s", host, event, key)
		}
	}
	return nil
}

func validateHostHookConfig(host string, config map[string]any) error {
	if host == "cursor" && config["version"] != float64(1) {
		return fmt.Errorf("Cursor hook config version must be 1")
	}
	hooks, ok := config["hooks"].(map[string]any)
	if !ok {
		return fmt.Errorf("host hook config has non-object hooks")
	}
	expected := map[string]bool{}
	for _, event := range hookEvents(host) {
		expected[event] = true
	}
	for event, entries := range hooks {
		if containsBoatstackHook(entries) && !expected[event] {
			return fmt.Errorf("%s Boatstack hook is attached to unsupported event %s", host, event)
		}
	}
	return nil
}

func loadHookConfig(path string) (map[string]any, error) {
	value, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	config := map[string]any{}
	if err := DecodeJSON("load host hook configuration", path, value, &config); err != nil {
		return nil, err
	}
	return config, nil
}

func mergeHostHook(config map[string]any, host string) error {
	return mergeHostHookWithOwnership(config, host, nil, false)
}

func mergeHostHookWithOwnership(config map[string]any, host string, installed map[string]any, repair bool) error {
	hooks, ok := config["hooks"].(map[string]any)
	if config["hooks"] == nil {
		hooks = map[string]any{}
		config["hooks"] = hooks
	} else if !ok {
		return fmt.Errorf("host hook config has non-object hooks")
	}
	desiredEvents := map[string]bool{}
	for _, event := range hookEvents(host) {
		desiredEvents[event] = true
	}
	// Retired events are an ordinary template migration when every Boatstack
	// entry exactly matches the committed fragment from the installed release.
	// The incoming helper may remove those entries before validating its own
	// event set; otherwise the updater would be blocked by the state it owns.
	for event, raw := range hooks {
		if desiredEvents[event] || !containsBoatstackHook(raw) {
			continue
		}
		entries, entriesOK := raw.([]any)
		if !entriesOK {
			return fmt.Errorf("host hook event %s is not a list", event)
		}
		kept := []any{}
		owned := 0
		verified := installed != nil && installed[event] != nil
		for _, entry := range entries {
			if !containsBoatstackHook(entry) {
				kept = append(kept, entry)
				continue
			}
			owned++
			verified = verified && sameJSON(entry, installed[event])
		}
		if owned > 0 && !verified && !repair {
			return fmt.Errorf("%s Boatstack hook is attached to unsupported event %s; rerun the update with --repair only after reviewing the owned-state preview", host, event)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	for _, event := range hookEvents(host) {
		entries := []any{}
		if existing := hooks[event]; existing != nil {
			var entriesOK bool
			entries, entriesOK = existing.([]any)
			if !entriesOK {
				return fmt.Errorf("host hook event %s is not a list", event)
			}
		}
		kept := []any{}
		found := 0
		verified := true
		for _, entry := range entries {
			if containsBoatstackHook(entry) {
				found++
				isDesired := sameJSON(entry, desiredHostHookForEvent(host, event))
				isInstalled := installed != nil && installed[event] != nil && sameJSON(entry, installed[event])
				if installed != nil && !isDesired && !isInstalled {
					verified = false
				}
				if installed == nil {
					if err := validateBoatstackHookEntry(host, event, entry); err != nil {
						return err
					}
				}
				continue
			}
			kept = append(kept, entry)
		}
		if found > 1 && (installed == nil || !verified) && !repair {
			return fmt.Errorf("ambiguous Boatstack hook collision in %s", event)
		}
		if found > 0 && !verified && !repair {
			return fmt.Errorf("drifted %s Boatstack engagement probe for %s; rerun the update with --repair only after reviewing the owned-state preview", host, event)
		}
		kept = append(kept, desiredHostHookForEvent(host, event))
		hooks[event] = kept
	}
	if host == "cursor" && config["version"] == nil {
		config["version"] = float64(1)
	}
	return validateHostHookConfig(host, config)
}

func InstallHostHooks(repo string, adapters []string) error {
	prepared, err := PrepareHostHooks(repo, adapters)
	if err != nil {
		return err
	}
	for _, path := range sortedKeys(prepared) {
		if err := atomicWrite(path, prepared[path]); err != nil {
			return err
		}
	}
	return nil
}

// PrepareHostHooks renders and validates every selected host document without
// writing, allowing initialization to fail before entering its commit phase.
func PrepareHostHooks(repo string, adapters []string) (map[string][]byte, error) {
	return prepareHostHooks(repo, adapters, false)
}

func PrepareHostHooksForUpdate(repo string, adapters []string, repair bool) (map[string][]byte, error) {
	return prepareHostHooks(repo, adapters, repair)
}

func prepareHostHooks(repo string, adapters []string, repair bool) (map[string][]byte, error) {
	prepared := map[string][]byte{}
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if !contains(adapters, host) {
			continue
		}
		path := hostHookConfigPath(repo, host)
		config, err := loadHookConfig(path)
		if err != nil {
			return nil, err
		}
		var installed map[string]any
		if repair || fileExists(filepath.Join(repo, ".product-loop", "hooks", host+".fragment.json")) {
			installed, err = loadInstalledHookEvents(repo, host)
			if err != nil {
				installed, err = loadCommittedInstalledHookEvents(repo, host)
				if err != nil {
					return nil, fmt.Errorf("prepare %s host hooks: cannot verify installed ownership: %w", host, err)
				}
			}
		}
		if err := mergeHostHookWithOwnership(config, host, installed, repair); err != nil {
			return nil, fmt.Errorf("prepare %s host hooks in %s: %w", host, path, err)
		}
		value, err := MarshalJSON(config)
		if err != nil {
			return nil, fmt.Errorf("serialize merged host hook configuration %s: %w", path, err)
		}
		if err := ValidateJSON("validate merged host hook configuration", path, value); err != nil {
			return nil, err
		}
		prepared[path] = value
	}
	return prepared, nil
}

func InstallHostHooksForUpdate(repo string, adapters []string, repair bool) error {
	prepared, err := PrepareHostHooksForUpdate(repo, adapters, repair)
	if err != nil {
		return err
	}
	for _, path := range sortedKeys(prepared) {
		if err := atomicWrite(path, prepared[path]); err != nil {
			return err
		}
	}
	return nil
}

func CheckHostHooks(repo string, adapters []string) error {
	return checkHostHooks(repo, adapters, func(host, event string) (any, error) {
		return desiredHostHookForEvent(host, event), nil
	})
}

// CheckInstalledHostHooks validates merged host settings against the committed
// fragment from the installed release. Update preflight must use this boundary:
// comparing an old, healthy hook with the incoming release template would
// misclassify an intentional template migration as user drift.
func CheckInstalledHostHooks(repo string, adapters []string) error {
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if !contains(adapters, host) {
			continue
		}
		installed, err := loadInstalledHookEvents(repo, host)
		if err != nil {
			installed, err = loadCommittedInstalledHookEvents(repo, host)
			if err != nil {
				return fmt.Errorf("cannot read installed %s hook fragment: %w", host, err)
			}
		}
		config, err := loadHookConfig(hostHookConfigPath(repo, host))
		if err != nil {
			return err
		}
		hooks, ok := config["hooks"].(map[string]any)
		if !ok {
			return fmt.Errorf("missing %s hooks", host)
		}
		for event, expected := range installed {
			entries, ok := hooks[event].([]any)
			if !ok {
				return fmt.Errorf("missing installed %s safety event %s", host, event)
			}
			matches := 0
			for _, entry := range entries {
				if containsBoatstackHook(entry) {
					matches++
					if !sameJSON(entry, expected) {
						return fmt.Errorf("drifted %s Boatstack safety hook", host)
					}
				}
			}
			if matches < 1 {
				return fmt.Errorf("expected an installed %s Boatstack safety hook for %s; found %d", host, event, matches)
			}
		}
		for event, raw := range hooks {
			if installed[event] == nil && containsBoatstackHook(raw) {
				return fmt.Errorf("drifted %s Boatstack safety hook on unowned event %s", host, event)
			}
		}
	}
	return nil
}

func checkHostHooks(repo string, adapters []string, expectedForEvent func(host, event string) (any, error)) error {
	for _, host := range []string{"cursor", "claude", "codex", "gemini"} {
		if !contains(adapters, host) {
			continue
		}
		path := hostHookConfigPath(repo, host)
		config, err := loadHookConfig(path)
		if err != nil {
			return err
		}
		if err := validateHostHookConfig(host, config); err != nil {
			return err
		}
		hooks, ok := config["hooks"].(map[string]any)
		if !ok {
			return fmt.Errorf("missing %s hooks in %s", host, path)
		}
		for _, event := range hookEvents(host) {
			expectedEntry, err := expectedForEvent(host, event)
			if err != nil {
				return err
			}
			if err := validateBoatstackHookEntry(host, event, expectedEntry); err != nil {
				return err
			}
			entries, ok := hooks[event].([]any)
			if !ok {
				return fmt.Errorf("missing %s safety event %s in %s", host, event, path)
			}
			matches := 0
			for _, entry := range entries {
				if containsBoatstackHook(entry) {
					matches++
					if err := validateBoatstackHookEntry(host, event, entry); err != nil {
						return err
					}
					current, _ := json.Marshal(entry)
					expected, _ := json.Marshal(expectedEntry)
					if string(current) != string(expected) {
						return fmt.Errorf("drifted %s Boatstack safety hook", host)
					}
				}
			}
			if matches != 1 {
				return fmt.Errorf("expected exactly one %s Boatstack safety hook for %s; found %d", host, event, matches)
			}
		}
	}
	return nil
}
