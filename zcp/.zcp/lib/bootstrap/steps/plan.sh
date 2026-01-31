#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/plan.sh
# Step: Create bootstrap plan from user inputs
#
# Inputs: --runtime, --services, --prefix, --ha
# Outputs: Plan JSON with service hostnames and configuration
#
# AUTOMATIC TYPE RESOLUTION:
# - "postgres" → "postgresql"
# - "redis" → "valkey"
# - "node" → "nodejs"
# - Validates against data.json for authoritative type list

# Note: output.sh and state.sh are sourced by bootstrap.sh before this runs

# Source resolve-types for automatic type resolution
RESOLVE_SCRIPT="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/..}/resolve-types.sh"
if [ -f "$RESOLVE_SCRIPT" ]; then
    source "$RESOLVE_SCRIPT"
fi

step_plan() {
    local runtime="" services="" prefix="app" ha_mode="false"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --runtime|--runtimes) runtime="$2"; shift 2 ;;
            --services) services="$2"; shift 2 ;;
            --prefix|--prefixes) prefix="$2"; shift 2 ;;
            --ha) ha_mode="true"; shift ;;
            *) shift ;;
        esac
    done

    # Validate required inputs
    if [ -z "$runtime" ]; then
        json_error "plan" "Missing required --runtime argument" '{}' '["Specify runtime: --runtime <types>", "Run .zcp/workflow.sh show to see valid types"]'
        return 1
    fi

    # ============================================================
    # AUTOMATIC TYPE RESOLUTION (via resolve-types.sh)
    # ============================================================
    # Returns full objects with versions from data.json

    local all_inputs="$runtime"
    [ -n "$services" ] && all_inputs="$all_inputs $services"

    local resolved_json
    if type resolve_types &>/dev/null; then
        all_inputs=$(echo "$all_inputs" | tr ',' ' ')
        resolved_json=$(resolve_types $all_inputs)

        local resolution_success
        resolution_success=$(echo "$resolved_json" | jq -r '.success')

        if [ "$resolution_success" != "true" ]; then
            local errors
            errors=$(echo "$resolved_json" | jq -r '.errors | join("; ")')
            json_error "plan" "Type resolution failed: $errors" "$resolved_json" '["Ensure network access to docs.zerops.io", "Run: .zcp/lib/bootstrap/resolve-types.sh --list"]'
            return 1
        fi

        # Show warnings about alias resolution
        local warnings
        warnings=$(echo "$resolved_json" | jq -r '.warnings[]' 2>/dev/null)
        [ -n "$warnings" ] && echo "Type resolution: $warnings" >&2
    else
        json_error "plan" "resolve-types.sh not loaded" '{}' '["Source resolve-types.sh first"]'
        return 1
    fi

    # Extract runtime objects with versions
    local runtime_objects
    runtime_objects=$(echo "$resolved_json" | jq '.runtimes')

    # Extract service objects with versions
    local service_objects
    service_objects=$(echo "$resolved_json" | jq '.services')

    # Get type names for prefix matching
    local runtime_names
    runtime_names=$(echo "$runtime_objects" | jq -r '.[].type' | tr '\n' ' ')
    local runtime_array=()
    read -ra runtime_array <<< "$runtime_names"

    local prefix_array=()
    IFS=',' read -ra prefix_array <<< "$prefix"

    # Validate we have at least one runtime
    if [ ${#runtime_array[@]} -eq 0 ] || [ -z "${runtime_array[0]}" ]; then
        json_error "plan" "No valid runtimes specified" '{}' '["At least one runtime required. Run .zcp/workflow.sh show to see valid types"]'
        return 1
    fi

    # Validate prefixes
    for pfx in "${prefix_array[@]}"; do
        if [[ ! "$pfx" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || [ ${#pfx} -gt 58 ]; then
            json_error "plan" "Invalid prefix: $pfx" '{}' '["Must be lowercase alphanumeric, may contain hyphens, max 58 chars"]'
            return 1
        fi
    done

    # If fewer prefixes than runtimes, use runtime type as prefix
    local num_runtimes=${#runtime_array[@]}
    local num_prefixes=${#prefix_array[@]}

    if [ $num_prefixes -lt $num_runtimes ]; then
        # Clear default "app" if we have multiple runtimes and only default prefix
        if [ $num_prefixes -eq 1 ] && [ "${prefix_array[0]}" = "app" ]; then
            prefix_array=()
        fi
        # Use runtime type as prefix for each runtime without explicit prefix
        for ((i=${#prefix_array[@]}; i<num_runtimes; i++)); do
            prefix_array+=("${runtime_array[$i]}")
        done
    fi

    # Build hostnames for each runtime
    local dev_hostnames=()
    local stage_hostnames=()

    for ((i=0; i<num_runtimes; i++)); do
        dev_hostnames+=("${prefix_array[$i]}dev")
        stage_hostnames+=("${prefix_array[$i]}stage")
    done

    # Build JSON arrays for hostnames
    local dev_hostnames_json stage_hostnames_json
    dev_hostnames_json=$(printf '%s\n' "${dev_hostnames[@]}" | jq -R . | jq -s .)
    stage_hostnames_json=$(printf '%s\n' "${stage_hostnames[@]}" | jq -R . | jq -s .)

    local plan_data
    plan_data=$(jq -n \
        --argjson runtimes "$runtime_objects" \
        --argjson managed "$service_objects" \
        --argjson dev_hosts "$dev_hostnames_json" \
        --argjson stage_hosts "$stage_hostnames_json" \
        --arg ha "$ha_mode" \
        '{
            runtimes: $runtimes,
            managed_services: $managed,
            dev_hostnames: $dev_hosts,
            stage_hostnames: $stage_hosts,
            ha_mode: ($ha == "true"),
            dev_hostname: $dev_hosts[0],
            stage_hostname: $stage_hosts[0]
        }')

    # Get or create session
    local session_id
    session_id=$(cat "${ZCP_TMP_DIR:-/tmp}/claude_session" 2>/dev/null || echo "")
    if [ -z "$session_id" ]; then
        session_id=$(generate_secure_session_id 2>/dev/null || echo "$(date +%Y%m%d%H%M%S)-$$-$RANDOM$RANDOM")
        echo "$session_id" > "${ZCP_TMP_DIR:-/tmp}/claude_session"
        echo "bootstrap" > "${ZCP_TMP_DIR:-/tmp}/claude_mode"
        echo "INIT" > "${ZCP_TMP_DIR:-/tmp}/claude_phase"
    fi

    # Initialize bootstrap state
    init_state "$plan_data" "$session_id"

    # Record step completion
    record_step "plan" "complete" "$plan_data"

    # Also write plan to temp file for compatibility
    echo "$plan_data" > "${ZCP_TMP_DIR:-/tmp}/bootstrap_plan.json"

    # Build message
    local msg
    if [ $num_runtimes -eq 1 ]; then
        msg="Plan created: ${runtime_array[0]} app"
        [ ${#managed_services[@]} -gt 0 ] && msg="$msg with ${managed_services[*]}"
    else
        msg="Plan created: ${num_runtimes} services (${runtime_array[*]})"
    fi

    json_response "plan" "$msg" "$plan_data" "recipe-search"
}

export -f step_plan
