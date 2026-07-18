#!/bin/bash
# PostToolUseFailure hook: detect common failure patterns and suggest fixes
# Always exit 0 — provides additionalContext

INPUT=$(cat)
STDERR=$(echo "$INPUT" | jq -r '.tool_response.stderr // empty')

[ -z "$STDERR" ] && exit 0

SUGGESTION=""

# Package not found
if echo "$STDERR" | grep -qE 'no required module provides package|cannot find package'; then
    SUGGESTION="Run 'go mod tidy' to resolve missing packages."
fi

# Build constraint / tag issue
if echo "$STDERR" | grep -qE 'build constraints exclude|no Go files'; then
    SUGGESTION="Check build tags. E2E tests need '-tags e2e'. Some files may have //go:build constraints."
fi

# Permission denied
if echo "$STDERR" | grep -qE 'permission denied'; then
    SUGGESTION="Check file permissions. Hook scripts need 'chmod +x'."
fi

# golangci-lint not found
if echo "$STDERR" | grep -qE 'golangci-lint.*not found|command not found.*golangci'; then
    SUGGESTION="Install golangci-lint: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0"
fi

# Import cycle
if echo "$STDERR" | grep -qE 'import cycle not allowed'; then
    SUGGESTION="Import cycle detected. Move shared types to a separate package or use interfaces."
fi

# Undefined / undeclared
if echo "$STDERR" | grep -qE 'undefined:|undeclared name'; then
    SUGGESTION="Undefined symbol. Check spelling, imports, and whether the type/function is exported."
fi

if [ -n "$SUGGESTION" ]; then
    jq -n --arg s "$SUGGESTION" '{ additionalContext: $s }'
fi

exit 0
