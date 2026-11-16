#!/usr/bin/env bash
# Install (or upgrade) tailfile-agent as a per-user service, locally or on
# another tailnet machine over Tailscale SSH. No root, no sudo.
#
#   scripts/install-agent.sh                         # this machine
#   scripts/install-agent.sh --ssh iori@iori-claw    # a Linux/macOS peer
#
# The agent binary is cross-compiled from this checkout (needs Go) unless
# --binary points at a prebuilt one for the target's OS/arch.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/install-agent.sh [options]

  --ssh TARGET     install on TARGET (user@host) via `tailscale ssh`.
                   Linux and macOS targets only. Without it, installs here.
  --binary PATH    use this agent binary instead of building one.
  --net MODE       auto | system | tsnet          (default: system)
  --read LIST      comma-separated readable roots (default: $HOME/Tailfile on the target)
  --write LIST     comma-separated writable roots (default: same as --read)
  --read-only      no writable roots
  --dest DIR       install directory on the target (default: $HOME/.local/bin)
  -h, --help       show this help

An existing service is stopped, its binary replaced, and started again; its
flags are kept. To change flags, run `tailfile-agent service uninstall` first.
EOF
}

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SSH_TARGET=""
BINARY=""
NET="system"
READ=""
WRITE=""
WRITE_SET=0
DEST=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ssh) SSH_TARGET="${2:?--ssh needs user@host}"; shift 2 ;;
    --binary) BINARY="${2:?--binary needs a path}"; shift 2 ;;
    --net) NET="${2:?--net needs a value}"; shift 2 ;;
    --read) READ="${2:?--read needs a value}"; shift 2 ;;
    --write) WRITE="${2:?--write needs a value}"; WRITE_SET=1; shift 2 ;;
    --read-only) WRITE=""; WRITE_SET=1; shift ;;
    --dest) DEST="${2:?--dest needs a directory}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

case "$NET" in auto|system|tsnet) ;; *) echo "--net must be auto, system, or tsnet" >&2; exit 2 ;; esac

# run CMD on the target. Remote commands go through the target's login shell,
# so $HOME and ~ expand there, not here.
run() {
  if [[ -n "$SSH_TARGET" ]]; then
    tailscale ssh "$SSH_TARGET" "$1" </dev/null
  else
    bash -c "$1"
  fi
}

# Single-quote a value for the target shell.
q() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }

echo "== target"
uname_sm="$(run 'uname -sm')"
read -r os arch <<<"$uname_sm"
case "$os" in
  Linux) goos=linux ;;
  Darwin) goos=darwin ;;
  *) echo "unsupported target OS: $os (Windows: see README, service install)" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) goarch=amd64 ;;
  aarch64|arm64) goarch=arm64 ;;
  *) echo "unsupported target arch: $arch" >&2; exit 1 ;;
esac
echo "${SSH_TARGET:-local}: $goos/$goarch"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [[ -z "$BINARY" ]]; then
  echo "== build"
  if [[ -x "$HOME/sdk/go1.27.3/bin/go" ]]; then
    export PATH="$HOME/sdk/go1.27.3/bin:$PATH"
  fi
  command -v go >/dev/null || { echo "go not found; install Go or pass --binary" >&2; exit 1; }
  BINARY="$tmp/tailfile-agent"
  ( cd "$ROOT/agent" && GOWORK=off CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -o "$BINARY" ./cmd/tailfile-agent )
fi
[[ -f "$BINARY" ]] || { echo "no binary at $BINARY" >&2; exit 1; }

if command -v sha256sum >/dev/null; then
  want="$(sha256sum "$BINARY" | cut -d' ' -f1)"
else
  want="$(shasum -a 256 "$BINARY" | cut -d' ' -f1)"
fi

dest_expr="${DEST:+$(q "$DEST")}"
dest_expr="${dest_expr:-\"\$HOME/.local/bin\"}"
exe_expr="$dest_expr/tailfile-agent"

echo "== upload"
run "mkdir -p $dest_expr"
upload="cat > $exe_expr.new && chmod 755 $exe_expr.new && (sha256sum $exe_expr.new 2>/dev/null || shasum -a 256 $exe_expr.new) | cut -d' ' -f1"
if [[ -n "$SSH_TARGET" ]]; then
  got="$(tailscale ssh "$SSH_TARGET" "$upload" <"$BINARY")"
else
  got="$(bash -c "$upload" <"$BINARY")"
fi
if [[ "$got" != "$want" ]]; then
  echo "checksum mismatch after upload: want $want, got $got" >&2
  run "rm -f $exe_expr.new" || true
  exit 1
fi
echo "sha256 $want"

echo "== install"
# status exits non-zero when nothing is installed.
if run "test -x $exe_expr && $exe_expr service status >/dev/null 2>&1"; then
  echo "service already installed: upgrading the binary, keeping its flags"
  run "$exe_expr service stop" || true
  run "mv -f $exe_expr.new $exe_expr"
  run "$exe_expr service start"
else
  read_expr="${READ:+$(q "$READ")}"
  read_expr="${read_expr:-\"\$HOME/Tailfile\"}"
  if [[ "$WRITE_SET" -eq 1 ]]; then
    write_expr="${WRITE:+$(q "$WRITE")}"
  else
    write_expr="$read_expr"
  fi
  flags="--net $(q "$NET") --read $read_expr"
  if [[ -n "$write_expr" ]]; then
    flags="$flags --write $write_expr"
  fi
  run "mv -f $exe_expr.new $exe_expr && mkdir -p \"\$HOME/Tailfile\" && $exe_expr service install $flags"
fi

echo "== status"
sleep 2
run "$exe_expr service status"

if [[ "$goos" == linux ]]; then
  linger="$(run 'loginctl show-user "$USER" --property=Linger --value 2>/dev/null || true')"
  if [[ "$linger" != "yes" ]]; then
    echo "note: user lingering is off, so the agent stops when ${SSH_TARGET:-you} log out."
    echo "      To keep it running: loginctl enable-linger \"\$USER\" (may need an administrator)."
  fi
fi
echo "done. Uninstall with: ${DEST:-~/.local/bin}/tailfile-agent service uninstall"
