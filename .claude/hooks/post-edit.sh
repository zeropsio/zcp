#!/bin/bash
# PostToolUse hook (ASYNC): Auto-run tests + vet + fast lint after Go file edits
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
CACHE_FILE="$CLAUDE_PROJECT_DIR/.claude/last-test-result"

if [ -d "$PKG_DIR" ]; then
    # Incremental test: if editing a test file, target specific test function
    RUN_FLAG=""
    if echo "$FILE_PATH" | grep -qE '_test\.go$'; then
        # Extract test function names from the edited file, use the last one
        LAST_FUNC=$(grep -oE 'func (Test[A-Za-z0-9_]+)' "$FILE_PATH" 2>/dev/null | tail -1 | awk '{print $2}')
        if [ -n "$LAST_FUNC" ]; then
            RUN_FLAG="-run ${LAST_FUNC}"
        fi
    fi

    # Run tests (short mode for speed, with optional -run targeting)
    if [ -n "$RUN_FLAG" ]; then
        TEST_OUTPUT=$(go test "./${PKG_DIR}" $RUN_FLAG -count=1 -short -timeout=15s 2>&1)
    else
        TEST_OUTPUT=$(go test "./${PKG_DIR}" -count=1 -short -timeout=30s 2>&1)
    fi

    # Cache test results for session-start.sh
    echo "$TEST_OUTPUT" > "$CACHE_FILE" 2>/dev/null

    if echo "$TEST_OUTPUT" | grep -qE 'FAIL'; then
        FAIL_LINES=$(echo "$TEST_OUTPUT" | grep -E 'FAIL|---' | tail -10)
        PROBLEMS+="Tests FAILED in ./${PKG_DIR}:\n${FAIL_LINES}\n"
    fi

    # Run vet
    VET_OUTPUT=$(go vet "./${PKG_DIR}" 2>&1)
    if [ -n "$VET_OUTPUT" ]; then
        PROBLEMS+="Vet issues in ./${PKG_DIR}:\n${VET_OUTPUT}\n"
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
    echo "{\"additionalContext\": \"Tests, vet, and lint passed for ./${PKG_DIR}\"}"
fi

exit 0
