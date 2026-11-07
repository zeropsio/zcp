#!/bin/bash
# PreCompact hook: save project state before context compaction
# Cannot block compaction — only saves state for recovery

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || exit 0
[ -f "go.mod" ] || exit 0

STATE_FILE="$CLAUDE_PROJECT_DIR/.claude/compact-state.md"

{
    echo "# Pre-compaction state ($(date -u +%Y-%m-%dT%H:%M:%SZ))"
    echo ""
    echo "## Branch"
    git branch --show-current 2>/dev/null
    echo ""
    echo "## Uncommitted changes"
    git diff --stat HEAD 2>/dev/null | tail -10
} > "$STATE_FILE" 2>/dev/null

jq -n '{ additionalContext: "State saved to .claude/compact-state.md. Read it after compaction to restore context about current work." }'

exit 0
