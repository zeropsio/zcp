#!/usr/bin/env bash
# .zcp/lib/bootstrap/state.sh
# Bootstrap state management - UNIFIED STATE API
# Single source of truth: /tmp/bootstrap_state.json
#
# This replaces the dual state system with a single unified state file.

set -euo pipefail

# Single state file - temp directory for reliability (always writable, no SSHFS issues)
STATE_FILE="${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"

# =============================================================================
# UNIFIED STATE API
# =============================================================================

# Generate UUID with fallbacks
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

# Atomic write to file
atomic_write() {
    local content="$1"
    local file="$2"
    local dir
    dir=$(dirname "$file")
    mkdir -p "$dir" 2>/dev/null || return 1
    local tmp_file="${file}.tmp.$$"
    echo "$content" > "$tmp_file" && mv "$tmp_file" "$file"
}

# -----------------------------------------------------------------------------
# BOOTSTRAP LIFECYCLE
# -----------------------------------------------------------------------------

# Initialize bootstrap with plan and step definitions
# Usage: init_bootstrap "$plan_json" "step1,step2,step3,..."
init_bootstrap() {
    local plan_data="$1"
    local steps_csv="${2:-plan,recipe-search,generate-import,import-services,wait-services,mount-dev,discover-services,finalize,spawn-subagents,aggregate-results}"

    local workflow_id session_id timestamp
    workflow_id=$(generate_uuid)
    session_id=$(generate_uuid)
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    # Build steps array
    local steps_json="[]"
    local index=1
    IFS=',' read -ra step_names <<< "$steps_csv"
    for name in "${step_names[@]}"; do
        steps_json=$(echo "$steps_json" | jq \
            --arg name "$name" \
            --argjson idx "$index" \
            '. + [{name: $name, index: $idx, status: "pending", data: null}]')
        ((index++))
    done

    local state
    state=$(jq -n \
        --arg wid "$workflow_id" \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --argjson plan "$plan_data" \
        --argjson steps "$steps_json" \
        '{
            workflow_id: $wid,
            session_id: $sid,
            started_at: $ts,
            current_step: 1,
            plan: $plan,
            steps: $steps,
            services: {}
        }')

    atomic_write "$state" "$STATE_FILE"
}

# Clear all bootstrap state
clear_bootstrap() {
    rm -f "$STATE_FILE" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_plan.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/recipe_review.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/service_discovery.json" 2>/dev/null || true
    # Clean up per-service completion files
    rm -f "${ZCP_TMP_DIR:-/tmp}"/*_complete.json 2>/dev/null || true
}

# -----------------------------------------------------------------------------
# STATE READERS
# -----------------------------------------------------------------------------

# Get entire state object
get_state() {
    if [[ -f "$STATE_FILE" ]]; then
        cat "$STATE_FILE"
    else
        echo '{}'
    fi
}

# Check if bootstrap is active
bootstrap_active() {
    [[ -f "$STATE_FILE" ]]
}

# Get plan from state
get_plan() {
    get_state | jq '.plan // {}'
}

# Get step object by name
get_step() {
    local name="$1"
    get_state | jq --arg n "$name" '.steps[] | select(.name == $n)'
}

# Get step data by name
get_step_data() {
    local name="$1"
    get_state | jq --arg n "$name" '(.steps[] | select(.name == $n) | .data) // {}'
}

# Check if step is complete
is_step_complete() {
    local name="$1"
    local status
    status=$(get_state | jq -r --arg n "$name" '(.steps[] | select(.name == $n) | .status) // "pending"')
    [[ "$status" == "complete" ]]
}

# Get next pending step name
get_next_step() {
    get_state | jq -r '(.steps[] | select(.status != "complete") | .name) // empty' | head -1
}

# Get current step index
get_current_step_index() {
    get_state | jq -r '.current_step // 1'
}

# -----------------------------------------------------------------------------
# STATE WRITERS
# -----------------------------------------------------------------------------

# Set step status (pending, in_progress, complete, failed)
set_step_status() {
    local name="$1"
    local status="$2"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    local state
    state=$(get_state)

    local ts_field
    case "$status" in
        in_progress) ts_field="started_at" ;;
        complete) ts_field="completed_at" ;;
        failed) ts_field="failed_at" ;;
        *) ts_field="" ;;
    esac

    if [[ -n "$ts_field" ]]; then
        state=$(echo "$state" | jq \
            --arg n "$name" \
            --arg s "$status" \
            --arg ts "$timestamp" \
            --arg tf "$ts_field" \
            '.steps |= map(if .name == $n then .status = $s | .[$tf] = $ts else . end)')
    else
        state=$(echo "$state" | jq \
            --arg n "$name" \
            --arg s "$status" \
            '.steps |= map(if .name == $n then .status = $s else . end)')
    fi

    # Update current_step pointer
    local new_index
    new_index=$(echo "$state" | jq --arg n "$name" '.steps | to_entries | map(select(.value.name == $n)) | .[0].key + 1')
    state=$(echo "$state" | jq --argjson idx "$new_index" '.current_step = $idx')

    atomic_write "$state" "$STATE_FILE"
}

# Set step data
set_step_data() {
    local name="$1"
    local data="$2"

    # Validate JSON
    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    local state
    state=$(get_state)
    state=$(echo "$state" | jq \
        --arg n "$name" \
        --argjson d "$data" \
        '.steps |= map(if .name == $n then .data = $d else . end)')

    atomic_write "$state" "$STATE_FILE"
}

# Combined: set status and data atomically
complete_step() {
    local name="$1"
    local data="${2:-"{}"}"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    local state
    state=$(get_state)
    state=$(echo "$state" | jq \
        --arg n "$name" \
        --arg ts "$timestamp" \
        --argjson d "$data" \
        '.steps |= map(if .name == $n then .status = "complete" | .completed_at = $ts | .data = $d else . end)')

    atomic_write "$state" "$STATE_FILE"
}

# Update plan
update_plan() {
    local plan_update="$1"
    local state
    state=$(get_state)
    state=$(echo "$state" | jq --argjson u "$plan_update" '.plan = (.plan + $u)')
    atomic_write "$state" "$STATE_FILE"
}

# -----------------------------------------------------------------------------
# SERVICE STATE (for subagents)
# -----------------------------------------------------------------------------

set_service_status() {
    local hostname="$1"
    local status="$2"
    local data="${3:-"{}"}"

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    local state
    state=$(get_state)
    state=$(echo "$state" | jq \
        --arg h "$hostname" \
        --arg s "$status" \
        --arg ts "$timestamp" \
        --argjson d "$data" \
        '.services[$h] = {status: $s, updated_at: $ts, data: $d}')

    atomic_write "$state" "$STATE_FILE"
}

get_service_status() {
    local hostname="$1"
    get_state | jq --arg h "$hostname" '.services[$h] // {status: "unknown"}'
}

get_all_services() {
    get_state | jq '.services // {}'
}

# -----------------------------------------------------------------------------
# LEGACY COMPATIBILITY LAYER
# -----------------------------------------------------------------------------
# These functions map old API names to new ones for backward compatibility
# with existing step scripts that haven't been updated yet.

# Legacy: init_state -> calls init_bootstrap
init_state() {
    local plan_data="$1"
    local session_id="${2:-}"
    init_bootstrap "$plan_data"
}

# Legacy: record_step -> calls complete_step (orchestrator handles this now)
record_step() {
    local step="$1"
    local status="$2"
    local data="${3:-"{}"}"

    if [[ "$status" == "complete" ]]; then
        complete_step "$step" "$data"
    else
        set_step_status "$step" "$status"
        set_step_data "$step" "$data"
    fi
}

# Legacy: get_bootstrap_state -> calls get_state
get_bootstrap_state() {
    get_state
}

# Legacy: workflow_exists -> calls bootstrap_active
workflow_exists() {
    bootstrap_active
}

# Legacy: update_step_status -> calls set_step_status
update_step_status() {
    set_step_status "$1" "$2"
}

# Legacy: clear_bootstrap_state -> calls clear_bootstrap
clear_bootstrap_state() {
    clear_bootstrap
}

# Legacy: get/set_checkpoint - derived from step status
get_checkpoint() {
    local state
    state=$(get_state)
    # Return the name of the last completed step
    echo "$state" | jq -r '(.steps | map(select(.status == "complete")) | .[-1].name) // "none"'
}

set_checkpoint() {
    # No-op in unified system - checkpoint is implicit from step status
    :
}

# Legacy: per-service state functions for subagents
# These use a separate directory structure for backward compatibility

set_service_state() {
    local hostname="$1"
    local key="$2"
    local value="$3"

    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "ERROR: Invalid hostname format: $hostname" >&2
        return 1
    fi

    local service_dir="${ZCP_STATE_DIR:-/tmp}/bootstrap/services/${hostname}"
    mkdir -p "$service_dir" 2>/dev/null || true

    local status_file="${service_dir}/status.json"
    local status
    if [[ -f "$status_file" ]]; then
        status=$(cat "$status_file")
    else
        status='{}'
    fi

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    status=$(echo "$status" | jq \
        --arg key "$key" \
        --arg val "$value" \
        --arg ts "$timestamp" \
        '.[$key] = $val | .last_update = $ts')

    atomic_write "$status" "$status_file"
}

get_service_state() {
    local hostname="$1"

    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo '{}'
        return 1
    fi

    local status_file="${ZCP_STATE_DIR:-/tmp}/bootstrap/services/${hostname}/status.json"

    if [[ -f "$status_file" ]]; then
        cat "$status_file"
    else
        echo '{}'
    fi
}

set_service_result() {
    local hostname="$1"
    local result="$2"

    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        return 1
    fi

    local service_dir="${ZCP_STATE_DIR:-/tmp}/bootstrap/services/${hostname}"
    mkdir -p "$service_dir" 2>/dev/null || true

    atomic_write "$result" "${service_dir}/result.json"
}

get_all_services_status() {
    local services_dir="${ZCP_STATE_DIR:-/tmp}/bootstrap/services"

    if [[ ! -d "$services_dir" ]]; then
        echo '{}'
        return
    fi

    local result='{}'

    for service_dir in "$services_dir"/*/; do
        if [[ -d "$service_dir" ]]; then
            local hostname
            hostname=$(basename "$service_dir")

            if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
                continue
            fi

            local status
            status=$(get_service_state "$hostname")

            result=$(echo "$result" | jq --arg h "$hostname" --argjson s "$status" '. + {($h): $s}')
        fi
    done

    echo "$result"
}

# -----------------------------------------------------------------------------
# STATUS DISPLAY
# -----------------------------------------------------------------------------

generate_status_json() {
    local state
    state=$(get_state)

    if [[ "$state" == '{}' ]]; then
        jq -n '{active: false, message: "No bootstrap in progress"}'
        return
    fi

    echo "$state" | jq '{
        active: true,
        workflow_id: .workflow_id,
        current_step: .current_step,
        total_steps: (.steps | length),
        steps_complete: ([.steps[] | select(.status == "complete")] | length),
        started_at: .started_at,
        plan: .plan,
        services: .services
    }'
}

# -----------------------------------------------------------------------------
# EXPORTS
# -----------------------------------------------------------------------------

export STATE_FILE
export -f generate_uuid atomic_write
export -f init_bootstrap clear_bootstrap
export -f get_state bootstrap_active get_plan get_step get_step_data
export -f is_step_complete get_next_step get_current_step_index
export -f set_step_status set_step_data complete_step update_plan
export -f set_service_status get_service_status get_all_services
export -f generate_status_json

# Legacy exports for backward compatibility
export -f init_state record_step get_bootstrap_state workflow_exists
export -f update_step_status clear_bootstrap_state get_checkpoint set_checkpoint
export -f set_service_state get_service_state set_service_result get_all_services_status
