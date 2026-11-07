#!/usr/bin/env bash
# Compile and test the Tailfile agent and Native Shell GUI (gui-nsh).
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
if [[ -x "$HOME/sdk/go1.27.3/bin/go" ]]; then
  export PATH="$HOME/sdk/go1.27.3/bin:$PATH"
fi
export GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
export GOWORK=off
OUT="${OUT:-$ROOT/build-out}"
mkdir -p "$OUT"
fail=0

note() { printf '\n== %s\n' "$*"; }

note "agent tests"
( cd "$ROOT/agent" && go test -count=1 -timeout 180s . ) || fail=1

note "agent binaries"
( cd "$ROOT/agent" && CGO_ENABLED=0 go build -o "$OUT/tailfile-agent-linux" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$OUT/tailfile-agent-linux-arm64" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o "$OUT/tailfile-agent-windows.exe" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -o "$OUT/tailfile-agent-windows-arm64.exe" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o "$OUT/tailfile-agent-darwin-amd64" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$OUT/tailfile-agent-darwin-arm64" ./cmd/tailfile-agent ) || fail=1

note "gui-nsh tests and binary"
if command -v cargo >/dev/null; then
  ( cd "$ROOT/gui-nsh" && cargo test --lib --test demo_agent ) || fail=1
  if pkg-config --exists gtk4; then
    ( cd "$ROOT/gui-nsh" && cargo build --release && cp target/release/tailfile-nsh "$OUT/tailfile-nsh-linux" ) || fail=1
  fi
fi

note "sizes"
wc -c "$OUT"/tailfile-* | sort
echo
if [[ "$fail" -ne 0 ]]; then
  echo "build-all: a required step failed"
  exit 1
fi
echo "build-all: required steps passed"
