#!/usr/bin/env bash
set -euo pipefail

# Cross-compile and package pure-Go Tailfile release archives across desktop platforms.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${DIST_DIR:-$REPO_ROOT/dist}"

if [[ -z "${VERSION:-}" ]]; then
  VERSION="$(sed -n 's/^[[:space:]]*"version":[[:space:]]*"\([^"]*\)".*/\1/p' "$REPO_ROOT/mygo.json" | head -n 1)"
fi
VERSION="${VERSION#v}"
if [[ -z "$VERSION" ]]; then
  echo "error: unable to determine VERSION from env or mygo.json" >&2
  exit 1
fi

STAGE_ROOT="$(mktemp -d)"
trap 'rm -rf "$STAGE_ROOT"' EXIT

mkdir -p "$DIST"
DIST="$(cd "$DIST" && pwd)"
rm -f "$DIST"/tailfile_*.tar.gz "$DIST"/tailfile_*.zip "$DIST"/SHA256SUMS.txt

zip_dir() {
  local src_dir="$1"
  local out_zip="$2"
  if command -v zip >/dev/null 2>&1; then
    zip -q -j "$out_zip" "$src_dir"/*
  elif command -v python3 >/dev/null 2>&1; then
    python3 - "$src_dir" "$out_zip" <<'PY'
import os, sys, zipfile
src_dir, out_zip = sys.argv[1], sys.argv[2]
with zipfile.ZipFile(out_zip, "w", compression=zipfile.ZIP_DEFLATED) as zf:
    for name in sorted(os.listdir(src_dir)):
        zf.write(os.path.join(src_dir, name), arcname=name)
PY
  else
    echo "error: neither zip nor python3 is installed to package $out_zip" >&2
    exit 1
  fi
}

build_target() {
  local os="$1"
  local arch="$2"
  local bin="tailfile"
  local ldflags="-s -w -X tailfile.local/internal/agent.Version=${VERSION}"
  if [[ "$os" == "windows" ]]; then
    bin="tailfile.exe"
    ldflags="${ldflags} -H=windowsgui"
  fi

  local stage="$STAGE_ROOT/${os}-${arch}"
  mkdir -p "$stage"
  echo "Building ${os}/${arch} (v${VERSION})..."
  (
    cd "$REPO_ROOT"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
      go build -trimpath -tags mygo_nowebview -ldflags="$ldflags" -o "$stage/$bin" .
  )

  cp "$REPO_ROOT/LICENSE" "$stage/LICENSE"
  cp "$REPO_ROOT/README.md" "$stage/README.md"

  if [[ "$os" == "windows" ]]; then
    local out="$DIST/tailfile_${VERSION}_windows_${arch}.zip"
    zip_dir "$stage" "$out"
  else
    local out="$DIST/tailfile_${VERSION}_${os}_${arch}.tar.gz"
    tar -C "$stage" -czf "$out" "$bin" LICENSE README.md
  fi
}

build_target linux amd64
build_target linux arm64
build_target darwin amd64
build_target darwin arm64
build_target windows amd64
build_target windows arm64

cp "$REPO_ROOT/scripts/install.sh" "$DIST/install.sh"
chmod 0755 "$DIST/install.sh"
cp "$REPO_ROOT/scripts/install.ps1" "$DIST/install.ps1"

(
  cd "$DIST"
  if command -v sha256sum >/dev/null 2>&1; then
    LC_ALL=C sha256sum tailfile_*.tar.gz tailfile_*.zip > SHA256SUMS.txt
  elif command -v shasum >/dev/null 2>&1; then
    LC_ALL=C shasum -a 256 tailfile_*.tar.gz tailfile_*.zip > SHA256SUMS.txt
  else
    echo "error: sha256sum or shasum is required to generate SHA256SUMS.txt" >&2
    exit 1
  fi
)

echo "Release artifacts written to $DIST"
