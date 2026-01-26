#!/usr/bin/env bash
# .zcp/lib/bootstrap/state.sh
# Bootstrap state and checkpoint management
# Handles atomic writes, state persistence, and per-service state

set -euo pipefail

# State file paths (derived from utils.sh patterns)
BOOTSTRAP_STATE_DIR="${STATE_DIR:-${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/..}/state}/bootstrap"
BOOTSTRAP_STATE_FILE="${BOOTSTRAP_STATE_DIR}/state.json"

# Initialize bootstrap state directory
init_bootstrap_state() {
    mkdir -p "$BOOTSTRAP_STATE_DIR/services" 2>/dev/null || true
}

# Atomic write to file - write to temp then move
# Usage: atomic_write "content" "/path/to/file"
atomic_write() {
    local content="$1"
    local file="$2"

    local dir
    dir=$(dirname "$file")

    # M-18: Add error handling for directory creation
    if ! mkdir -p "$dir" 2>/dev/null; then
        echo "ERROR: Cannot create directory: $dir" >&2
        return 1
    fi

    local tmp_file="${file}.tmp.$$"

    # M-18: Check write operation
    if ! echo "$content" > "$tmp_file"; then
        echo "ERROR: Cannot write to temp file: $tmp_file" >&2
        rm -f "$tmp_file" 2>/dev/null
        return 1
    fi

    # M-18: Check move operation
    if ! mv "$tmp_file" "$file"; then
        echo "ERROR: Cannot move temp file to: $file" >&2
        rm -f "$tmp_file" 2>/dev/null
        return 1
    fi

    return 0
}

# Read bootstrap state file
# Returns: JSON state object or empty object if not exists
get_bootstrap_state() {
    if [ -f "$BOOTSTRAP_STATE_FILE" ]; then
        cat "$BOOTSTRAP_STATE_FILE"
    else
        echo '{}'
    fi
}

# Initialize new bootstrap state
# Usage: init_state '{"plan": {...}}'
init_state() {
    local plan_data="$1"
    local session_id="${2:-$(generate_secure_session_id 2>/dev/null || echo "$(date +%Y%m%d%H%M%S)-$$-$RANDOM")}"

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

    # Also write to temp for compatibility
    atomic_write "$state" "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
}

# Get current checkpoint
get_checkpoint() {
    get_bootstrap_state | jq -r '.checkpoint // "none"'
}

# Set checkpoint
# Usage: set_checkpoint "recipe-search"
set_checkpoint() {
    local checkpoint="$1"

    local state
    state=$(get_bootstrap_state)
    state=$(echo "$state" | jq --arg cp "$checkpoint" '.checkpoint = $cp')

    atomic_write "$state" "$BOOTSTRAP_STATE_FILE"
    atomic_write "$state" "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
}

# Record step completion
# Usage: record_step "plan" "complete" '{"data": "here"}'
record_step() {
    local step="$1"
    local status="$2"
    local data="${3:-"{}"}"

    # Validate data is valid JSON
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

    # Update checkpoint if step completed successfully
    if [ "$status" = "complete" ]; then
        state=$(echo "$state" | jq --arg cp "$step" '.checkpoint = $cp')
    fi

    atomic_write "$state" "$BOOTSTRAP_STATE_FILE"
    atomic_write "$state" "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
}

# Check if step is complete
# Usage: is_step_complete "recipe-search" && echo "yes"
is_step_complete() {
    local step="$1"
    local status
    status=$(get_bootstrap_state | jq -r --arg s "$step" '.steps[$s].status // ""')
    [ "$status" = "complete" ]
}

# Get step data
# Usage: get_step_data "plan"
get_step_data() {
    local step="$1"
    get_bootstrap_state | jq --arg s "$step" '.steps[$s].data // {}'
}

# Get plan from state
get_plan() {
    get_bootstrap_state | jq '.plan // {}'
}

# Update plan in state
# Usage: update_plan '{"key": "value"}'
update_plan() {
    local plan_update="$1"

    local state
    state=$(get_bootstrap_state)
    state=$(echo "$state" | jq --argjson update "$plan_update" '.plan = (.plan + $update)')

    atomic_write "$state" "$BOOTSTRAP_STATE_FILE"
    atomic_write "$state" "${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
}

# Per-service state management (for subagents)
# Usage: set_service_state "apidev" "phase" "deploying"
set_service_state() {
    local hostname="$1"
    local key="$2"
    local value="$3"

    # CRITICAL-7: Validate hostname to prevent path traversal
    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "ERROR: Invalid hostname format: $hostname" >&2
        return 1
    fi

    local service_dir="${BOOTSTRAP_STATE_DIR}/services/${hostname}"
    mkdir -p "$service_dir" 2>/dev/null || true

    local status_file="${service_dir}/status.json"
    local status
    if [ -f "$status_file" ]; then
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

# Get per-service state
# Usage: get_service_state "apidev"
get_service_state() {
    local hostname="$1"

    # CRITICAL-7: Validate hostname to prevent path traversal
    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "ERROR: Invalid hostname format: $hostname" >&2
        echo '{}'
        return 1
    fi

    local status_file="${BOOTSTRAP_STATE_DIR}/services/${hostname}/status.json"

    if [ -f "$status_file" ]; then
        cat "$status_file"
    else
        echo '{}'
    fi
}

# Set service result (final outcome)
# Usage: set_service_result "apidev" '{"success": true, "files": [...]}'
set_service_result() {
    local hostname="$1"
    local result="$2"

    # CRITICAL-7: Validate hostname to prevent path traversal
    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "ERROR: Invalid hostname format: $hostname" >&2
        return 1
    fi

    local service_dir="${BOOTSTRAP_STATE_DIR}/services/${hostname}"
    mkdir -p "$service_dir" 2>/dev/null || true

    atomic_write "$result" "${service_dir}/result.json"
}

# Get all services status (aggregate for orchestrator)
# CRITICAL-6: Use jq for safe JSON construction to prevent injection
get_all_services_status() {
    local services_dir="${BOOTSTRAP_STATE_DIR}/services"

    if [ ! -d "$services_dir" ]; then
        echo '{}'
        return
    fi

    local result='{}'

    for service_dir in "$services_dir"/*/; do
        if [ -d "$service_dir" ]; then
            local hostname
            hostname=$(basename "$service_dir")

            # Validate hostname before using it
            if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
                continue
            fi

            local status
            status=$(get_service_state "$hostname")

            # Use jq for safe JSON construction
            result=$(echo "$result" | jq --arg h "$hostname" --argjson s "$status" '. + {($h): $s}')
        fi
    done

    echo "$result"
}

# Clear bootstrap state (for reset)
clear_bootstrap_state() {
    rm -rf "$BOOTSTRAP_STATE_DIR" 2>/dev/null || true
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

    if [ "$state" = '{}' ]; then
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

    local total_steps=7  # plan, recipe-search, generate-import, import-services, wait-services, mount-dev, finalize

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
export -f init_bootstrap_state atomic_write get_bootstrap_state init_state
export -f get_checkpoint set_checkpoint record_step is_step_complete get_step_data
export -f get_plan update_plan
export -f set_service_state get_service_state set_service_result get_all_services_status
export -f clear_bootstrap_state generate_status_json
