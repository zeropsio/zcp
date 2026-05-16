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
warn()  { printf 'WARN: %s\n' "$*" >&2; }

# parseRequiredEnvVars extracts `requiredEnvVars:` list-items from a
# scenario's frontmatter. Returns one var name per line. Empty when
# the scenario doesn't declare any.
parseRequiredEnvVars() {
  local file="$1"
  awk '
    /^---[[:space:]]*$/ { fm = !fm; next }
    fm && /^requiredEnvVars:/ { collecting = 1; next }
    fm && collecting && /^[[:space:]]*-[[:space:]]/ { sub(/^[[:space:]]*-[[:space:]]*/, ""); print; next }
    fm && collecting && /^[^[:space:]-]/ { collecting = 0 }
  ' "$file"
}

# tokenSentinelGrep flags token-shaped strings in LLM-authored artifacts
# (self-review.md + retrospective.jsonl) post-pull. The transcript is
# DELIBERATELY EXCLUDED — the persona convention (`Bash echo $ZCP_E2E_*`
# fetch + `zerops_env set` call) necessarily puts the token in the wire
# transcript at the Bash tool result and the env-set call args. Those
# are expected. The leak we guard against is the agent ECHOING the
# token in its own prose (self-review) or the retrospective payload —
# those are LLM-authored, not tool-mediated, and a hit there means the
# agent broke containment.
#
# Patterns:
#   - ghp_<20+>           classic GitHub PAT
#   - github_pat_<20+>    fine-grained GitHub PAT (newer shape; Karel's tokens)
#   - YJQTh.<20+>         Zerops LaunchKey (one known prefix)
#
# Returns 0 (clean) or 1 (hit found).
tokenSentinelGrep() {
  local suite_dir="$1"
  local hit=0
  local pattern='ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|YJQTh\.[A-Za-z0-9._-]{20,}'
  while IFS= read -r f; do
    if grep -E "$pattern" "$f" > /dev/null 2>&1; then
      warn "token-shaped string in $f — review before committing"
      hit=1
    fi
  done < <(find "$suite_dir" -type f \( -name 'self-review.md' -o -name 'retrospective.jsonl' \))
  return $hit
}

cmd="${1:-list}"

case "$cmd" in
  list|"")
    # List runs locally — no zcp roundtrip needed for the scenario index.
    exec go run ./cmd/zcp eval behavioral list --scenarios-dir "$SCENARIOS_DIR_LOCAL"
    ;;
esac

# All non-list paths go to the remote.
# Fail-fast on missing required env vars for the chosen scenario(s) BEFORE
# spending 5+ minutes on a build/scp/ssh cycle. Scenarios declare deps in
# frontmatter as a `requiredEnvVars:` list; this loop checks each is set
# in the operator's shell.
if [[ "$cmd" != "all" ]]; then
  scenario_path="$SCENARIOS_DIR_LOCAL/$cmd.md"
  if [[ -f "$scenario_path" ]]; then
    missing=()
    while IFS= read -r var; do
      [[ -z "$var" ]] && continue
      if [[ -z "${!var:-}" ]]; then
        missing+=("$var")
      fi
    done < <(parseRequiredEnvVars "$scenario_path")
    if (( ${#missing[@]} > 0 )); then
      fatal "scenario '$cmd' requires env vars not set in your shell: ${missing[*]}"
    fi
  fi
fi

log "Building + deploying current zcp binary"
./eval/scripts/build-deploy.sh >&2

log "Syncing scenarios to $REMOTE_HOST:$SCENARIOS_DIR_REMOTE"
ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" \
  "rm -rf '$SCENARIOS_DIR_REMOTE' && mkdir -p '$SCENARIOS_DIR_REMOTE'"
scp "${SSH_OPTS[@]}" -rq "$SCENARIOS_DIR_LOCAL"/. "$REMOTE_HOST:$SCENARIOS_DIR_REMOTE/"

# Propagate ZCP_E2E_* env vars to the container via a temp env-file. Avoids
# SendEnv / AcceptEnv sshd_config dependencies. The file lands at
# /tmp/zcp-e2e-env-$$ on the container, gets sourced before `zcp eval`,
# and is removed (`trap`) regardless of run outcome.
REMOTE_ENV_FILE="/tmp/zcp-e2e-env-$$"
ENV_PREFIX=""
e2e_vars=$(env | grep -E '^ZCP_E2E_' || true)
if [[ -n "$e2e_vars" ]]; then
  log "Propagating ZCP_E2E_* env vars to $REMOTE_HOST (env-file)"
  LOCAL_ENV_FILE=$(mktemp)
  # Write KEY=VALUE pairs verbatim — no shell-quoting because the file is
  # sourced (not exec'd) and values are tokens (no embedded $, ", or
  # newlines in current usage). If future tokens carry shell metacharacters,
  # add per-line `printf 'export %s=%q\n'` here.
  printf '%s\n' "$e2e_vars" > "$LOCAL_ENV_FILE"
  scp "${SSH_OPTS[@]}" -q "$LOCAL_ENV_FILE" "$REMOTE_HOST:$REMOTE_ENV_FILE"
  rm -f "$LOCAL_ENV_FILE"
  ENV_PREFIX="set -a && . '$REMOTE_ENV_FILE' && set +a && "
  trap "ssh ${SSH_OPTS[*]} '$REMOTE_HOST' 'rm -f $REMOTE_ENV_FILE' 2>/dev/null || true" EXIT
fi

# Capture suite ID from the run so we can scp just that suite back.
LOCAL_LOG=$(mktemp)
trap "rm -f $LOCAL_LOG; ssh ${SSH_OPTS[*]} '$REMOTE_HOST' 'rm -f $REMOTE_ENV_FILE' 2>/dev/null || true" EXIT

case "$cmd" in
  all)
    log "Running ALL behavioral scenarios on $REMOTE_HOST"
    ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" \
      "${ENV_PREFIX}zcp eval behavioral all --scenarios-dir '$SCENARIOS_DIR_REMOTE'" \
      2>&1 | tee "$LOCAL_LOG"
    ;;
  *)
    # treat $cmd as scenario id
    local_path="$SCENARIOS_DIR_LOCAL/$cmd.md"
    [[ -f "$local_path" ]] || fatal "no such scenario: $cmd (looked at $local_path)"
    log "Running behavioral scenario '$cmd' on $REMOTE_HOST"
    ssh "${SSH_OPTS[@]}" "$REMOTE_HOST" \
      "${ENV_PREFIX}zcp eval behavioral run --scenarios-dir '$SCENARIOS_DIR_REMOTE' --id '$cmd'" \
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

# Defense-in-depth: scan pulled artifacts for token-shaped strings (operator
# personas should reference $ENV_VAR by name, never embed literals; this
# catches accidental leaks before the operator commits the suite).
if ! tokenSentinelGrep "$RUNS_DIR_LOCAL/$SUITE_ID"; then
  warn "review token-shaped findings above before committing the suite"
fi

log "Run done. Open:"
for sr in "$RUNS_DIR_LOCAL/$SUITE_ID"/*/self-review.md; do
  [[ -f "$sr" ]] || continue
  printf '  %s\n' "$sr" >&2
done
