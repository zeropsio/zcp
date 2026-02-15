#!/bin/bash
# Emergency cleanup: delete all eval* services from Zerops via remote Claude.
# Matches both random hostnames (evalappbun242) and fixed (evalappnodejs).
#
# Usage:
#   ./eval/scripts/cleanup.sh              # Default: cleanup on zcpx
#   EVAL_REMOTE_HOST=myhost ./eval/scripts/cleanup.sh

set -euo pipefail

REMOTE_HOST="${EVAL_REMOTE_HOST:-zcpx}"

CLEANUP_PROMPT='List all services. Then delete every service whose hostname starts with "eval". Confirm deletion for each. If there are no eval* services, just say "Nothing to clean up."'

echo "==> Cleaning up eval* services on $REMOTE_HOST..."

ssh -o ServerAliveInterval=30 "$REMOTE_HOST" \
  "claude --dangerously-skip-permissions \
  -p '$CLEANUP_PROMPT' \
  --model haiku \
  --max-turns 15 \
  --no-session-persistence > /tmp/eval_cleanup.log 2>&1 && cat /tmp/eval_cleanup.log"

echo "==> Cleanup complete."
