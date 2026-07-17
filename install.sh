#!/usr/bin/env bash
# Generated from operatorstack/intelligence-flow.
set -euo pipefail

repository="operatorstack/boatstack"
version="${BOATSTACK_VERSION:-latest}"
target_repo="${BOATSTACK_REPO:-$PWD}"
mode="${BOATSTACK_MODE:-install}"

case "$mode" in
  install|update) ;;
  *) echo "BLOCKED: BOATSTACK_MODE must be install or update" >&2; exit 1 ;;
esac

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
if [ "$mode" = "update" ]; then
  current_helper="$target_repo/.product-loop/bin/boatstack-helper${extension}"
  [ -x "$current_helper" ] || { echo "BLOCKED: current Boatstack helper is missing; repair the installation before updating" >&2; exit 1; }
  "$current_helper" doctor --repo "$target_repo"
fi
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

command_name="init"
[ "$mode" = "update" ] && command_name="update"
arguments=("$command_name" --repo "$target_repo" --binary "$binary")
if [ "$mode" = "install" ] && [ -n "${BOATSTACK_INTEGRATIONS:-}" ]; then
  arguments+=(--integrations "$BOATSTACK_INTEGRATIONS")
fi
if [ "${BOATSTACK_YES:-0}" = "1" ]; then
  arguments+=(--yes)
fi

exec "$binary" "${arguments[@]}"
