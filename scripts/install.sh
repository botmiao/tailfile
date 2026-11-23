#!/bin/sh
set -eu

# Install Tailfile for Linux or macOS from GitHub Releases with SHA-256 verification.
# Usage:
#   curl -fsSL https://github.com/botmiao/tailfile/releases/latest/download/install.sh | sh
#   curl -fsSL https://github.com/botmiao/tailfile/releases/latest/download/install.sh | sh -s -- --service

REPO="${TAILFILE_REPO:-botmiao/tailfile}"
REQ_VERSION="${TAILFILE_VERSION:-latest}"
PREFIX="${TAILFILE_PREFIX:-$HOME/.local/bin}"
DOWNLOAD_BASE="${TAILFILE_DOWNLOAD_BASE:-}"
INSTALL_SERVICE="${TAILFILE_SERVICE:-0}"

usage() {
  cat <<'EOF'
Install Tailfile for Linux or macOS.

Options:
  -v, --version <ver>   Install a specific version (e.g. 0.2.0 or v0.2.0; default: latest)
  -p, --prefix <dir>    Install directory (default: $HOME/.local/bin)
      --service         Also register per-user background daemon (tailfile service install)
  -h, --help            Show this help
  -- <flags...>         Extra flags forwarded to `tailfile service install`
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -v|--version)
      [ "$#" -ge 2 ] || { echo "error: --version requires a value" >&2; exit 2; }
      REQ_VERSION="$2"
      shift 2
      ;;
    -p|--prefix)
      [ "$#" -ge 2 ] || { echo "error: --prefix requires a value" >&2; exit 2; }
      PREFIX="$2"
      shift 2
      ;;
    --service)
      INSTALL_SERVICE=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    *)
      echo "error: unknown option: $1 (pass service flags after --)" >&2
      exit 2
      ;;
  esac
done

case "$(uname -s)" in
  Linux)  OS="linux" ;;
  Darwin) OS="darwin" ;;
  *)
    echo "error: unsupported OS $(uname -s) (use install.ps1 on Windows)" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "error: unsupported CPU architecture $(uname -m)" >&2
    exit 1
    ;;
esac

# Prefer native Apple Silicon builds even if invoked from a Rosetta x86_64 shell.
if [ "$OS" = "darwin" ] && [ "$ARCH" = "amd64" ]; then
  if [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = "1" ]; then
    ARCH="arm64"
  fi
fi

http_get() {
  url="$1"
  out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
  else
    echo "error: curl or wget is required to download Tailfile" >&2
    exit 1
  fi
}

resolve_latest_tag() {
  latest_url="https://github.com/${REPO}/releases/latest"
  if command -v curl >/dev/null 2>&1; then
    eff="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$latest_url" 2>/dev/null || true)"
    case "$eff" in
      */tag/*)
        printf '%s\n' "${eff##*/tag/}"
        return 0
        ;;
    esac
  fi

  api_tmp="$TMPDIR_WORK/release.json"
  http_get "https://api.github.com/repos/${REPO}/releases/latest" "$api_tmp"
  tag="$(sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$api_tmp" | head -n 1)"
  if [ -z "$tag" ]; then
    echo "error: failed to determine latest Tailfile release tag from $REPO" >&2
    exit 1
  fi
  printf '%s\n' "$tag"
}

sha256_file() {
  file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print tolower($1)}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print tolower($1)}'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$file" | awk '{print tolower($NF)}'
  else
    echo "error: sha256sum, shasum, or openssl is required to verify download checksum" >&2
    exit 1
  fi
}

TMPDIR_WORK="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_WORK"' EXIT INT TERM

if [ "$REQ_VERSION" = "latest" ]; then
  if [ -n "$DOWNLOAD_BASE" ]; then
    echo "error: TAILFILE_VERSION must be specified when TAILFILE_DOWNLOAD_BASE is set" >&2
    exit 1
  fi
  TAG="$(resolve_latest_tag)"
else
  VER_CLEAN="${REQ_VERSION#v}"
  TAG="v${VER_CLEAN}"
fi
VERSION="${TAG#v}"

if [ -n "$DOWNLOAD_BASE" ]; then
  BASE_URL="${DOWNLOAD_BASE%/}"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
fi

ARCHIVE="tailfile_${VERSION}_${OS}_${ARCH}.tar.gz"
ARCHIVE_PATH="$TMPDIR_WORK/$ARCHIVE"
SUMS_PATH="$TMPDIR_WORK/SHA256SUMS.txt"

echo "Downloading Tailfile ${TAG} (${OS}/${ARCH})..."
http_get "${BASE_URL}/${ARCHIVE}" "$ARCHIVE_PATH"
http_get "${BASE_URL}/SHA256SUMS.txt" "$SUMS_PATH"

EXPECTED_HASH="$(awk -v f="$ARCHIVE" '$2 == f || $2 == ("*" f) { print tolower($1); exit }' "$SUMS_PATH")"
if [ -z "$EXPECTED_HASH" ] || [ "${#EXPECTED_HASH}" -ne 64 ]; then
  echo "error: checksum entry for $ARCHIVE not found in SHA256SUMS.txt" >&2
  exit 1
fi

ACTUAL_HASH="$(sha256_file "$ARCHIVE_PATH")"
if [ "$ACTUAL_HASH" != "$EXPECTED_HASH" ]; then
  echo "error: SHA-256 mismatch for $ARCHIVE (expected $EXPECTED_HASH, got $ACTUAL_HASH)" >&2
  exit 1
fi
echo "Verified SHA-256: $ACTUAL_HASH"

UNPACK_DIR="$TMPDIR_WORK/unpack"
mkdir -p "$UNPACK_DIR"
tar -xzf "$ARCHIVE_PATH" -C "$UNPACK_DIR"
[ -f "$UNPACK_DIR/tailfile" ] || { echo "error: archive did not contain tailfile binary" >&2; exit 1; }

mkdir -p "$PREFIX"
TMP_BIN="$PREFIX/.tailfile.install.$$"
cp "$UNPACK_DIR/tailfile" "$TMP_BIN"
chmod 0755 "$TMP_BIN"
mv -f "$TMP_BIN" "$PREFIX/tailfile"

if [ "$OS" = "darwin" ] && command -v xattr >/dev/null 2>&1; then
  xattr -d com.apple.quarantine "$PREFIX/tailfile" >/dev/null 2>&1 || true
fi

echo "Installed: $PREFIX/tailfile"
"$PREFIX/tailfile" --version

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *)
    echo
    echo "Add $PREFIX to your PATH if not already configured:"
    echo "  export PATH=\"$PREFIX:\$PATH\""
    ;;
esac

if [ "$INSTALL_SERVICE" = "1" ]; then
  echo
  echo "Installing per-user background service..."
  "$PREFIX/tailfile" service install "$@"
  "$PREFIX/tailfile" service status || true
fi
