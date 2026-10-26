#!/usr/bin/env bash
# Compile the agent and both GUIs, and run the agent and MyGo tests.
# Prints sizes for what actually linked. Does not claim a platform was run.
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
keep() {
  local name="$1" src="$2"
  if [[ -f "$src" ]]; then
    cp "$src" "$OUT/$name"
    printf '%-40s %s bytes\n' "$name" "$(wc -c < "$OUT/$name")"
  fi
}

note "agent tests"
( cd "$ROOT/agent" && go test -count=1 -timeout 180s . ) || fail=1

note "agent binaries"
( cd "$ROOT/agent" && CGO_ENABLED=0 go build -o "$OUT/tailfile-agent-linux" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$OUT/tailfile-agent-linux-arm64" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o "$OUT/tailfile-agent-windows.exe" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -o "$OUT/tailfile-agent-windows-arm64.exe" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o "$OUT/tailfile-agent-darwin-amd64" ./cmd/tailfile-agent ) || fail=1
( cd "$ROOT/agent" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$OUT/tailfile-agent-darwin-arm64" ./cmd/tailfile-agent ) || fail=1

note "mygo tests and binaries"
( cd "$ROOT/gui-mygo" && go test -count=1 -timeout 180s . ) || fail=1
( cd "$ROOT/gui-mygo" && CGO_ENABLED=0 go build -o "$OUT/tailfile-mygo-linux" . ) || fail=1
( cd "$ROOT/gui-mygo" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$OUT/tailfile-mygo-linux-arm64" . ) || fail=1
( cd "$ROOT/gui-mygo" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o "$OUT/tailfile-mygo-windows.exe" . ) || fail=1
( cd "$ROOT/gui-mygo" && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -o "$OUT/tailfile-mygo-windows-arm64.exe" . ) || fail=1
( cd "$ROOT/gui-mygo" && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o "$OUT/tailfile-mygo-darwin-amd64" . ) || fail=1
( cd "$ROOT/gui-mygo" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$OUT/tailfile-mygo-darwin-arm64" . ) || fail=1

note "wails frontend"
if command -v npm >/dev/null; then
  ( cd "$ROOT/gui-wails/frontend" && npm ci && npm run build ) || fail=1
else
  echo "npm missing; using the committed frontend/dist"
fi

note "wails server binaries (pure Go, -tags server)"
( cd "$ROOT/gui-wails" && CGO_ENABLED=0 go build -tags server -o "$OUT/tailfile-wails-server-linux" . ) || fail=1
( cd "$ROOT/gui-wails" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags server -o "$OUT/tailfile-wails-server-linux-arm64" . ) || fail=1
( cd "$ROOT/gui-wails" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -tags server -o "$OUT/tailfile-wails-server-windows.exe" . ) || fail=1
( cd "$ROOT/gui-wails" && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -tags server -o "$OUT/tailfile-wails-server-windows-arm64.exe" . ) || fail=1
( cd "$ROOT/gui-wails" && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -tags server -o "$OUT/tailfile-wails-server-darwin-amd64" . ) || fail=1
( cd "$ROOT/gui-wails" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags server -o "$OUT/tailfile-wails-server-darwin-arm64" . ) || fail=1

note "wails desktop"
if pkg-config --exists gtk4 webkitgtk-6.0; then
  ( cd "$ROOT/gui-wails" && CGO_ENABLED=1 go build -o "$OUT/tailfile-wails-desktop-linux" . ) || fail=1
else
  echo "skip linux desktop: pkg-config gtk4 webkitgtk-6.0 not found"
fi
( cd "$ROOT/gui-wails" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o "$OUT/tailfile-wails-desktop-windows.exe" . ) || echo "windows desktop build failed (see above)"
( cd "$ROOT/gui-wails" && CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -o "$OUT/tailfile-wails-desktop-windows-arm64.exe" . ) || echo "windows/arm64 desktop build failed (see above)"
if ( cd "$ROOT/gui-wails" && CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o "$OUT/tailfile-wails-desktop-darwin" . >/tmp/wails-darwin-desktop.log 2>&1 ); then
  echo "darwin desktop build unexpectedly succeeded"
else
  echo "darwin desktop build failed, as expected without a macOS SDK:"
  tail -5 /tmp/wails-darwin-desktop.log
fi

note "sizes"
wc -c "$OUT"/tailfile-* | sort
echo
if [[ "$fail" -ne 0 ]]; then
  echo "build-all: a required step failed"
  exit 1
fi
echo "build-all: required steps passed"
