#!/usr/bin/env bash
# lib/unified-state.sh - Unified State Management API
# Clean implementation - no legacy aliases
# Note: This is a library - it does NOT set shell options to avoid overriding caller's settings

ZCP_STATE_FILE="${ZCP_TMP_DIR:-/tmp}/zcp_state.json"
ZCP_STATE_PERSISTENT="${ZCP_STATE_DIR:-${SCRIPT_DIR:-$(pwd)}/state}/zcp_state.json"

# =============================================================================
# CORE UTILITIES
# =============================================================================

generate_uuid() {
    if command -v uuidgen &>/dev/null; then
        uuidgen | tr '[:upper:]' '[:lower:]'
    elif [[ -f /proc/sys/kernel/random/uuid ]]; then
        cat /proc/sys/kernel/random/uuid
    elif command -v python3 &>/dev/null; then
        python3 -c 'import uuid; print(uuid.uuid4())'
    else
        echo "$(date +%s)-$$-${RANDOM}${RANDOM}"
    fi
}

atomic_write() {
    local content="$1"
    local file="$2"
    local dir
    dir=$(dirname "$file")
    if ! mkdir -p "$dir" 2>/dev/null; then
        echo "ERROR: Cannot create directory $dir" >&2
        return 1
    fi
    local tmp_file="${file}.tmp.$$"
    if ! echo "$content" > "$tmp_file" 2>/dev/null; then
        echo "ERROR: Cannot write to $file" >&2
        rm -f "$tmp_file"
        return 1
    fi
    if ! mv "$tmp_file" "$file" 2>/dev/null; then
        echo "ERROR: Cannot finalize $file" >&2
        rm -f "$tmp_file"
        return 1
    fi
    return 0
}

# =============================================================================
# UNIFIED STATE OPERATIONS
# =============================================================================

zcp_init() {
    local session_id="${1:-$(generate_uuid)}"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local state
    state=$(jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        '{
            version: "2.0",
            session_id: $sid,
            mode: "full",
            updated_at: $ts,
            workflow: {
                phase: "NONE",
                iteration: 1,
                started_at: $ts,
                bootstrap_complete: false,
                services: {}
            },
            bootstrap: {
                active: false,
                workflow_id: null,
                current_step: 0,
                steps: [],
                services: {}
            },
            evidence: {},
            context: { intent: "", notes: [], last_error: null }
        }')
    atomic_write "$state" "$ZCP_STATE_FILE"
    [[ -d "$(dirname "$ZCP_STATE_PERSISTENT")" ]] && atomic_write "$state" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true
    echo "$session_id"
}

zcp_state() {
    if [[ -f "$ZCP_STATE_FILE" ]]; then
        cat "$ZCP_STATE_FILE"
    elif [[ -f "$ZCP_STATE_PERSISTENT" ]]; then
        cat "$ZCP_STATE_PERSISTENT"
    else
        echo '{}'
    fi
}

zcp_get() {
    local path="$1"
    zcp_state | jq -r "$path // empty" 2>/dev/null
}

zcp_set() {
    local path="$1"
    local value="$2"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local state
    state=$(zcp_state)
    state=$(echo "$state" | jq --arg ts "$timestamp" "$path = $value | .updated_at = \$ts")
    atomic_write "$state" "$ZCP_STATE_FILE"
    [[ -d "$(dirname "$ZCP_STATE_PERSISTENT")" ]] && atomic_write "$state" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true
}

zcp_exists() { [[ -f "$ZCP_STATE_FILE" ]] || [[ -f "$ZCP_STATE_PERSISTENT" ]]; }
zcp_clear() { rm -f "$ZCP_STATE_FILE" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true; }

# =============================================================================
# SESSION/MODE/PHASE (overrides utils.sh versions with unified state backing)
# =============================================================================

get_session() { zcp_get '.session_id'; }

get_mode() {
    local mode
    mode=$(zcp_get '.mode')
    if [[ -z "$mode" ]]; then
        [[ -f "${ZCP_TMP_DIR:-/tmp}/claude_mode" ]] && cat "${ZCP_TMP_DIR:-/tmp}/claude_mode" || echo "full"
    else
        echo "$mode"
    fi
}

set_mode() {
    local mode="$1"
    case "$mode" in
        quick|dev-only|full|hotfix|bootstrap) ;;
        *) echo "ERROR: Invalid mode: $mode" >&2; return 1 ;;
    esac
    zcp_set '.mode' "\"$mode\""
    echo "$mode" > "${ZCP_TMP_DIR:-/tmp}/claude_mode"
}

get_phase() {
    local phase
    phase=$(zcp_get '.workflow.phase')
    if [[ -z "$phase" ]] || [[ "$phase" == "null" ]]; then
        [[ -f "${ZCP_TMP_DIR:-/tmp}/claude_phase" ]] && cat "${ZCP_TMP_DIR:-/tmp}/claude_phase" || echo "NONE"
    else
        echo "$phase"
    fi
}

set_phase() {
    local phase="$1"
    case "$phase" in
        NONE|INIT|DISCOVER|DEVELOP|DEPLOY|VERIFY|DONE|QUICK) ;;
        *) echo "ERROR: Invalid phase: $phase" >&2; return 1 ;;
    esac
    zcp_set '.workflow.phase' "\"$phase\""
    echo "$phase" > "${ZCP_TMP_DIR:-/tmp}/claude_phase"
}

get_iteration() {
    local iter
    iter=$(zcp_get '.workflow.iteration')
    [[ -z "$iter" ]] || [[ "$iter" == "null" ]] && echo "1" || echo "$iter"
}

set_iteration() {
    local n="$1"
    [[ ! "$n" =~ ^[0-9]+$ ]] || [[ "$n" -lt 1 ]] && { echo "ERROR: Invalid iteration: $n" >&2; return 1; }
    zcp_set '.workflow.iteration' "$n"
}

check_bootstrap_mode() {
    local completed
    completed=$(zcp_get '.workflow.bootstrap_complete')
    if [[ "$completed" == "true" ]]; then
        echo "true"
    else
        [[ -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json" ]] && echo "true" || echo "false"
    fi
}

# =============================================================================
# BOOTSTRAP STATE MANAGEMENT
# =============================================================================

init_bootstrap() {
    local plan_data="$1"
    local steps_csv="${2:-plan,recipe-search,generate-import,import-services,wait-services,mount-dev,discover-services,finalize,spawn-subagents,aggregate-results}"
    local workflow_id session_id timestamp
    workflow_id=$(generate_uuid)
    session_id=$(zcp_get '.session_id')
    [[ -z "$session_id" ]] || [[ "$session_id" == "null" ]] && session_id=$(zcp_init)
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local steps_json="[]"
    local index=1
    IFS=',' read -ra step_names <<< "$steps_csv"
    for name in "${step_names[@]}"; do
        steps_json=$(echo "$steps_json" | jq --arg name "$name" --argjson idx "$index" '. + [{name: $name, index: $idx, status: "pending", data: null}]')
        ((index++))
    done
    local state
    state=$(zcp_state)
    state=$(echo "$state" | jq \
        --arg wid "$workflow_id" \
        --arg ts "$timestamp" \
        --argjson plan "$plan_data" \
        --argjson steps "$steps_json" \
        '.mode = "bootstrap" | .bootstrap.active = true | .bootstrap.workflow_id = $wid | .bootstrap.started_at = $ts | .bootstrap.current_step = 1 | .bootstrap.plan = $plan | .bootstrap.steps = $steps | .bootstrap.services = {} | .updated_at = $ts')
    atomic_write "$state" "$ZCP_STATE_FILE"
    [[ -d "$(dirname "$ZCP_STATE_PERSISTENT")" ]] && atomic_write "$state" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true
}

get_state() { zcp_get '.bootstrap'; }
bootstrap_active() { [[ "$(zcp_get '.bootstrap.active')" == "true" ]]; }
get_plan() { zcp_get '.bootstrap.plan'; }
get_step() { local name="$1"; zcp_state | jq --arg n "$name" '.bootstrap.steps[] | select(.name == $n)'; }
get_step_data() { local name="$1"; zcp_state | jq --arg n "$name" '(.bootstrap.steps[] | select(.name == $n) | .data) // {}'; }
is_step_complete() { local name="$1"; [[ "$(zcp_state | jq -r --arg n "$name" '(.bootstrap.steps[] | select(.name == $n) | .status) // "pending"')" == "complete" ]]; }
get_next_step() { zcp_state | jq -r '(.bootstrap.steps[] | select(.status != "complete") | .name) // empty' | head -1; }
get_current_step_index() { zcp_get '.bootstrap.current_step'; }

get_step_index() {
    local name="$1"
    zcp_state | jq -r --arg n "$name" '(.bootstrap.steps | to_entries | .[] | select(.value.name == $n) | .key) // -1'
}

get_step_by_index() {
    local idx="$1"
    zcp_state | jq -r --argjson i "$idx" '(.bootstrap.steps[$i].name) // empty'
}

set_step_status() {
    local name="$1"
    local status="$2"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local ts_field
    case "$status" in
        in_progress) ts_field="started_at" ;;
        complete) ts_field="completed_at" ;;
        failed) ts_field="failed_at" ;;
        *) ts_field="" ;;
    esac
    local state
    state=$(zcp_state)
    if [[ -n "$ts_field" ]]; then
        state=$(echo "$state" | jq --arg n "$name" --arg s "$status" --arg ts "$timestamp" --arg tf "$ts_field" '.bootstrap.steps |= map(if .name == $n then .status = $s | .[$tf] = $ts else . end) | .updated_at = $ts')
    else
        state=$(echo "$state" | jq --arg n "$name" --arg s "$status" --arg ts "$timestamp" '.bootstrap.steps |= map(if .name == $n then .status = $s else . end) | .updated_at = $ts')
    fi
    local new_index
    new_index=$(echo "$state" | jq --arg n "$name" '.bootstrap.steps | to_entries | map(select(.value.name == $n)) | .[0].key + 1')
    state=$(echo "$state" | jq --argjson idx "$new_index" '.bootstrap.current_step = $idx')
    atomic_write "$state" "$ZCP_STATE_FILE"
    [[ -d "$(dirname "$ZCP_STATE_PERSISTENT")" ]] && atomic_write "$state" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true
}

set_step_data() {
    local name="$1"
    local data="$2"
    echo "$data" | jq -e . >/dev/null 2>&1 || data='{}'
    local state
    state=$(zcp_state)
    state=$(echo "$state" | jq --arg n "$name" --argjson d "$data" '.bootstrap.steps |= map(if .name == $n then .data = $d else . end)')
    atomic_write "$state" "$ZCP_STATE_FILE"
}

complete_step() {
    local name="$1"
    local data="${2:-"{}"}"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    echo "$data" | jq -e . >/dev/null 2>&1 || data='{}'
    local state
    state=$(zcp_state)
    state=$(echo "$state" | jq --arg n "$name" --arg ts "$timestamp" --argjson d "$data" '.bootstrap.steps |= map(if .name == $n then .status = "complete" | .completed_at = $ts | .data = $d else . end) | .updated_at = $ts')
    atomic_write "$state" "$ZCP_STATE_FILE"
    [[ -d "$(dirname "$ZCP_STATE_PERSISTENT")" ]] && atomic_write "$state" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true
}

update_plan() {
    local plan_update="$1"
    local state
    state=$(zcp_state)
    state=$(echo "$state" | jq --argjson u "$plan_update" '.bootstrap.plan = (.bootstrap.plan + $u)')
    atomic_write "$state" "$ZCP_STATE_FILE"
}

# =============================================================================
# SERVICE STATE (unified + file-based for subagent isolation)
# =============================================================================

set_service_status() {
    local hostname="$1"
    local status="$2"
    local data="${3:-"{}"}"
    echo "$data" | jq -e . >/dev/null 2>&1 || data='{}'
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local state
    state=$(zcp_state)
    state=$(echo "$state" | jq --arg h "$hostname" --arg s "$status" --arg ts "$timestamp" --argjson d "$data" '.bootstrap.services[$h] = {status: $s, updated_at: $ts, data: $d} | .updated_at = $ts')
    atomic_write "$state" "$ZCP_STATE_FILE"
}

get_service_status() { local hostname="$1"; zcp_state | jq --arg h "$hostname" '.bootstrap.services[$h] // {status: "unknown"}'; }
get_all_services() { zcp_get '.bootstrap.services'; }

# File-based per-service state for subagent isolation (used by mark-complete.sh)
set_service_state() {
    local hostname="$1"
    local key="$2"
    local value="$3"
    [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]] && { echo "ERROR: Invalid hostname: $hostname" >&2; return 1; }
    local service_dir="${ZCP_STATE_DIR:-${SCRIPT_DIR:-$(pwd)}/state}/bootstrap/services/${hostname}"
    mkdir -p "$service_dir" 2>/dev/null || true
    local status_file="${service_dir}/status.json"
    local status='{}'
    [[ -f "$status_file" ]] && status=$(cat "$status_file")
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    status=$(echo "$status" | jq --arg key "$key" --arg val "$value" --arg ts "$timestamp" '.[$key] = $val | .last_update = $ts')
    atomic_write "$status" "$status_file"
}

get_service_state() {
    local hostname="$1"
    [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]] && { echo '{}'; return 1; }
    local status_file="${ZCP_STATE_DIR:-${SCRIPT_DIR:-$(pwd)}/state}/bootstrap/services/${hostname}/status.json"
    [[ -f "$status_file" ]] && cat "$status_file" || echo '{}'
}

# =============================================================================
# BOOTSTRAP LIFECYCLE
# =============================================================================

clear_bootstrap() {
    local state
    state=$(zcp_state)
    state=$(echo "$state" | jq '.bootstrap.active = false | .bootstrap.workflow_id = null | .bootstrap.current_step = 0 | .bootstrap.steps = [] | .bootstrap.services = {}')
    atomic_write "$state" "$ZCP_STATE_FILE"
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json" "${ZCP_TMP_DIR:-/tmp}/bootstrap_plan.json" "${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml" "${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json" 2>/dev/null || true
}

complete_bootstrap() {
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local services
    services=$(zcp_get '.bootstrap.services')
    [[ -z "$services" ]] && services='{}'
    local state
    state=$(zcp_state)
    state=$(echo "$state" | jq --arg ts "$timestamp" --argjson svc "$services" '.bootstrap.active = false | .bootstrap.completed_at = $ts | .workflow.bootstrap_complete = true | .workflow.services = $svc | .mode = "full" | .updated_at = $ts')
    atomic_write "$state" "$ZCP_STATE_FILE"
    [[ -d "$(dirname "$ZCP_STATE_PERSISTENT")" ]] && atomic_write "$state" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true
    echo '{"status":"complete","completed_at":"'"$timestamp"'"}' > "${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"
}

# =============================================================================
# EXPORTS
# =============================================================================

export ZCP_STATE_FILE ZCP_STATE_PERSISTENT
export -f generate_uuid atomic_write zcp_init zcp_state zcp_get zcp_set zcp_exists zcp_clear
export -f get_session get_mode set_mode get_phase set_phase get_iteration set_iteration check_bootstrap_mode
export -f init_bootstrap get_state bootstrap_active get_plan get_step get_step_data is_step_complete get_next_step get_current_step_index
export -f get_step_index get_step_by_index set_step_status set_step_data complete_step update_plan
export -f set_service_status get_service_status get_all_services set_service_state get_service_state
export -f clear_bootstrap complete_bootstrap
