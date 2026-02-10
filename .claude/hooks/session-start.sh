#!/bin/bash
# SessionStart hook: inject project context
# Always exit 0 — provides additionalContext via JSON
# Does NOT run tests — reads cached results from last post-edit/stop hook

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || exit 0

# Skip if no Go files yet (empty project)
[ -f "go.mod" ] || exit 0

CONTEXT=""

# Current branch
BRANCH=$(git branch --show-current 2>/dev/null)
[ -n "$BRANCH" ] && CONTEXT+="Branch: ${BRANCH}. "

# Recent commits (one-line)
RECENT=$(git log --oneline -3 2>/dev/null | tr '\n' ' ')
[ -n "$RECENT" ] && CONTEXT+="Recent: ${RECENT}. "

# Cached test results (written by post-edit.sh and stop.sh)
CACHED="$CLAUDE_PROJECT_DIR/.claude/last-test-result"
if [ -f "$CACHED" ]; then
    AGE=$(( $(date +%s) - $(stat -f%m "$CACHED" 2>/dev/null || stat -c%Y "$CACHED" 2>/dev/null || echo 0) ))
    if [ "$AGE" -lt 3600 ]; then
        FAIL_LINES=$(grep -E 'FAIL' "$CACHED" 2>/dev/null | head -3 | tr '\n' ' ')
        if [ -n "$FAIL_LINES" ]; then
            CONTEXT+="FAILING (cached ${AGE}s ago): ${FAIL_LINES}. "
        else
            CONTEXT+="Tests passing (cached ${AGE}s ago). "
        fi
    else
        CONTEXT+="Test cache stale (${AGE}s). Run 'go test -short ./...' to refresh. "
    fi
fi

# Uncommitted changes summary
DIRTY=$(git diff --stat HEAD 2>/dev/null | tail -1)
[ -n "$DIRTY" ] && CONTEXT+="Uncommitted: ${DIRTY}. "

if [ -n "$CONTEXT" ]; then
    jq -n --arg ctx "$CONTEXT" '{ additionalContext: $ctx }'
fi

exit 0
