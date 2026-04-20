#!/bin/bash
# Run a single scenario on the zcp container and pull results back.
#
# Flow:
#   1. Cross-compile zcp + ship to remote (build-deploy.sh)
#   2. Upload scenario file + fixtures to remote /tmp/zcp-scenarios/
#   3. SSH in, run `zcp eval scenario` — scenario runner cleans /var/www,
#      regenerates CLAUDE.md via zcp init, then spawns Claude CLI there
#   4. SCP results dir back to ./eval/results/
#
# Usage:
#   ./eval/scripts/run-scenario.sh internal/eval/scenarios/greenfield-laravel-weather.md
#   EVAL_REMOTE_HOST=myhost ./eval/scripts/run-scenario.sh <scenario>
#   EVAL_SKIP_BUILD=1 ./eval/scripts/run-scenario.sh <scenario>   # reuse already-shipped binary

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
REMOTE_HOST="${EVAL_REMOTE_HOST:-zcp}"
REMOTE_WORK="/var/www"
REMOTE_SCENARIOS="/tmp/zcp-scenarios"
REMOTE_BIN="${EVAL_REMOTE_BIN:-/usr/local/bin/zcp}"

if [ $# -lt 1 ]; then
    echo "usage: $0 <path-to-scenario.md>" >&2
    exit 1
fi
SCENARIO="$1"
if [ ! -f "$SCENARIO" ]; then
    echo "error: scenario file not found: $SCENARIO" >&2
    exit 1
fi

SCENARIO_ABS="$(cd "$(dirname "$SCENARIO")" && pwd)/$(basename "$SCENARIO")"
SCENARIO_BASE="$(basename "$SCENARIO")"
FIXTURES_DIR="$PROJECT_DIR/internal/eval/scenarios/fixtures"

# 1. Build + ship (unless skipped)
if [ "${EVAL_SKIP_BUILD:-0}" != "1" ]; then
    echo "==> Building + shipping zcp binary to $REMOTE_HOST..."
    "$SCRIPT_DIR/build-deploy.sh"
fi

# 2. Upload scenario + fixtures
SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
echo "==> Uploading scenario + fixtures to $REMOTE_HOST:$REMOTE_SCENARIOS..."
ssh $SSH_OPTS "$REMOTE_HOST" "mkdir -p $REMOTE_SCENARIOS/fixtures"
scp $SSH_OPTS -q "$SCENARIO_ABS" "$REMOTE_HOST:$REMOTE_SCENARIOS/$SCENARIO_BASE"
if [ -d "$FIXTURES_DIR" ] && [ -n "$(ls -A "$FIXTURES_DIR" 2>/dev/null)" ]; then
    scp $SSH_OPTS -q -r "$FIXTURES_DIR"/* "$REMOTE_HOST:$REMOTE_SCENARIOS/fixtures/"
fi

# 3. Run scenario on zcp. The scenario runner handles seed cleanup + zcp init.
echo "==> Running scenario on $REMOTE_HOST (workDir=$REMOTE_WORK)..."
ssh $SSH_OPTS -o ServerAliveInterval=30 -o ServerAliveCountMax=60 "$REMOTE_HOST" \
  "cd $REMOTE_WORK && \
   ZCP_EVAL_WORK_DIR=$REMOTE_WORK \
   ZCP_EVAL_RESULTS_DIR=$REMOTE_WORK/.zcp/eval/results \
   $REMOTE_BIN eval scenario --file $REMOTE_SCENARIOS/$SCENARIO_BASE" \
  || true  # grade-fail exits non-zero; we still want the results

# 4. Pull results back
TAG=$(date +%Y%m%d_%H%M%S)
LOCAL_RESULTS="$PROJECT_DIR/eval/results/scenario-$TAG"
mkdir -p "$LOCAL_RESULTS"
echo "==> Fetching results to $LOCAL_RESULTS/..."
scp $SSH_OPTS -q -r "$REMOTE_HOST:$REMOTE_WORK/.zcp/eval/results/." "$LOCAL_RESULTS/" 2>/dev/null \
  || echo "==> Warning: no results directory on remote"

echo ""
echo "=== Scenario run complete ==="
echo "Local results: $LOCAL_RESULTS/"
find "$LOCAL_RESULTS" -name result.json -exec echo "  {}" \;
