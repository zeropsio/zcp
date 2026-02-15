#!/bin/bash
# Cleanup functional eval services (evalapp, evaldb) from Zerops.
#
# Usage:
#   ./eval/functional/cleanup-dev.sh
#   EVAL_REMOTE_HOST=myhost ./eval/functional/cleanup-dev.sh

set -euo pipefail

REMOTE_HOST="${EVAL_REMOTE_HOST:-zcpx}"

echo "==> Cleaning up functional eval services on $REMOTE_HOST..."

ssh -o ServerAliveInterval=30 "$REMOTE_HOST" \
  "claude --dangerously-skip-permissions \
  -p 'Delete the services with hostnames \"evalapp\" and \"evaldb\". Confirm each deletion. If a service does not exist, skip it silently.' \
  --model haiku \
  --max-turns 10 \
  --no-session-persistence > /tmp/eval_func_cleanup.log 2>&1 && cat /tmp/eval_func_cleanup.log"

echo "==> Cleanup complete."
