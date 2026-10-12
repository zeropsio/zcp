#!/bin/bash
# PostToolUse hook (ASYNC): Remind to check AGENTS.md when key files change
# Always exit 0 — informational only

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')
[ -z "$FILE_PATH" ] && exit 0

# Unified key file patterns from both source repos (zaia + zaia-mcp)
if echo "$FILE_PATH" | grep -qE '(go\.mod$|cmd/.+\.go$|internal/platform/client\.go|internal/server/server\.go|internal/tools/.+\.go$|internal/auth/auth\.go|internal/knowledge/engine\.go)'; then
    jq -n --arg file "$(basename "$FILE_PATH")" \
      '{ additionalContext: ("Key file changed: " + $file + ". Check whether AGENTS.md knowledge routes or traps need updating; architecture details live in docs/spec-architecture.md.") }'
fi

exit 0
