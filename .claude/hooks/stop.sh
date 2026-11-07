#!/bin/bash
# Stop hook: verify go vet passes before Claude finishes responding.
# Tests run only when the owner explicitly asks; this hook never runs them.
# Exit 0 with JSON {ok: true/false} — Stop hooks use ok/reason protocol
# CRITICAL: Must check stop_hook_active FIRST to prevent infinite loops

# Require jq — without it, loop guard and JSON output break; fail open (allow stop)
command -v jq &>/dev/null || { echo '{"ok": true}'; exit 0; }

INPUT=$(cat)

# ============================================================
# INFINITE LOOP GUARD — must be the FIRST check
# When stop_hook_active is true, another stop hook is already running.
# We MUST return ok:true immediately to break the loop.
# ============================================================
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
if [ "$ACTIVE" = "true" ]; then
    echo '{"ok": true}'
    exit 0
fi

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || { echo '{"ok": true}'; exit 0; }

# Skip if no Go files
[ -f "go.mod" ] || { echo '{"ok": true}'; exit 0; }

PROBLEMS=""

# Run vet
VET_OUTPUT=$(go vet ./... 2>&1)
if [ -n "$VET_OUTPUT" ]; then
    VET_LINES=$(echo "$VET_OUTPUT" | tail -3 | tr '\n' ' ')
    PROBLEMS+="Vet issues: ${VET_LINES} "
fi

if [ -n "$PROBLEMS" ]; then
    jq -n --arg reason "$PROBLEMS" '{"ok": false, "reason": $reason}'
else
    echo '{"ok": true}'
fi

exit 0
