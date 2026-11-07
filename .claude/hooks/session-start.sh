#!/bin/bash
# SessionStart hook: inject project context
# Always exit 0 — provides additionalContext via JSON

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

# Uncommitted changes summary
DIRTY=$(git diff --stat HEAD 2>/dev/null | tail -1)
[ -n "$DIRTY" ] && CONTEXT+="Uncommitted: ${DIRTY}. "

if [ -n "$CONTEXT" ]; then
    jq -n --arg ctx "$CONTEXT" '{ additionalContext: $ctx }'
fi

exit 0
