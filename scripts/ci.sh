#!/bin/bash
# Wrapper that injects the CI read-only PAT into `gh` from .claude/github-ci-token.
#
# Why: Claude Code's permission settings deny Read on *token* files, so the
# agent cannot extract the token into its own context. This wrapper runs as the
# user, reads the file directly, and execs `gh` — the agent sees only the gh
# subcommand arguments, never the token.
#
# Setup (one line):
#   echo '<YOUR_PAT>' > .claude/github-ci-token
#
# Rotation (one line):
#   echo '<NEW_PAT>' > .claude/github-ci-token
#
# Usage:
#   ./scripts/ci.sh run list --branch main --limit 3
#   ./scripts/ci.sh run watch <run-id>
#   ./scripts/ci.sh run view <run-id> --log-failed

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TOKEN_FILE="$SCRIPT_DIR/../.claude/github-ci-token"

if [ ! -r "$TOKEN_FILE" ]; then
  echo "scripts/ci.sh: $TOKEN_FILE not found or unreadable." >&2
  echo "Seed it with: echo '<YOUR_PAT>' > .claude/github-ci-token" >&2
  echo "The PAT only needs 'actions:read' scope for watching CI runs." >&2
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "scripts/ci.sh: 'gh' CLI not found — install via 'brew install gh'" >&2
  exit 2
fi

# Strip comments and trailing whitespace — forgiving about trailing newlines.
GH_TOKEN="$(grep -v '^#' "$TOKEN_FILE" | head -1 | tr -d '\r\n[:space:]')"
if [ -z "$GH_TOKEN" ]; then
  echo "scripts/ci.sh: $TOKEN_FILE is empty." >&2
  exit 1
fi
export GH_TOKEN

exec gh "$@"
