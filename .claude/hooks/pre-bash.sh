#!/bin/bash
# PreToolUse hook: Security gate + pre-commit lint gate + pre-push test gate
# Exit 0 = allow, Exit 2 = block (stderr fed to Claude)

# Require jq — without it, security checks silently fail
command -v jq &>/dev/null || { echo "BLOCKED: jq is required for security checks" >&2; exit 2; }

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

# ============================================================
# SECURITY GATE — block destructive operations
# ============================================================

# Block recursive rm targeting dangerous paths
# Two-pass: (1) rm with recursive flag in any position, (2) dangerous target
if echo "$COMMAND" | grep -qE 'rm\s' && \
   echo "$COMMAND" | grep -qE '\s-[a-zA-Z]*[rR]|--recursive' && \
   echo "$COMMAND" | grep -qE '\s(\.\.|\./?|/|~|\$HOME|\$\{HOME\}|\*)(\s|$)'; then
    echo "BLOCKED: Destructive rm command. Specify exact paths instead." >&2
    exit 2
fi

# Block dangerous git operations
# force push (but allow --force-with-lease), hard reset, clean -f, checkout .,
# restore ., stash drop/clear, branch -D, push --delete
if echo "$COMMAND" | grep -qE 'git\s+(push\s+.*--force(\s|$)|push\s+-f\b|reset\s+--hard|clean\s+-[a-zA-Z]*f|checkout\s+(--\s+)?\.(\s|$)|checkout\s+-f\b|restore\s+\.(\s|$)|stash\s+(drop|clear)|branch\s+-[a-zA-Z]*D|push\s+\S+\s+--delete)'; then
    echo "BLOCKED: Dangerous git operation. Use safer alternatives." >&2
    exit 2
fi

# Block chmod 777 and other overly permissive operations
if echo "$COMMAND" | grep -qE 'chmod\s+777'; then
    echo "BLOCKED: chmod 777 is too permissive. Use specific permissions." >&2
    exit 2
fi

# Block pipe-to-shell/interpreter patterns (curl|bash, wget|zsh, python, etc.)
if echo "$COMMAND" | grep -qE '\|\s*((ba|z|da|c|tc|fi)?sh|/\S*sh|env\s+\S*sh|python[23]?|ruby|perl|node)\b|\|\s*sudo'; then
    echo "BLOCKED: Piping to shell/interpreter is dangerous. Download first, inspect, then execute." >&2
    exit 2
fi

# Block dd to disk devices
if echo "$COMMAND" | grep -qE 'dd\s+.*of=/dev/'; then
    echo "BLOCKED: Direct disk write with dd." >&2
    exit 2
fi

# ============================================================
# PRE-PUSH TEST GATE — block push if tests fail
# ============================================================

if echo "$COMMAND" | grep -qE '^git\s+push'; then
    MODULE_ROOT="$CLAUDE_PROJECT_DIR"
    [ -z "$MODULE_ROOT" ] && MODULE_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
    cd "$MODULE_ROOT" || exit 0
    [ -f "go.mod" ] || exit 0

    TEST_OUTPUT=$(go test ./... -count=1 -short -timeout=60s 2>&1)
    if echo "$TEST_OUTPUT" | grep -qE 'FAIL'; then
        echo "BLOCKED: Tests failing. Fix before pushing." >&2
        echo "$TEST_OUTPUT" | grep -E 'FAIL|---' | tail -5 >&2
        exit 2
    fi

    exit 0
fi

# ============================================================
# PRE-COMMIT LINT GATE — block commit if lint fails
# ============================================================

echo "$COMMAND" | grep -qE '^git\s+commit' || exit 0

MODULE_ROOT="$CLAUDE_PROJECT_DIR"
[ -z "$MODULE_ROOT" ] && MODULE_ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$MODULE_ROOT" || exit 0

STAGED=$(git diff --cached --name-only 2>/dev/null)
[ -z "$STAGED" ] && exit 0

# Check if CLAUDE.md should be staged
KEY_PATTERNS='internal/.*/[a-z]+\.go$|go\.mod$|cmd/'
KEY_STAGED=$(echo "$STAGED" | grep -E "$KEY_PATTERNS" | head -5)
CLAUDE_MD_STAGED=$(echo "$STAGED" | grep -E 'CLAUDE\.md$')

if [ -n "$KEY_STAGED" ] && [ -z "$CLAUDE_MD_STAGED" ]; then
    echo "CLAUDE.md CHECK: Key files staged but CLAUDE.md not included. Consider: git add CLAUDE.md"
fi

# Lint gate — exit 2 to actually block the commit
GO_STAGED=$(echo "$STAGED" | grep -E '\.go$')
if [ -n "$GO_STAGED" ]; then
    LINT_BIN="golangci-lint"
    [ -x "${MODULE_ROOT}/bin/golangci-lint" ] && LINT_BIN="${MODULE_ROOT}/bin/golangci-lint"
    if command -v "$LINT_BIN" &>/dev/null || [ -x "$LINT_BIN" ]; then
        # Lint only packages with staged Go files (not entire codebase)
        LINT_PKGS=$(echo "$GO_STAGED" | while read -r f; do dirname "./$f"; done | sort -u | tr '\n' ' ')
        echo "-- pre-commit: $LINT_BIN on changed packages --"
        LINT_OUTPUT=$($LINT_BIN run $LINT_PKGS --timeout=60s 2>&1)
        LINT_EXIT=$?
        if [ $LINT_EXIT -ne 0 ]; then
            echo "$LINT_OUTPUT" | tail -30 >&2
            echo "LINT FAILED: fix issues before committing" >&2
            exit 2
        else
            echo "-- LINT PASSED --"
        fi
    fi
fi

exit 0
