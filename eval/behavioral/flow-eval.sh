#!/usr/bin/env bash
# flow-eval.sh — dev-side wrapper for behavioral eval runs.
#
#   flow-eval.sh                   list scenarios (= `list`)
#   flow-eval.sh list              list scenarios
#   flow-eval.sh <id>              run one scenario on zcp (cleanup → build → run → pull)
#   flow-eval.sh all               run every scenario sequentially on zcp
#
# All work on the remote container is delegated to `zcp eval behavioral …`.
# This wrapper only does dev-side glue: build+deploy, scenario file scp,
# invocation over ssh, and pulling artifacts back into eval/behavioral/runs/.
set -euo pipefail

cd "$(dirname "$(readlink -f "$0" 2>/dev/null || echo "$0")")/../.."  # repo root

SCENARIOS_DIR_LOCAL="eval/behavioral/scenarios"
RUNS_DIR_LOCAL="eval/behavioral/runs"
SCENARIOS_DIR_REMOTE="/tmp/zcp-behavioral-scenarios"
RESULTS_DIR_REMOTE="/var/www/.zcp/eval/results"
REMOTE_HOST="${EVAL_REMOTE_HOST:-zcp}"

SSH_OPTS=(
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o LogLevel=ERROR
  -o ServerAliveInterval=30
  -o ServerAliveCountMax=60
)

log()   { printf '==> %s\n' "$*" >&2; }
fatal() { printf 'FATAL: %s\n' "$*" >&2; exit 1; }

cmd="${1:-list}"

case "$cmd" in
  list|"")
    # List runs locally — no zcp roundtrip needed for the scenario index.
    exec go run ./cmd/zcp eval behavioral list --scenarios-dir "$SCENARIOS_DIR_LOCAL"
    ;;
esac

# All non-list paths go to the remote.
log "Building + deploying current zcp binary"
./eval/scripts/build-deploy.sh >&2

log "Syncing scenarios to $REMOTE_HOST:$SCENARIOS_DIR_REMOTE"
ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" \
  "rm -rf '$SCENARIOS_DIR_REMOTE' && mkdir -p '$SCENARIOS_DIR_REMOTE'"
scp "${SSH_OPTS[@]}" -q "$SCENARIOS_DIR_LOCAL"/*.md "$REMOTE_HOST:$SCENARIOS_DIR_REMOTE/"

# Capture suite ID from the run so we can scp just that suite back.
LOCAL_LOG=$(mktemp)
trap "rm -f $LOCAL_LOG" EXIT

case "$cmd" in
  all)
    log "Running ALL behavioral scenarios on $REMOTE_HOST"
    ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" \
      "zcp eval behavioral all --scenarios-dir '$SCENARIOS_DIR_REMOTE'" \
      2>&1 | tee "$LOCAL_LOG"
    ;;
  *)
    # treat $cmd as scenario id
    local_path="$SCENARIOS_DIR_LOCAL/$cmd.md"
    [[ -f "$local_path" ]] || fatal "no such scenario: $cmd (looked at $local_path)"
    log "Running behavioral scenario '$cmd' on $REMOTE_HOST"
    ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" \
      "zcp eval behavioral run --scenarios-dir '$SCENARIOS_DIR_REMOTE' --id '$cmd'" \
      2>&1 | tee "$LOCAL_LOG"
    ;;
esac

# Suite ID printed by zcp on stderr as `Running behavioral … (suite=<id>)`.
SUITE_ID=$(grep -oE 'suite=[0-9]+-[0-9]+' "$LOCAL_LOG" | head -1 | cut -d= -f2)
if [[ -z "$SUITE_ID" ]]; then
  fatal "could not extract suite id from output (see above)"
fi

mkdir -p "$RUNS_DIR_LOCAL"
log "Pulling $REMOTE_HOST:$RESULTS_DIR_REMOTE/$SUITE_ID → $RUNS_DIR_LOCAL/"
scp "${SSH_OPTS[@]}" -rq "$REMOTE_HOST:$RESULTS_DIR_REMOTE/$SUITE_ID" "$RUNS_DIR_LOCAL/"

log "Run done. Open:"
for sr in "$RUNS_DIR_LOCAL/$SUITE_ID"/*/self-review.md; do
  [[ -f "$sr" ]] || continue
  printf '  %s\n' "$sr" >&2
done
