#!/usr/bin/env bash
set -euo pipefail

# Boatstack bootstrap trust boundary. This script installs a checksum-bound
# runtime transport. Every repository mutation is then performed by the
# installation.initialize or installation.update kernel transition.

repository="${BOATSTACK_REPO:-$PWD}"
version="${BOATSTACK_VERSION:-latest}"
mode="${BOATSTACK_MODE:-install}"
actor="${BOATSTACK_ACTOR:-}"
install_dir="${BOATSTACK_INSTALL_DIR:-${HOME}/.local/bin}"
boatstack_home="${BOATSTACK_HOME:-${XDG_DATA_HOME:-${HOME}/.local/share}/boatstack}"
config_source="${BOATSTACK_CONFIG:-}"

case "$mode" in
  install|update|hydrate) ;;
  *) echo "Boatstack supports BOATSTACK_MODE=install, update, or hydrate" >&2; exit 2 ;;
esac

if [[ "$mode" != hydrate && -z "$actor" ]]; then
  echo "BOATSTACK_HUMAN_ACTOR_REQUIRED: install and update require an explicit BOATSTACK_ACTOR" >&2
  exit 2
fi

if [[ "$mode" == hydrate ]]; then
  [[ "$version" != latest ]] || {
    echo "BOATSTACK_RUNTIME_PIN_INVALID: hydrate requires an exact BOATSTACK_VERSION" >&2
    exit 2
  }
  [[ "${BOATSTACK_EXPECTED_RUNTIME_SHA256:-}" =~ ^[0-9a-fA-F]{64}$ ]] || {
    echo "BOATSTACK_RUNTIME_PIN_INVALID: hydrate requires an exact BOATSTACK_EXPECTED_RUNTIME_SHA256" >&2
    exit 2
  }
fi

repository="$(git -C "$repository" rev-parse --show-toplevel)"
default_branch=""
if [[ "$mode" != hydrate ]]; then
  current_branch="$(git -C "$repository" symbolic-ref --quiet --short HEAD)" || {
    echo "Boatstack installation requires an attached branch" >&2
    exit 2
  }
  remote_default="$(git -C "$repository" symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || true)"
  default_branch="${remote_default#origin/}"
  if [[ -z "$default_branch" ]]; then
    default_branch="$current_branch"
  fi
  git check-ref-format --branch "$default_branch" >/dev/null || {
    echo "Boatstack could not resolve a valid default branch" >&2
    exit 2
  }
fi
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "unsupported operating system" >&2; exit 2 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported architecture" >&2; exit 2 ;;
esac

temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
asset="boatstack-helper_${os}_${arch}"
candidate="$temporary/$asset"

if [[ -n "${BOATSTACK_BINARY:-}" ]]; then
  [[ -n "${BOATSTACK_BINARY_SHA256:-}" ]] || { echo "BOATSTACK_BINARY_SHA256 is required with BOATSTACK_BINARY" >&2; exit 2; }
  cp "$BOATSTACK_BINARY" "$candidate"
  expected="$BOATSTACK_BINARY_SHA256"
else
  if [[ "$version" == latest ]]; then
    base="https://github.com/operatorstack/boatstack/releases/latest/download"
  else
    base="https://github.com/operatorstack/boatstack/releases/download/$version"
  fi
  if ! curl --fail --silent --show-error --location "$base/$asset" --output "$candidate"; then
    echo "BOATSTACK_RUNTIME_ARTIFACT_UNAVAILABLE: version=$version asset=$asset" >&2
    exit 1
  fi
  if ! curl --fail --silent --show-error --location "$base/$asset.sha256" --output "$temporary/$asset.sha256"; then
    echo "BOATSTACK_RUNTIME_ARTIFACT_UNAVAILABLE: version=$version asset=$asset.sha256" >&2
    exit 1
  fi
  expected="$(awk '{print $1}' "$temporary/$asset.sha256")"
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$candidate" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$candidate" | awk '{print $1}')"
fi
[[ "$actual" == "$expected" ]] || {
  echo "BOATSTACK_RUNTIME_ARTIFACT_CHECKSUM_MISMATCH: version=$version asset=$asset expected=$expected actual=$actual" >&2
  exit 1
}
if [[ -n "${BOATSTACK_EXPECTED_RUNTIME_SHA256:-}" && "$actual" != "$BOATSTACK_EXPECTED_RUNTIME_SHA256" ]]; then
  echo "BOATSTACK_RUNTIME_ARTIFACT_CHECKSUM_MISMATCH: version=$version asset=$asset expected=$BOATSTACK_EXPECTED_RUNTIME_SHA256 actual=$actual" >&2
  exit 1
fi
chmod 0755 "$candidate"

candidate_version="$("$candidate" version)"
safe_version="${candidate_version//[^a-zA-Z0-9._-]/-}"
[[ -n "$safe_version" && "$safe_version" == "$candidate_version" ]] || { echo "Boatstack runtime reported an invalid version identity" >&2; exit 1; }
if [[ "$mode" == hydrate && "$version" != latest && "$candidate_version" != "$version" ]]; then
  echo "Boatstack runtime version mismatch: requested=$version actual=$candidate_version" >&2
  exit 1
fi
runtime_dir="$boatstack_home/runtimes/${safe_version}-${actual}"
runtime="$runtime_dir/boatstack-runtime"
mkdir -p "$runtime_dir"
if [[ -e "$runtime" ]]; then
  if command -v sha256sum >/dev/null 2>&1; then
    installed="$(sha256sum "$runtime" | awk '{print $1}')"
  else
    installed="$(shasum -a 256 "$runtime" | awk '{print $1}')"
  fi
  [[ "$installed" == "$actual" ]] || { echo "Boatstack immutable runtime store collision" >&2; exit 1; }
else
  staged="$runtime_dir/.boatstack-runtime.$$"
  install -m 0755 "$candidate" "$staged"
  if ! ln "$staged" "$runtime" 2>/dev/null; then
    rm -f "$staged"
    [[ -f "$runtime" ]] || { echo "Boatstack runtime installation raced without a durable artifact" >&2; exit 1; }
    if command -v sha256sum >/dev/null 2>&1; then
      installed="$(sha256sum "$runtime" | awk '{print $1}')"
    else
      installed="$(shasum -a 256 "$runtime" | awk '{print $1}')"
    fi
    [[ "$installed" == "$actual" ]] || { echo "Boatstack immutable runtime store collision" >&2; exit 1; }
  else
    rm -f "$staged"
  fi
fi
runtime="$(cd -P -- "$(dirname "$runtime")" && pwd)/$(basename "$runtime")"

if [[ "$mode" == install ]]; then
  if [[ -z "$config_source" ]]; then
    config_source="$temporary/project.json"
    json_default_branch="${default_branch//\\/\\\\}"
    json_default_branch="${json_default_branch//\"/\\\"}"
    printf '%s\n' "{\"schema_version\":5,\"identity\":{\"default\":\"developer\",\"roles\":{\"developer\":{\"kind\":\"literal\",\"value\":\"$actor\"}}},\"project\":{\"name\":\"repository\",\"default_branch\":\"$json_default_branch\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\",\"cursor\",\"codex\",\"claude\",\"gemini\",\"mcp\"],\"projections\":[\"codex\",\"claude\",\"cursor\",\"gemini\"]}" > "$config_source"
  fi
  "$runtime" init --repo "$repository" --human "$actor" --param "config_path=$config_source" --format text
elif [[ "$mode" == update ]]; then
  update_arguments=(
    update --repo "$repository" --human "$actor"
    --param "runtime_sha256=$actual"
    --format json
  )
  if [[ "${BOATSTACK_ACCEPT_PROGRAM_CHANGE:-false}" == "true" ]]; then
    update_arguments+=(--accept-program-change)
  fi
  "$runtime" "${update_arguments[@]}"
fi

mkdir -p "$install_dir"
if [[ "$mode" != hydrate || ! -e "$install_dir/boatstack" ]]; then
  launcher_staged="$install_dir/.boatstack.$$"
  install -m 0755 "$candidate" "$launcher_staged"
  mv -f "$launcher_staged" "$install_dir/boatstack"
fi

echo "Boatstack installed at $runtime"
if [[ "$mode" != hydrate ]]; then
  echo "Review and commit $repository/.boatstack/project.json, $repository/.boatstack/runtime.json, and the generated host projections"
fi
echo "Run: $install_dir/boatstack doctor --repo $repository --format text"
