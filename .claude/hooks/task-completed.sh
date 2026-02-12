#!/bin/bash
# TaskCompleted hook: verify tests + vet pass before task completion
# Exit 0 = allow completion, Exit 2 = block (stderr fed to Claude)

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || exit 0

# Skip if no Go files
[ -f "go.mod" ] || exit 0

CACHE_FILE="$CLAUDE_PROJECT_DIR/.claude/last-test-result"

# Run tests
TEST_OUTPUT=$(go test ./... -count=1 -short -timeout=60s 2>&1)

# Cache results with full-scope marker
{ echo "SCOPE=./..."; echo "$TEST_OUTPUT"; } > "$CACHE_FILE" 2>/dev/null

if echo "$TEST_OUTPUT" | grep -qE 'FAIL'; then
    echo "Cannot complete task: tests are failing" >&2
    echo "$TEST_OUTPUT" | grep -E 'FAIL|---' | tail -10 >&2
    exit 2
fi

# Run vet
VET_OUTPUT=$(go vet ./... 2>&1)
if [ -n "$VET_OUTPUT" ]; then
    echo "Cannot complete task: go vet reports issues" >&2
    echo "$VET_OUTPUT" | tail -5 >&2
    exit 2
fi

exit 0
