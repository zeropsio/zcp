#!/usr/bin/env bash
# .zcp/lib/bootstrap/state.sh
# Bootstrap state and checkpoint management
# Handles atomic writes, state persistence, and per-service state
#
# Approach E: Hybrid Chain + Recovery workflow state management

set -euo pipefail

# State file paths
BOOTSTRAP_STATE_DIR="${STATE_DIR:-${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../..}/state}/bootstrap"
BOOTSTRAP_STATE_FILE="${BOOTSTRAP_STATE_DIR}/state.json"

# NEW: Workflow state file for Approach E
WORKFLOW_STATE_FILE="${ZCP_STATE_DIR:-${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../..}/state}/workflow.json"

# Initialize bootstrap state directory
init_bootstrap_state() {
    mkdir -p "$BOOTSTRAP_STATE_DIR/services" 2>/dev/null || true
    mkdir -p "$(dirname "$WORKFLOW_STATE_FILE")" 2>/dev/null || true
}

# Atomic write to file - write to temp then move
atomic_write() {
    local content="$1"
    local file="$2"

    local dir
    dir=$(dirname "$file")

    if ! mkdir -p "$dir" 2>/dev/null; then
        echo "ERROR: Cannot create directory: $dir" >&2
        return 1
    fi

    local tmp_file="${file}.tmp.$$"

    if ! echo "$content" > "$tmp_file"; then
        echo "ERROR: Cannot write to temp file: $tmp_file" >&2
        rm -f "$tmp_file" 2>/dev/null
        return 1
    fi

    if ! mv "$tmp_file" "$file"; then
        echo "ERROR: Cannot move temp file to: $file" >&2
        rm -f "$tmp_file" 2>/dev/null
        return 1
    fi

    return 0
}

# =============================================================================
# APPROACH E: WORKFLOW STATE MANAGEMENT
# =============================================================================

# Generate a UUID with fallbacks for portability
generate_uuid() {
    if command -v uuidgen &>/dev/null; then
        uuidgen | tr '[:upper:]' '[:lower:]'
    elif [[ -f /proc/sys/kernel/random/uuid ]]; then
        cat /proc/sys/kernel/random/uuid
    elif command -v python3 &>/dev/null; then
        python3 -c 'import uuid; print(uuid.uuid4())'
    else
        # Fallback: timestamp + PID + random
        echo "$(date +%s)-$$-${RANDOM}${RANDOM}"
    fi
}

# Check if workflow exists
workflow_exists() {
    [[ -f "$WORKFLOW_STATE_FILE" ]]
}

# Check if step exists in workflow
# Usage: step_exists <step_name>
step_exists() {
    local step_name="$1"

    [[ ! -f "$WORKFLOW_STATE_FILE" ]] && return 1

    jq -e --arg name "$step_name" '.steps[] | select(.name == $name)' "$WORKFLOW_STATE_FILE" > /dev/null 2>&1
}

# Update step status in workflow state
# Usage: update_step_status <step_name> <status>
update_step_status() {
    local step_name="$1"
    local new_status="$2"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    [[ ! -f "$WORKFLOW_STATE_FILE" ]] && return 1

    local tmp_file="${WORKFLOW_STATE_FILE}.tmp.$$"

    case "$new_status" in
        "in_progress")
            jq --arg name "$step_name" --arg ts "$timestamp" '
                .steps |= map(
                    if .name == $name then
                        .status = "in_progress" | .started_at = $ts
                    else . end
                ) |
                .current_step = (.steps | to_entries | map(select(.value.name == $name)) | .[0].key + 1)
            ' "$WORKFLOW_STATE_FILE" > "$tmp_file"
            ;;
        "complete")
            jq --arg name "$step_name" --arg ts "$timestamp" '
                .steps |= map(
                    if .name == $name then
                        .status = "complete" | .completed_at = $ts
                    else . end
                )
            ' "$WORKFLOW_STATE_FILE" > "$tmp_file"
            ;;
        "failed")
            jq --arg name "$step_name" --arg ts "$timestamp" '
                .steps |= map(
                    if .name == $name then
                        .status = "failed" | .failed_at = $ts
                    else . end
                )
            ' "$WORKFLOW_STATE_FILE" > "$tmp_file"
            ;;
        *)
            echo "ERROR: Unknown status: $new_status" >&2
            return 1
            ;;
    esac

    mv "$tmp_file" "$WORKFLOW_STATE_FILE"
}

# Reset workflow state
reset_workflow() {
    rm -f "$WORKFLOW_STATE_FILE" 2>/dev/null || true
}

# =============================================================================
# LEGACY STATE MANAGEMENT (backward compatibility)
# =============================================================================

# Read bootstrap state file
get_bootstrap_state() {
    if [[ -f "$BOOTSTRAP_STATE_FILE" ]]; then
        cat "$BOOTSTRAP_STATE_FILE"
    else
        echo '{}'
    fi
}

# Initialize new bootstrap state
init_state() {
    local plan_data="$1"
    local session_id="${2:-$(generate_uuid)}"

    init_bootstrap_state

    local state
    state=$(jq -n \
        --arg session "$session_id" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        --argjson plan "$plan_data" \
        '{
            session_id: $session,
            started_at: $ts,
            checkpoint: "plan",
            plan: $plan,
            steps: {},
            code_generation: null
        }')

    atomic_write "$state" "$BOOTSTRAP_STATE_FILE"
    atomic_write "$state" "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
}

# Get current checkpoint
get_checkpoint() {
    get_bootstrap_state | jq -r '.checkpoint // "none"'
}

# Set checkpoint
set_checkpoint() {
    local checkpoint="$1"

    local state
    state=$(get_bootstrap_state)
    state=$(echo "$state" | jq --arg cp "$checkpoint" '.checkpoint = $cp')

    atomic_write "$state" "$BOOTSTRAP_STATE_FILE"
    atomic_write "$state" "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
}

# Record step completion (legacy format)
record_step() {
    local step="$1"
    local status="$2"
    local data="${3:-"{}"}"

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    local state
    state=$(get_bootstrap_state)

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    state=$(echo "$state" | jq \
        --arg step "$step" \
        --arg status "$status" \
        --arg ts "$timestamp" \
        --argjson data "$data" \
        '.steps[$step] = {status: $status, at: $ts, data: $data}')

    if [[ "$status" == "complete" ]]; then
        state=$(echo "$state" | jq --arg cp "$step" '.checkpoint = $cp')
    fi

    atomic_write "$state" "$BOOTSTRAP_STATE_FILE"
    atomic_write "$state" "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"

    # Also update workflow state if it exists
    if workflow_exists; then
        update_step_status "$step" "$status"
    fi
}

# Check if step is complete (legacy)
is_step_complete() {
    local step="$1"

    # First check workflow state (Approach E)
    if workflow_exists; then
        local status
        status=$(jq -r --arg name "$step" \
            '.steps[] | select(.name == $name) | .status' "$WORKFLOW_STATE_FILE" 2>/dev/null)
        [[ "$status" == "complete" ]] && return 0
    fi

    # Fall back to legacy state
    local status
    status=$(get_bootstrap_state | jq -r --arg s "$step" '.steps[$s].status // ""')
    [[ "$status" == "complete" ]]
}

# Get step data
get_step_data() {
    local step="$1"
    get_bootstrap_state | jq --arg s "$step" '.steps[$s].data // {}'
}

# Get plan from state
get_plan() {
    get_bootstrap_state | jq '.plan // {}'
}

# Update plan in state
update_plan() {
    local plan_update="$1"

    local state
    state=$(get_bootstrap_state)
    state=$(echo "$state" | jq --argjson update "$plan_update" '.plan = (.plan + $update)')

    atomic_write "$state" "$BOOTSTRAP_STATE_FILE"
    atomic_write "$state" "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
}

# =============================================================================
# PER-SERVICE STATE (for subagents)
# =============================================================================

set_service_state() {
    local hostname="$1"
    local key="$2"
    local value="$3"

    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "ERROR: Invalid hostname format: $hostname" >&2
        return 1
    fi

    local service_dir="${BOOTSTRAP_STATE_DIR}/services/${hostname}"
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
        echo "ERROR: Invalid hostname format: $hostname" >&2
        echo '{}'
        return 1
    fi

    local status_file="${BOOTSTRAP_STATE_DIR}/services/${hostname}/status.json"

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
        echo "ERROR: Invalid hostname format: $hostname" >&2
        return 1
    fi

    local service_dir="${BOOTSTRAP_STATE_DIR}/services/${hostname}"
    mkdir -p "$service_dir" 2>/dev/null || true

    atomic_write "$result" "${service_dir}/result.json"
}

get_all_services_status() {
    local services_dir="${BOOTSTRAP_STATE_DIR}/services"

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

# Clear all bootstrap state
clear_bootstrap_state() {
    rm -rf "$BOOTSTRAP_STATE_DIR" 2>/dev/null || true
    rm -f "$WORKFLOW_STATE_FILE" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_plan.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/recipe_review.json" 2>/dev/null || true
}

# Generate status JSON for bootstrap.sh status command
generate_status_json() {
    local state
    state=$(get_bootstrap_state)

    if [[ "$state" == '{}' ]]; then
        jq -n '{
            active: false,
            checkpoint: null,
            can_resume: false,
            message: "No bootstrap in progress"
        }'
        return
    fi

    local checkpoint
    checkpoint=$(echo "$state" | jq -r '.checkpoint // "none"')

    local steps_complete
    steps_complete=$(echo "$state" | jq '[.steps | to_entries[] | select(.value.status == "complete")] | length')

    local total_steps=10

    local services_status
    services_status=$(get_all_services_status)

    echo "$state" | jq \
        --arg cp "$checkpoint" \
        --argjson steps_done "$steps_complete" \
        --argjson total "$total_steps" \
        --argjson services "$services_status" \
        '{
            active: true,
            checkpoint: $cp,
            can_resume: true,
            steps_complete: $steps_done,
            total_steps: $total,
            started_at: .started_at,
            plan: .plan,
            services: $services,
            message: "Bootstrap at checkpoint: \($cp) (\($steps_done)/\($total) steps)"
        }'
}

# Export functions
export -f init_bootstrap_state atomic_write
export -f generate_uuid workflow_exists step_exists update_step_status reset_workflow
export -f get_bootstrap_state init_state get_checkpoint set_checkpoint record_step is_step_complete get_step_data
export -f get_plan update_plan
export -f set_service_state get_service_state set_service_result get_all_services_status
export -f clear_bootstrap_state generate_status_json
