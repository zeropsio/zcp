#!/usr/bin/env bash
# Step: Discover actual environment variables from running services
#
# This step runs AFTER services are created and mounted, BEFORE finalize.
# It queries each dev container to discover what env vars are actually available.
#
# Inputs: Plan with managed_services list, mounted dev services
# Outputs: discovery.json with actual available variables per service

step_discover_services() {
    local plan
    plan=$(get_plan)

    if [ -z "$plan" ] || [ "$plan" = "null" ] || [ "$plan" = "{}" ]; then
        json_error "discover-services" "No bootstrap plan found" '{}' '["Run plan step first: .zcp/bootstrap.sh step plan --runtime <type>"]'
        return 1
    fi

    # Get first dev hostname to query (all dev services see same managed service vars)
    local dev_hostname
    dev_hostname=$(echo "$plan" | jq -r '.dev_hostnames[0] // "appdev"')

    # Get managed service hostnames from plan
    local managed_services
    managed_services=$(echo "$plan" | jq -r '.managed_services[]?.hostname // empty' 2>/dev/null | tr '\n' ' ')

    # Also check for standard hostnames based on service types
    local service_types
    service_types=$(echo "$plan" | jq -r '.managed_services[]?.type // empty' 2>/dev/null)

    local hostnames_to_check=""
    for svc_type in $service_types; do
        case "$svc_type" in
            postgresql*|mysql*|mariadb*|mongodb*) hostnames_to_check+=" db" ;;
            valkey*|keydb*|redis*) hostnames_to_check+=" cache" ;;
            rabbitmq*|nats*) hostnames_to_check+=" queue" ;;
            elasticsearch*) hostnames_to_check+=" search" ;;
            minio*) hostnames_to_check+=" storage" ;;
        esac
    done

    # Combine and dedupe
    local all_hostnames
    all_hostnames=$(echo "$managed_services $hostnames_to_check" | tr ' ' '\n' | grep -v '^$' | sort -u | tr '\n' ' ')

    if [ -z "$all_hostnames" ] || [ "$all_hostnames" = " " ]; then
        # No managed services to discover
        local empty_result
        empty_result=$(jq -n \
            --arg dev "$dev_hostname" \
            --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            '{
                discovered_from: $dev,
                timestamp: $ts,
                services: {},
                note: "No managed services configured - nothing to discover"
            }')

        record_step "discover-services" "complete" "$empty_result"
        json_response "discover-services" "No managed services to discover" "$empty_result" "finalize"
        return 0
    fi

    # Wait for dev service to be ready
    local ssh_ready=false
    local max_attempts=3
    local attempt=0

    while [ "$attempt" -lt "$max_attempts" ] && [ "$ssh_ready" = "false" ]; do
        if ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$dev_hostname" "echo ready" &>/dev/null; then
            ssh_ready=true
        else
            attempt=$((attempt + 1))
            [ "$attempt" -lt "$max_attempts" ] && sleep 2
        fi
    done

    if [ "$ssh_ready" = "false" ]; then
        json_error "discover-services" "Dev service $dev_hostname not accessible via SSH" '{}' \
            '["Wait for service: .zcp/status.sh --wait '"$dev_hostname"'", "Check SSH: ssh '"$dev_hostname"' echo test"]'
        return 1
    fi

    # Run discovery
    local discovery_script="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")}/../discover-env.sh"

    local discovered
    if [ -f "$discovery_script" ]; then
        # shellcheck disable=SC2086
        discovered=$("$discovery_script" "$dev_hostname" $all_hostnames 2>/dev/null || echo '{}')
    else
        # Inline discovery if script not found
        discovered='{}'
        for svc in $all_hostnames; do
            [ -z "$svc" ] && continue
            local vars
            vars=$(ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$dev_hostname" \
                "env | grep -E \"^${svc}_\" | cut -d= -f1 | sort" 2>/dev/null | \
                jq -R -s 'split("\n") | map(select(length > 0))' || echo '[]')
            discovered=$(echo "$discovered" | jq --arg s "$svc" --argjson v "$vars" '.[$s] = {variables: $v}')
        done
    fi

    # Save discovery results
    local discovery_file="${ZCP_TMP_DIR:-/tmp}/service_discovery.json"

    local result
    result=$(jq -n \
        --arg dev "$dev_hostname" \
        --argjson discovered "$discovered" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{
            discovered_from: $dev,
            timestamp: $ts,
            services: $discovered,
            note: "These are the ACTUAL available environment variables, discovered at runtime"
        }')

    echo "$result" > "$discovery_file"

    record_step "discover-services" "complete" "$result"

    # Count discovered services with variables
    local svc_count
    svc_count=$(echo "$discovered" | jq 'to_entries | map(select(.value.variables | length > 0)) | length')

    json_response "discover-services" "Discovered environment variables from $dev_hostname ($svc_count services with vars)" "$result" "finalize"
}

export -f step_discover_services
