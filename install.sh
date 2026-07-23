#!/usr/bin/env bash
# Generated from operatorstack/intelligence-flow.
set -euo pipefail

repository="operatorstack/boatstack"
version="${BOATSTACK_VERSION:-latest}"
target_repo="${BOATSTACK_REPO:-$PWD}"
mode="${BOATSTACK_MODE:-install}"
repair="${BOATSTACK_REPAIR:-0}"
allow_downgrade="${BOATSTACK_ALLOW_DOWNGRADE:-0}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repair) repair=1 ;;
    --allow-downgrade) allow_downgrade=1 ;;
    *) echo "BLOCKED: unsupported installer argument: $1" >&2; exit 1 ;;
  esac
  shift
done

case "$mode" in
  install|update) ;;
  *) echo "BLOCKED: BOATSTACK_MODE must be install or update" >&2; exit 1 ;;
esac

if [ "$mode" = "install" ] && { [ -f "$target_repo/.product-loop/generated.lock.json" ] || [ -f "$target_repo/.product-loop/bin/boatstack-helper" ] || [ -f "$target_repo/.product-loop/bin/boatstack-helper.exe" ]; }; then
  if [ "$repair" = "1" ]; then
    mode="update"
    echo "Existing Boatstack installation detected; preserving its configuration and using update repair semantics."
  else
    echo "BLOCKED: Boatstack is already installed; use BOATSTACK_MODE=update, or add --repair when owned control state prevents updating" >&2
    exit 1
  fi
fi

case "$(uname -s)" in
  Darwin) os_name="darwin" ;;
  Linux) os_name="linux" ;;
  MINGW*|MSYS*|CYGWIN*) os_name="windows" ;;
  *) echo "BLOCKED: unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "BLOCKED: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

command -v curl >/dev/null 2>&1 || { echo "BLOCKED: curl is required to download Boatstack" >&2; exit 1; }
command -v git >/dev/null 2>&1 || { echo "BLOCKED: Git is required because Boatstack operates on reviewable repository state" >&2; exit 1; }

extension=""
[ "$os_name" = "windows" ] && extension=".exe"
asset="boatstack-helper_${os_name}_${arch}${extension}"
if [ "$version" = "latest" ]; then
  base="https://github.com/${repository}/releases/latest/download"
else
  base="https://github.com/${repository}/releases/download/${version}"
fi

temporary="$(mktemp -d 2>/dev/null || mktemp -d -t boatstack)"
trap 'rm -rf "$temporary"' EXIT
binary="$temporary/$asset"
checksum="$temporary/$asset.sha256"

echo "Downloading verified Boatstack helper for ${os_name}/${arch}..."
curl -fsSL "$base/$asset" -o "$binary"
curl -fsSL "$base/$asset.sha256" -o "$checksum"
expected="$(awk '{print $1}' "$checksum")"
if command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$binary" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$binary" | awk '{print $1}')"
else
  echo "BLOCKED: shasum or sha256sum is required to verify the Boatstack binary" >&2
  exit 1
fi
[ "$expected" = "$actual" ] || { echo "BLOCKED: Boatstack binary checksum mismatch" >&2; exit 1; }
chmod +x "$binary"

if [ "$mode" = "update" ]; then
  current_helper="$target_repo/.product-loop/bin/boatstack-helper${extension}"
  if [ -x "$current_helper" ]; then
    if ! "$current_helper" doctor --repo "$target_repo"; then
      echo "Current Boatstack doctor reported drift; the verified target helper will classify whether it is safely repairable." >&2
    fi
  else
    echo "Current Boatstack helper is missing; the verified target helper will classify whether it is safely repairable." >&2
  fi
fi

command_name="init"
[ "$mode" = "update" ] && command_name="update"
arguments=("$command_name" --repo "$target_repo" --binary "$binary")
if [ "$mode" = "install" ] && [ -n "${BOATSTACK_INTEGRATIONS:-}" ]; then
  arguments+=(--integrations "$BOATSTACK_INTEGRATIONS")
fi
if [ "${BOATSTACK_YES:-0}" = "1" ]; then
  arguments+=(--yes)
fi
if [ "$repair" = "1" ]; then
  arguments+=(--repair)
fi
if [ "$allow_downgrade" = "1" ]; then
  arguments+=(--allow-downgrade)
fi

exec "$binary" "${arguments[@]}"
