#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/wait-services.sh
# Step: Wait for services to reach RUNNING state
#
# This step is designed for repeated calls (polling).
# Returns in_progress until all services are RUNNING.
#
# Inputs: Service IDs (from import-services)
# Outputs: Service statuses, ready when all RUNNING

# HIGH-11: Track start time for timeout (session-scoped to prevent race conditions)
# Use session ID from bootstrap state for scoping
_get_wait_start_file() {
    local session_id
    session_id=$(cat "${ZCP_TMP_DIR:-/tmp}/claude_session" 2>/dev/null || echo "$$")
    echo "${ZCP_TMP_DIR:-/tmp}/bootstrap_wait_start_${session_id}"
}

step_wait_services() {
    local timeout="${1:-600}"  # Default 10 minute timeout

    # Check projectId
    if [ -z "${projectId:-}" ]; then
        json_error "wait-services" "projectId not set" '{}' '[]'
        return 1
    fi

    # HIGH-11: Use session-scoped wait start file
    local wait_start_file
    wait_start_file=$(_get_wait_start_file)

    # Track elapsed time
    local start_time elapsed_seconds
    if [ -f "$wait_start_file" ]; then
        start_time=$(cat "$wait_start_file")
    else
        start_time=$(date +%s)
        echo "$start_time" > "$wait_start_file"
    fi
    elapsed_seconds=$(($(date +%s) - start_time))

    # Check timeout
    if [ $elapsed_seconds -gt $timeout ]; then
        rm -f "$wait_start_file"
        json_error "wait-services" "Timeout waiting for services (${timeout}s)" \
            "{\"elapsed_seconds\": $elapsed_seconds, \"timeout\": true}" \
            '["Check zcli service list", "Check project notifications", "Retry: .zcp/bootstrap.sh step wait-services"]'
        return 1
    fi

    # Get current service status
    local services_json
    services_json=$(zcli service list -P "$projectId" --format json 2>&1 | extract_zcli_json)

    if [ -z "$services_json" ] || ! echo "$services_json" | jq -e . >/dev/null 2>&1; then
        json_error "wait-services" "Failed to get service list" '{}' '["Check zcli authentication", "Run: zcli service list"]'
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

    # HIGH-9: Use JSON array output instead of colon-delimited (prevents parsing failures)
    local service_count
    service_count=$(echo "$services_json" | jq '.services | length')

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
            # Managed services use hostnames like "db", "cache"
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

            local is_ready="false"
            if [ "$status" = "RUNNING" ] || [ "$status" = "ACTIVE" ]; then
                ready_count=$((ready_count + 1))
                is_ready="true"
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

    # If no bootstrap services found yet, services might still be creating
    if [ $total_count -eq 0 ]; then
        local data
        data=$(jq -n \
            --argjson elapsed "$elapsed_seconds" \
            '{
                services: {},
                ready_count: 0,
                total_count: 0,
                elapsed_seconds: $elapsed,
                note: "Services not yet visible - still creating"
            }')

        json_progress "wait-services" "Waiting for services to appear (${elapsed_seconds}s elapsed)" "$data" "wait-services"
        return 0
    fi

    # Check if all ready
    if [ $ready_count -eq $total_count ]; then
        rm -f "$wait_start_file"

        local data
        data=$(jq -n \
            --argjson services "$service_statuses" \
            --argjson ids "$service_ids" \
            '{
                services: $services,
                service_ids: $ids
            }')

        record_step "wait-services" "complete" "$data"

        # Save wait status to temp for compatibility
        echo "$data" > "${ZCP_TMP_DIR:-/tmp}/bootstrap_wait_status.json"

        json_response "wait-services" "All $total_count services RUNNING" "$data" "mount-dev"
    else
        local data
        data=$(jq -n \
            --argjson services "$service_statuses" \
            --argjson ready "$ready_count" \
            --argjson total "$total_count" \
            --argjson elapsed "$elapsed_seconds" \
            '{
                services: $services,
                ready_count: $ready,
                total_count: $total,
                elapsed_seconds: $elapsed
            }')

        json_progress "wait-services" "Waiting: $ready_count/$total_count services ready (${elapsed_seconds}s elapsed)" "$data" "wait-services"
    fi
}

export -f step_wait_services
