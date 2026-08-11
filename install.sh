#!/usr/bin/env bash
set -euo pipefail

# Boatstack V2 bootstrap trust boundary. This script installs a checksum-bound
# runtime transport. Every repository mutation is then performed by the
# installation.initialize or installation.update kernel transition.

repository="${BOATSTACK_REPO:-$PWD}"
version="${BOATSTACK_VERSION:-latest}"
mode="${BOATSTACK_MODE:-install}"
actor="${BOATSTACK_ACTOR:-${USER:-operator}}"
install_dir="${BOATSTACK_INSTALL_DIR:-${HOME}/.local/bin}"
config_source="${BOATSTACK_CONFIG:-}"

case "$mode" in
  install|update) ;;
  *) echo "Boatstack V2 supports BOATSTACK_MODE=install or update" >&2; exit 2 ;;
esac

repository="$(git -C "$repository" rev-parse --show-toplevel)"
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
  curl --fail --silent --show-error --location "$base/$asset" --output "$candidate"
  curl --fail --silent --show-error --location "$base/$asset.sha256" --output "$temporary/$asset.sha256"
  expected="$(awk '{print $1}' "$temporary/$asset.sha256")"
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$candidate" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$candidate" | awk '{print $1}')"
fi
[[ "$actual" == "$expected" ]] || { echo "Boatstack runtime checksum mismatch" >&2; exit 1; }
chmod 0755 "$candidate"

mkdir -p "$install_dir"
safe_version="${version//[^a-zA-Z0-9._-]/-}"
runtime="$install_dir/boatstack-v2-${safe_version}-${actual:0:16}"
install -m 0755 "$candidate" "$runtime"
runtime="$(cd -P -- "$(dirname "$runtime")" && pwd)/$(basename "$runtime")"

if [[ "$mode" == install ]]; then
  if [[ -z "$config_source" ]]; then
    config_source="$temporary/project.json"
    json_default_branch="${default_branch//\\/\\\\}"
    json_default_branch="${json_default_branch//\"/\\\"}"
    printf '%s\n' "{\"schema_version\":2,\"project\":{\"name\":\"repository\",\"default_branch\":\"$json_default_branch\",\"commands\":{}},\"policy\":{\"plan_approval\":\"human\",\"visual_evidence\":\"optional\"},\"hosts\":[\"cli\",\"cursor\",\"codex\",\"claude\",\"gemini\",\"mcp\"]}" > "$config_source"
  fi
  "$runtime" init --repo "$repository" --human "$actor" --param "config_path=$config_source" --format text
else
  update_arguments=(
    update --repo "$repository" --human "$actor"
    --param "runtime_path=$runtime"
    --param "runtime_sha256=$actual"
    --format json
  )
  if [[ "${BOATSTACK_ACCEPT_PROGRAM_CHANGE:-false}" == "true" ]]; then
    update_arguments+=(--accept-program-change)
  fi
  "$runtime" "${update_arguments[@]}"
fi

echo "Boatstack V2 installed at $runtime"
echo "Review and commit $repository/.boatstack/project.json and the generated host skills"
echo "Run: $install_dir/boatstack doctor --repo $repository --format text"
