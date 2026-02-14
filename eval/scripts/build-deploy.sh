#!/bin/bash
# Build ZCP for Linux amd64 and deploy to remote host via scp.
#
# Usage:
#   ./eval/scripts/build-deploy.sh              # Default: deploy to zcpx
#   EVAL_REMOTE_HOST=myhost ./eval/scripts/build-deploy.sh
#
# Environment:
#   EVAL_REMOTE_HOST  — SSH host (default: zcpx)
#   EVAL_REMOTE_BIN   — Remote binary path (default: /home/zerops/.local/bin/zcp)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
REMOTE_HOST="${EVAL_REMOTE_HOST:-zcpx}"
REMOTE_BIN="${EVAL_REMOTE_BIN:-/home/zerops/.local/bin/zcp}"
LOCAL_BIN="$PROJECT_DIR/builds/zcp-linux-amd64"

echo "==> Building ZCP for linux/amd64..."
(cd "$PROJECT_DIR" && make linux-amd)

echo "==> Deploying to $REMOTE_HOST:$REMOTE_BIN..."
# Upload to temp file and mv (handles running binary that can't be overwritten)
REMOTE_TMP="${REMOTE_BIN}.tmp.$$"
scp "$LOCAL_BIN" "$REMOTE_HOST:$REMOTE_TMP"
ssh "$REMOTE_HOST" "mv -f '$REMOTE_TMP' '$REMOTE_BIN' && chmod +x '$REMOTE_BIN'"

# Verify deployment
echo "==> Verifying..."
LOCAL_HASH=$(shasum -a 256 "$LOCAL_BIN" | cut -d' ' -f1)
REMOTE_HASH=$(ssh "$REMOTE_HOST" "sha256sum $REMOTE_BIN" | cut -d' ' -f1)

if [ "$LOCAL_HASH" = "$REMOTE_HASH" ]; then
    echo "==> Deploy OK (hash match: ${LOCAL_HASH:0:12}...)"
else
    echo "==> WARNING: Hash mismatch!"
    echo "    Local:  $LOCAL_HASH"
    echo "    Remote: $REMOTE_HASH"
    exit 1
fi
