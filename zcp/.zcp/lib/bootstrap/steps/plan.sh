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
        json_error "plan" "Missing required --runtime argument" '{}' '["Specify runtime: --runtime go", "Supported: go, nodejs, python, php, rust, bun, java, dotnet", "Aliases: node→nodejs, postgres→postgresql, redis→valkey"]'
        return 1
    fi

    # ============================================================
    # AUTOMATIC TYPE RESOLUTION (via resolve-types.sh)
    # ============================================================
    # Resolves aliases and validates against data.json
    # "go bun postgres valkey nats" → runtimes: [go, bun], services: [postgresql, valkey, nats]

    local all_inputs="$runtime"
    [ -n "$services" ] && all_inputs="$all_inputs $services"

    local resolved_json
    if type resolve_types &>/dev/null; then
        # Normalize: replace commas with spaces for resolution
        all_inputs=$(echo "$all_inputs" | tr ',' ' ')
        resolved_json=$(resolve_types $all_inputs)

        # Check if resolution succeeded
        local resolution_success
        resolution_success=$(echo "$resolved_json" | jq -r '.success')

        if [ "$resolution_success" != "true" ]; then
            local errors
            errors=$(echo "$resolved_json" | jq -r '.errors | join("; ")')
            json_error "plan" "Type resolution failed: $errors" "$resolved_json" '["Run: .zcp/lib/bootstrap/resolve-types.sh --list"]'
            return 1
        fi

        # Extract resolved values
        local resolved_runtimes resolved_services
        resolved_runtimes=$(echo "$resolved_json" | jq -r '.flags.runtime')
        resolved_services=$(echo "$resolved_json" | jq -r '.flags.services')

        # Show warnings about alias resolution
        local warnings
        warnings=$(echo "$resolved_json" | jq -r '.warnings[]' 2>/dev/null)
        if [ -n "$warnings" ]; then
            echo "Type resolution: $warnings" >&2
        fi

        # Use resolved values
        runtime="$resolved_runtimes"
        services="$resolved_services"
    fi

    # Parse multiple runtimes if comma-separated
    local runtime_array=()
    local prefix_array=()

    IFS=',' read -ra runtime_array <<< "$runtime"
    IFS=',' read -ra prefix_array <<< "$prefix"

    # Validate we have at least one runtime
    if [ ${#runtime_array[@]} -eq 0 ] || [ -z "${runtime_array[0]}" ]; then
        json_error "plan" "No valid runtimes specified" '{}' '["At least one runtime required: --runtime go", "Run: .zcp/lib/bootstrap/resolve-types.sh --list"]'
        return 1
    fi

    # HIGH-10: Validate each prefix (lowercase alphanumeric, hyphens allowed but not at start/end)
    for pfx in "${prefix_array[@]}"; do
        if [[ ! "$pfx" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || [ ${#pfx} -gt 58 ]; then
            json_error "plan" "Invalid prefix: $pfx" '{}' '["Must be lowercase alphanumeric, may contain hyphens, max 58 chars"]'
            return 1
        fi
    done

    # If fewer prefixes than runtimes, use first prefix for all
    local num_runtimes=${#runtime_array[@]}
    local num_prefixes=${#prefix_array[@]}

    if [ $num_prefixes -lt $num_runtimes ]; then
        for ((i=num_prefixes; i<num_runtimes; i++)); do
            prefix_array+=("${prefix_array[0]}")
        done
    fi

    # Build hostnames for each runtime
    local dev_hostnames=()
    local stage_hostnames=()

    for ((i=0; i<num_runtimes; i++)); do
        dev_hostnames+=("${prefix_array[$i]}dev")
        stage_hostnames+=("${prefix_array[$i]}stage")
    done

    # Parse managed services
    local managed_services=()
    if [ -n "$services" ]; then
        IFS=',' read -ra managed_services <<< "$services"
    fi

    # Build plan JSON
    local dev_hostnames_json stage_hostnames_json managed_json runtimes_json

    runtimes_json=$(printf '%s\n' "${runtime_array[@]}" | jq -R . | jq -s .)
    dev_hostnames_json=$(printf '%s\n' "${dev_hostnames[@]}" | jq -R . | jq -s .)
    stage_hostnames_json=$(printf '%s\n' "${stage_hostnames[@]}" | jq -R . | jq -s .)

    if [ ${#managed_services[@]} -gt 0 ]; then
        managed_json=$(printf '%s\n' "${managed_services[@]}" | jq -R . | jq -s .)
    else
        managed_json='[]'
    fi

    local plan_data
    plan_data=$(jq -n \
        --argjson runtimes "$runtimes_json" \
        --argjson managed "$managed_json" \
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
