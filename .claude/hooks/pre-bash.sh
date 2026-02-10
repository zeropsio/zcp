#!/bin/bash
# PreToolUse hook: Security gate + pre-commit lint gate + pre-push test gate
# Exit 0 = allow, Exit 2 = block (stderr fed to Claude)

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
[ -z "$COMMAND" ] && exit 0

# ============================================================
# SECURITY GATE — block destructive operations
# ============================================================

# Block rm -rf / rm -r with broad or relative dangerous targets
# Catches: rm -rf /, rm -rf ., rm -rf .., rm -rf ~, rm -rf *, rm -r /foo && ..., etc.
if echo "$COMMAND" | grep -qE 'rm\s+(-[a-zA-Z]*r[a-zA-Z]*\s+)+(\.\.|\/|\.(\s|$)|~|\$HOME|\$\{HOME\}|\*)'; then
    echo "BLOCKED: Destructive rm command. Specify exact paths instead." >&2
    exit 2
fi

# Block dangerous git operations
# force push, hard reset, clean -f, checkout ., restore ., stash drop
if echo "$COMMAND" | grep -qE 'git\s+(push\s+.*--force|push\s+-f\b|reset\s+--hard|clean\s+-[a-zA-Z]*f|checkout\s+(--\s+)?\.(\s|$)|restore\s+\.(\s|$)|stash\s+drop)'; then
    echo "BLOCKED: Dangerous git operation. Use safer alternatives." >&2
    exit 2
fi

# Block chmod 777 and other overly permissive operations
if echo "$COMMAND" | grep -qE 'chmod\s+777'; then
    echo "BLOCKED: chmod 777 is too permissive. Use specific permissions." >&2
    exit 2
fi

# Block pipe-to-shell patterns (curl|bash, wget|sh, etc.)
if echo "$COMMAND" | grep -qE '\|\s*(ba)?sh\b|\|\s*sudo'; then
    echo "BLOCKED: Piping to shell is dangerous. Download first, inspect, then execute." >&2
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
GO_STAGED=$(echo "$STAGED" | grep -E '\.go$' | head -1)
if [ -n "$GO_STAGED" ]; then
    if command -v golangci-lint &>/dev/null; then
        echo "-- pre-commit: golangci-lint run ./... --"
        LINT_OUTPUT=$(golangci-lint run ./... --timeout=60s 2>&1)
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
