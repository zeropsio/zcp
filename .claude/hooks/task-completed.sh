#!/bin/bash
# TaskCompleted hook: verify go vet passes before task completion.
# Tests run only when the owner explicitly asks; this hook never runs them.
# Exit 0 = allow completion, Exit 2 = block (stderr fed to Claude)

cd "$CLAUDE_PROJECT_DIR" 2>/dev/null || exit 0

# Skip if no Go files
[ -f "go.mod" ] || exit 0

# Run vet
VET_OUTPUT=$(go vet ./... 2>&1)
if [ -n "$VET_OUTPUT" ]; then
    echo "Cannot complete task: go vet reports issues" >&2
    echo "$VET_OUTPUT" | tail -5 >&2
    exit 2
fi

exit 0
