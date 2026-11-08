#!/bin/bash
# Runs the golangci-lint version .golangci.yaml needs (v2; a v1 binary rejects
# the config). Resolution: this checkout's bin/, then the main checkout's bin/
# (a linked worktree shares its git common dir and has no gitignored bin/),
# else the pinned version is installed into this checkout's bin/. PATH is never
# consulted: a stray v1 there is what breaks the hooks. Keep VERSION equal to
# the golangci-lint-action version in .github/workflows/ci.yml.
set -euo pipefail

VERSION=2.8.0
ROOT=$(cd "$(dirname "$0")/.." && pwd)
COMMON=$(git -C "$ROOT" rev-parse --path-format=absolute --git-common-dir)

pinned() {
	[ -x "$1" ] && [ "$("$1" version --short 2>/dev/null)" = "$VERSION" ]
}

for lint in "$ROOT/bin/golangci-lint" "$(dirname "$COMMON")/bin/golangci-lint"; do
	pinned "$lint" && exec "$lint" "$@"
done

echo "golangci-lint: installing v$VERSION into $ROOT/bin" >&2
GOBIN="$ROOT/bin" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$VERSION" >&2
exec "$ROOT/bin/golangci-lint" "$@"
