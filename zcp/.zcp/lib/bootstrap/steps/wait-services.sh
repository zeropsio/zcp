#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/wait-services.sh
# Step: Wait for services to reach RUNNING state
#
# TWO MODES:
#   1. Single check (default): Returns immediately with current status
#   2. Polling mode (--wait): Loops internally until complete or timeout
#
# IMPORTANT: Agents should use --wait mode to avoid writing shell loops!
# DO NOT write while loops in bash - use --wait flag instead!
#
# Usage:
#   .zcp/bootstrap.sh step wait-services              # Single check (returns immediately)
#   .zcp/bootstrap.sh step wait-services --wait       # Poll until ready (RECOMMENDED)
#   .zcp/bootstrap.sh step wait-services --wait 300   # Poll with custom timeout
#
# Inputs: Service IDs (from import-services)
# Outputs: Service statuses, ready when all RUNNING

# Track start time for timeout (session-scoped to prevent race conditions)
_get_wait_start_file() {
    local session_id
    session_id=$(cat "${ZCP_TMP_DIR:-/tmp}/claude_session" 2>/dev/null || echo "$$")
    echo "${ZCP_TMP_DIR:-/tmp}/bootstrap_wait_start_${session_id}"
}

# Internal: Check service status once, return result code
# Returns: 0 = all ready, 1 = error, 2 = still waiting
_check_services_status() {
    local timeout="$1"
    local silent="${2:-false}"

    # Check projectId
    if [ -z "${projectId:-}" ]; then
        [ "$silent" = false ] && json_error "wait-services" "projectId not set" '{}' '[]'
        return 1
    fi

    # Track elapsed time
    local wait_start_file
    wait_start_file=$(_get_wait_start_file)

    local start_time elapsed_seconds
    if [ -f "$wait_start_file" ]; then
        start_time=$(cat "$wait_start_file")
    else
        start_time=$(date +%s)
        echo "$start_time" > "$wait_start_file"
    fi
    elapsed_seconds=$(($(date +%s) - start_time))

    # Check timeout
    if [ "$elapsed_seconds" -gt "$timeout" ]; then
        rm -f "$wait_start_file"
        [ "$silent" = false ] && json_error "wait-services" "Timeout waiting for services (${timeout}s)" \
            "{\"elapsed_seconds\": $elapsed_seconds, \"timeout\": true}" \
            '["Check zcli service list", "Check project notifications"]'
        return 1
    fi

    # Get current service status
    local services_json
    services_json=$(zcli service list -P "$projectId" --format json 2>&1 | extract_zcli_json)

    if [ -z "$services_json" ] || ! echo "$services_json" | jq -e . >/dev/null 2>&1; then
        [ "$silent" = false ] && json_error "wait-services" "Failed to get service list" '{}' '["Check zcli authentication"]'
        return 1
    fi

    # Get plan to know which services to wait for
    local plan
    plan=$(get_plan)

    local dev_hostnames stage_hostnames
    dev_hostnames=$(echo "$plan" | jq -r '.dev_hostnames // [.dev_hostname] | .[]')
    stage_hostnames=$(echo "$plan" | jq -r '.stage_hostnames // [.stage_hostname] | .[]')

    # Build status object for each service
    local service_statuses='{}'
    local ready_count=0
    local total_count=0
    local service_ids='{}'

    local service_count
    service_count=$(echo "$services_json" | jq '(.services // []) | length')

    # Ensure service_count is a valid number
    if ! [[ "$service_count" =~ ^[0-9]+$ ]]; then
        service_count=0
    fi

    for ((i=0; i<service_count; i++)); do
        local name status id
        name=$(echo "$services_json" | jq -r ".services[$i].name // \"\"")
        status=$(echo "$services_json" | jq -r ".services[$i].status // \"\"")
        id=$(echo "$services_json" | jq -r ".services[$i].id // \"\"")

        # Check if this is one of our bootstrap services
        local is_bootstrap_service=false

        for dev in $dev_hostnames; do
            [ "$name" = "$dev" ] && is_bootstrap_service=true
        done
        for stage in $stage_hostnames; do
            [ "$name" = "$stage" ] && is_bootstrap_service=true
        done

        # Also check managed services (db, cache, etc.)
        local managed
        managed=$(echo "$plan" | jq -r '.managed_services[]' 2>/dev/null || echo "")
        for m in $managed; do
            case "$m" in
                postgresql*|mysql*|mariadb*|mongodb*) [ "$name" = "db" ] && is_bootstrap_service=true ;;
                valkey*|keydb*) [ "$name" = "cache" ] && is_bootstrap_service=true ;;
                rabbitmq*|nats*) [ "$name" = "queue" ] && is_bootstrap_service=true ;;
                elasticsearch*) [ "$name" = "search" ] && is_bootstrap_service=true ;;
                minio*) [ "$name" = "storage" ] && is_bootstrap_service=true ;;
            esac
        done

        if [ "$is_bootstrap_service" = true ]; then
            total_count=$((total_count + 1))

            local is_ready=false
            if [ "$status" = "RUNNING" ] || [ "$status" = "ACTIVE" ]; then
                ready_count=$((ready_count + 1))
                is_ready=true
            fi

            # Ensure valid JSON base before updating
            if ! echo "$service_statuses" | jq -e . >/dev/null 2>&1; then
                service_statuses='{}'
            fi
            if ! echo "$service_ids" | jq -e . >/dev/null 2>&1; then
                service_ids='{}'
            fi

            service_statuses=$(echo "$service_statuses" | jq \
                --arg n "$name" \
                --arg s "$status" \
                --argjson r "$is_ready" \
                '. + {($n): {status: $s, ready: $r}}')

            service_ids=$(echo "$service_ids" | jq \
                --arg n "$name" \
                --arg id "$id" \
                '. + {($n): $id}')
        fi
    done

    # Export for caller
    export _WS_SERVICE_STATUSES="$service_statuses"
    export _WS_SERVICE_IDS="$service_ids"
    export _WS_READY_COUNT="$ready_count"
    export _WS_TOTAL_COUNT="$total_count"
    export _WS_ELAPSED="$elapsed_seconds"

    # If no bootstrap services found yet, services might still be creating
    if [ "$total_count" -eq 0 ]; then
        return 2  # Still waiting
    fi

    # Check if all ready
    if [ "$ready_count" -eq "$total_count" ]; then
        rm -f "$wait_start_file"
        return 0  # All ready
    fi

    return 2  # Still waiting
}

# Output JSON response based on status
_output_status() {
    local status_code="$1"
    local service_statuses="${_WS_SERVICE_STATUSES}"
    local service_ids="${_WS_SERVICE_IDS}"
    local ready_count="${_WS_READY_COUNT:-0}"
    local total_count="${_WS_TOTAL_COUNT:-0}"
    local elapsed="${_WS_ELAPSED:-0}"

    # Ensure valid JSON (empty string != unset, so :- doesn't help)
    [ -z "$service_statuses" ] && service_statuses='{}'
    [ -z "$service_ids" ] && service_ids='{}'

    case "$status_code" in
        0)  # All ready
            local data
            data=$(jq -n \
                --argjson services "$service_statuses" \
                --argjson ids "$service_ids" \
                '{
                    services: $services,
                    service_ids: $ids
                }')

            record_step "wait-services" "complete" "$data"
            echo "$data" > "${ZCP_TMP_DIR:-/tmp}/bootstrap_wait_status.json"
            json_response "wait-services" "All $total_count services RUNNING" "$data" "mount-dev"
            ;;
        2)  # Still waiting
            if [ "$total_count" -eq 0 ]; then
                local data
                data=$(jq -n \
                    --argjson elapsed "$elapsed" \
                    '{
                        services: {},
                        ready_count: 0,
                        total_count: 0,
                        elapsed_seconds: $elapsed,
                        note: "Services not yet visible - still creating"
                    }')
                json_progress "wait-services" "Waiting for services to appear (${elapsed}s elapsed)" "$data" "wait-services"
            else
                local data
                data=$(jq -n \
                    --argjson services "$service_statuses" \
                    --argjson ready "$ready_count" \
                    --argjson total "$total_count" \
                    --argjson elapsed "$elapsed" \
                    '{
                        services: $services,
                        ready_count: $ready,
                        total_count: $total,
                        elapsed_seconds: $elapsed,
                        hint: "Use --wait flag for automatic polling: .zcp/bootstrap.sh step wait-services --wait"
                    }')
                json_progress "wait-services" "Waiting: $ready_count/$total_count services ready (${elapsed}s elapsed)" "$data" "wait-services"
            fi
            ;;
    esac
}

# Main entry point
step_wait_services() {
    local wait_mode=false
    local timeout=600  # Default 10 minutes
    local poll_interval=5

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --wait)
                wait_mode=true
                shift
                ;;
            --timeout)
                timeout="$2"
                shift 2
                ;;
            [0-9]*)
                # Legacy: first number is timeout
                timeout="$1"
                shift
                ;;
            *)
                shift
                ;;
        esac
    done

    if [ "$wait_mode" = true ]; then
        # POLLING MODE: Loop internally until complete
        echo "Waiting for services to be ready (timeout: ${timeout}s)..." >&2

        while true; do
            _check_services_status "$timeout" true
            local result=$?

            case $result in
                0)  # All ready
                    _output_status 0
                    return 0
                    ;;
                1)  # Error/timeout
                    _output_status 1
                    return 1
                    ;;
                2)  # Still waiting
                    local ready="${_WS_READY_COUNT:-0}"
                    local total="${_WS_TOTAL_COUNT:-0}"
                    local elapsed="${_WS_ELAPSED:-0}"
                    printf "\r  [%3d/%3ds] %s/%s services ready...   " "$elapsed" "$timeout" "$ready" "$total" >&2
                    sleep "$poll_interval"
                    ;;
            esac
        done
    else
        # SINGLE CHECK MODE: Return immediately
        _check_services_status "$timeout" false
        local result=$?
        _output_status $result
        return 0
    fi
}

export -f step_wait_services _check_services_status _output_status _get_wait_start_file
