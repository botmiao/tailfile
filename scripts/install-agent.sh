#!/usr/bin/env bash
set -euo pipefail

# Build and install the unified tailfile binary for the current user, then
# register the per-user background daemon (`tailfile service install`).

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREFIX="${TAILFILE_PREFIX:-$HOME/.local/bin}"
INSTALL_SERVICE="${TAILFILE_INSTALL_SERVICE:-1}"

mkdir -p "$PREFIX"

echo "Building unified tailfile binary (CGO_ENABLED=0)..."
(
  cd "$REPO_ROOT"
  CGO_ENABLED=0 go build -tags mygo_nowebview -ldflags="-s -w" -o "$PREFIX/tailfile" .
)

echo "Installed: $PREFIX/tailfile"

if [[ "$INSTALL_SERVICE" == "1" ]]; then
  echo "Installing per-user background service..."
  "$PREFIX/tailfile" service install "$@"
  echo
  echo "Done. You can check status anytime with:"
  echo "  $PREFIX/tailfile service status"
fi
