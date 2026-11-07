#!/bin/bash
# PostToolUse hook (ASYNC): go vet + fast lint after Go file edits.
# Tests run only when the owner explicitly asks; this hook never runs them.
# Runs in background — results delivered to Claude on next turn
# NOTE: "decision": "block" is ADVISORY — the edit is already applied.
# It tells Claude to prioritize fixing the issue, but cannot undo the edit.

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')
[ -z "$FILE_PATH" ] && exit 0

# Only for Go files
echo "$FILE_PATH" | grep -qE '\.go$' || exit 0

# Skip generated, test data, embedded files
echo "$FILE_PATH" | grep -qE '(testdata/|embed/|\.pb\.go$|_generated\.go$)' && exit 0

MODULE_ROOT="$CLAUDE_PROJECT_DIR"
[ -z "$MODULE_ROOT" ] && MODULE_ROOT=$(cd "$(dirname "$0")/../.." && pwd)

# Only run for files in this project
echo "$FILE_PATH" | grep -q "$MODULE_ROOT" || exit 0

cd "$MODULE_ROOT" || exit 0

# Determine package from file path
PKG_DIR=$(echo "$FILE_PATH" | sed "s|^${MODULE_ROOT}/||" | xargs dirname)

PROBLEMS=""

if [ -d "$PKG_DIR" ]; then
    VET_OUTPUT=$(go vet "./${PKG_DIR}" 2>&1)
    if [ $? -ne 0 ] && [ -n "$VET_OUTPUT" ]; then
        VET_LINES=$(echo "$VET_OUTPUT" | tail -10)
        PROBLEMS+="Vet issues in ./${PKG_DIR}:\n${VET_LINES}\n"
    fi

    # Fast lint (non-blocking)
    if command -v golangci-lint &>/dev/null; then
        LINT_OUTPUT=$(golangci-lint run "./${PKG_DIR}" --fast-only --timeout=30s 2>&1)
        LINT_EXIT=$?
        if [ $LINT_EXIT -ne 0 ] && [ -n "$LINT_OUTPUT" ]; then
            LINT_LINES=$(echo "$LINT_OUTPUT" | tail -10)
            PROBLEMS+="Lint warnings in ./${PKG_DIR}:\n${LINT_LINES}\n"
        fi
    fi
fi

# Structured JSON feedback
if [ -n "$PROBLEMS" ]; then
    # Escape for JSON
    ESCAPED=$(echo -e "$PROBLEMS" | jq -Rs '.')
    # NOTE: "decision": "block" is advisory — tells Claude to prioritize fixing,
    # but cannot undo the edit (it's already applied in async context)
    echo "{\"decision\": \"block\", \"reason\": ${ESCAPED}}"
else
    echo "{\"additionalContext\": \"Vet and lint passed for ./${PKG_DIR}\"}"
fi

exit 0
