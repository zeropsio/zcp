#!/bin/bash
# PostToolUseFailure hook: detect common failure patterns and suggest fixes
# Auto-records to memory/errors.md when a pattern recurs
# Always exit 0 — provides additionalContext

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // empty')
STDERR=$(echo "$INPUT" | jq -r '.tool_response.stderr // empty')

[ -z "$STDERR" ] && exit 0

SUGGESTION=""
PATTERN=""

# Package not found
if echo "$STDERR" | grep -qE 'no required module provides package|cannot find package'; then
    SUGGESTION="Run 'go mod tidy' to resolve missing packages."
    PATTERN="missing-package"
fi

# Build constraint / tag issue
if echo "$STDERR" | grep -qE 'build constraints exclude|no Go files'; then
    SUGGESTION="Check build tags. E2E tests need '-tags e2e'. Some files may have //go:build constraints."
    PATTERN="build-tags"
fi

# Permission denied
if echo "$STDERR" | grep -qE 'permission denied'; then
    SUGGESTION="Check file permissions. Hook scripts need 'chmod +x'."
    PATTERN="permission-denied"
fi

# golangci-lint not found
if echo "$STDERR" | grep -qE 'golangci-lint.*not found|command not found.*golangci'; then
    SUGGESTION="Install golangci-lint: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0"
    PATTERN="lint-not-found"
fi

# Import cycle
if echo "$STDERR" | grep -qE 'import cycle not allowed'; then
    SUGGESTION="Import cycle detected. Move shared types to a separate package or use interfaces."
    PATTERN="import-cycle"
fi

# Undefined / undeclared
if echo "$STDERR" | grep -qE 'undefined:|undeclared name'; then
    SUGGESTION="Undefined symbol. Check spelling, imports, and whether the type/function is exported."
    PATTERN="undefined"
fi

if [ -n "$SUGGESTION" ]; then
    jq -n --arg s "$SUGGESTION" '{ additionalContext: $s }'

    # Auto-record recurring patterns to memory (if memory dir exists)
    MEMORY_DIR="$HOME/.claude/projects/-Users-macbook-Documents-Zerops-MCP-zcp/memory"
    if [ -d "$MEMORY_DIR" ] && [ -n "$PATTERN" ]; then
        ERRORS_FILE="$MEMORY_DIR/errors.md"
        # Only append if this pattern isn't already recorded
        if ! grep -q "$PATTERN" "$ERRORS_FILE" 2>/dev/null; then
            echo "" >> "$ERRORS_FILE"
            echo "## $PATTERN ($(date -u +%Y-%m-%d))" >> "$ERRORS_FILE"
            echo "- Command: \`$(echo "$COMMAND" | head -c 100)\`" >> "$ERRORS_FILE"
            echo "- Fix: $SUGGESTION" >> "$ERRORS_FILE"
        fi
    fi
fi

exit 0
