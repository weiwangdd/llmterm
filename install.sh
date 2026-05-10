#!/usr/bin/env bash
# llmterm one-liner installer.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/weiwangdd/llmterm/main/install.sh | bash
#
# What it does:
#   1. checks Go is installed
#   2. runs `go install github.com/weiwangdd/llmterm/cmd/llmterm@latest`
#   3. runs `llmterm onboard` so you can pick a backend and wire up zsh
set -eu

REPO="github.com/weiwangdd/llmterm"
PKG="$REPO/cmd/llmterm@latest"

c() { printf '\033[%sm%s\033[0m' "$1" "$2"; }
say() { printf '%s %s\n' "$(c '1;36' '→')" "$*"; }
warn() { printf '%s %s\n' "$(c '1;33' '!')" "$*" >&2; }
die() { printf '%s %s\n' "$(c '1;31' '✗')" "$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die "Go is required but not found in PATH.
   Install from https://go.dev/dl/  (or: brew install go)
   Then re-run this installer."

GO_VERSION=$(go env GOVERSION 2>/dev/null || echo unknown)
say "using $GO_VERSION"

GOBIN=$(go env GOBIN)
[ -n "$GOBIN" ] || GOBIN="$(go env GOPATH)/bin"
mkdir -p "$GOBIN"

say "go install $PKG"
GOFLAGS=${GOFLAGS:-} go install "$PKG"

BIN="$GOBIN/llmterm"
[ -x "$BIN" ] || die "go install completed but $BIN is missing — check \$GOPATH / \$GOBIN."

say "installed: $BIN"

case ":$PATH:" in
  *":$GOBIN:"*) ;;
  *) warn "$GOBIN is not in your PATH.
   Add this to your shell rc:  export PATH=\"$GOBIN:\$PATH\"" ;;
esac

# Hand off to onboard. It probes backends, picks a default, and offers
# to add the eval line to ~/.zshrc.
echo
"$BIN" onboard "$@"
