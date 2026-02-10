#!/bin/bash
# PreCompact hook: save project state before context compaction
# Cannot block compaction — only saves state for recovery
# Does NOT run tests (10s timeout too short); reads cached results instead

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || exit 0
[ -f "go.mod" ] || exit 0

STATE_FILE="$CLAUDE_PROJECT_DIR/.claude/compact-state.md"
CACHE_FILE="$CLAUDE_PROJECT_DIR/.claude/last-test-result"

{
    echo "# Pre-compaction state ($(date -u +%Y-%m-%dT%H:%M:%SZ))"
    echo ""
    echo "## Branch"
    git branch --show-current 2>/dev/null
    echo ""
    echo "## Uncommitted changes"
    git diff --stat HEAD 2>/dev/null | tail -10
    echo ""
    echo "## Test status (cached)"
    if [ -f "$CACHE_FILE" ]; then
        FAIL=$(grep -E 'FAIL|---' "$CACHE_FILE" 2>/dev/null | head -10)
        if [ -n "$FAIL" ]; then
            echo "$FAIL"
        else
            echo "All tests passing (from cache)."
        fi
    else
        echo "No cached test results. Run 'go test -short ./...' after compaction."
    fi
} > "$STATE_FILE" 2>/dev/null

jq -n '{ additionalContext: "State saved to .claude/compact-state.md. Read it after compaction to restore context about current work." }'

exit 0
