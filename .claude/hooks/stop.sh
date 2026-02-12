#!/bin/bash
# Stop hook: verify tests + vet pass before Claude finishes responding
# Exit 0 with JSON {ok: true/false} — Stop hooks use ok/reason protocol
# CRITICAL: Must check stop_hook_active FIRST to prevent infinite loops
# PERF: Skips tests if no .go files changed since last test run

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

CACHE_FILE="$CLAUDE_PROJECT_DIR/.claude/last-test-result"

# ============================================================
# SKIP IF NO .go FILES CHANGED SINCE LAST FULL TEST RUN
# Only trusts cache from full runs (SCOPE=./...), not partial post-edit runs
# ============================================================
if [ -f "$CACHE_FILE" ]; then
    CACHE_SCOPE=$(head -1 "$CACHE_FILE" 2>/dev/null)
    if [ "$CACHE_SCOPE" = "SCOPE=./..." ]; then
        NEWER_GO=$(find . -name '*.go' -not -path './vendor/*' -newer "$CACHE_FILE" -print -quit 2>/dev/null)
        if [ -z "$NEWER_GO" ]; then
            if tail -n +2 "$CACHE_FILE" | grep -qE 'FAIL' 2>/dev/null; then
                FAIL_LINES=$(tail -n +2 "$CACHE_FILE" | grep -E 'FAIL|---' | tail -5 | tr '\n' ' ')
                jq -n --arg reason "Tests failing (cached): ${FAIL_LINES}" '{"ok": false, "reason": $reason}'
            else
                echo '{"ok": true}'
            fi
            exit 0
        fi
    fi
fi

PROBLEMS=""

# Run tests (timeout must be LESS than hook timeout: 60s test < 90s hook)
TEST_OUTPUT=$(go test ./... -count=1 -short -timeout=60s 2>&1)

# Cache results with full-scope marker
{ echo "SCOPE=./..."; echo "$TEST_OUTPUT"; } > "$CACHE_FILE" 2>/dev/null

if echo "$TEST_OUTPUT" | grep -qE 'FAIL'; then
    FAIL_LINES=$(echo "$TEST_OUTPUT" | grep -E 'FAIL|---' | tail -5 | tr '\n' ' ')
    PROBLEMS+="Tests failing: ${FAIL_LINES} "
fi

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
