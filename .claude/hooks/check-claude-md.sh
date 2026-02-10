#!/bin/bash
# PostToolUse hook (ASYNC): Remind to check CLAUDE.md when key files change
# Always exit 0 — informational only

INPUT=$(cat)
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')
[ -z "$FILE_PATH" ] && exit 0

# Unified key file patterns from both source repos (zaia + zaia-mcp)
if echo "$FILE_PATH" | grep -qE '(go\.mod$|cmd/.+\.go$|internal/platform/client\.go|internal/server/server\.go|internal/tools/.+\.go$|internal/auth/manager\.go|internal/knowledge/engine\.go)'; then
    jq -n --arg file "$(basename "$FILE_PATH")" \
      '{ additionalContext: ("Key file changed: " + $file + ". Check if CLAUDE.md Architecture table needs updating.") }'
fi

exit 0
