#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/finalize.sh
# Step: Create per-service handoff data for subagent consumption
#
# This is the bridge between infrastructure setup and code generation.
# It produces structured handoffs that subagents can consume directly.
#
# Inputs: All previous steps (plan, recipes, mounts, service IDs)
# Outputs: Per-service handoffs with everything needed for code generation

step_finalize() {
    # Gather all data from previous steps
    local plan
    plan=$(get_plan)

    if [ "$plan" = '{}' ]; then
        json_error "finalize" "No plan found - run earlier steps first" '{}' '[]'
        return 1
    fi

    # Get recipe patterns
    local recipe_patterns='{}'
    local recipe_file="${ZCP_TMP_DIR:-/tmp}/recipe_review.json"
    if [ -f "$recipe_file" ]; then
        recipe_patterns=$(cat "$recipe_file")
    fi

    # Get service IDs from wait-services step
    local wait_data
    wait_data=$(get_step_data "wait-services")
    local service_ids
    service_ids=$(echo "$wait_data" | jq '.service_ids // {}')

    # Get mount data
    local mount_data
    mount_data=$(get_step_data "mount-dev")
    local mounts
    mounts=$(echo "$mount_data" | jq '.mounts // {}')

    # Get runtimes and hostnames
    local runtimes dev_hostnames stage_hostnames managed_services
    runtimes=$(echo "$plan" | jq -r '.runtimes // [.runtimes[0]] | .[]' 2>/dev/null || echo "$plan" | jq -r '.runtimes[0]')
    dev_hostnames=$(echo "$plan" | jq -r '.dev_hostnames // [.dev_hostname] | .[]')
    stage_hostnames=$(echo "$plan" | jq -r '.stage_hostnames // [.stage_hostname] | .[]')
    managed_services=$(echo "$plan" | jq '.managed_services // []')

    # Build per-service handoffs
    local handoffs='[]'
    local index=0

    # Convert to arrays for iteration
    local dev_array=()
    local stage_array=()
    local runtime_array=()

    while IFS= read -r line; do
        [ -n "$line" ] && dev_array+=("$line")
    done <<< "$dev_hostnames"

    while IFS= read -r line; do
        [ -n "$line" ] && stage_array+=("$line")
    done <<< "$stage_hostnames"

    while IFS= read -r line; do
        [ -n "$line" ] && runtime_array+=("$line")
    done <<< "$runtimes"

    # If only one runtime, use it for all
    if [ ${#runtime_array[@]} -eq 1 ] && [ ${#dev_array[@]} -gt 1 ]; then
        local single_runtime="${runtime_array[0]}"
        runtime_array=()
        for ((i=0; i<${#dev_array[@]}; i++)); do
            runtime_array+=("$single_runtime")
        done
    fi

    for ((i=0; i<${#dev_array[@]}; i++)); do
        local dev_host="${dev_array[$i]}"
        local stage_host="${stage_array[$i]:-${dev_host/dev/stage}}"
        local runtime="${runtime_array[$i]:-${runtime_array[0]}}"

        # Get service IDs
        local dev_id stage_id
        dev_id=$(echo "$service_ids" | jq -r --arg h "$dev_host" '.[$h] // ""')
        stage_id=$(echo "$service_ids" | jq -r --arg h "$stage_host" '.[$h] // ""')

        # Get mount path
        local mount_path
        mount_path=$(echo "$mounts" | jq -r --arg h "$dev_host" '.[$h].mount_path // "/var/www/\($h)"')

        # Get runtime version from recipe patterns
        local runtime_version
        runtime_version=$(echo "$recipe_patterns" | jq -r \
            --arg rt "$runtime" \
            '.patterns_extracted.runtime_patterns[$rt].dev_runtime_base // "\($rt)@1"' 2>/dev/null || echo "${runtime}@1")

        # Build managed services info with env prefix mapping
        local managed_info='[]'
        local managed_list
        managed_list=$(echo "$managed_services" | jq -r '.[]' 2>/dev/null || echo "")

        for svc in $managed_list; do
            local svc_name svc_type env_prefix
            case "$svc" in
                postgresql*|mysql*|mariadb*|mongodb*)
                    svc_name="db"
                    svc_type=$(source "${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/lib/bootstrap/import-gen.sh" 2>/dev/null && get_service_version "$svc" || echo "${svc}@latest")
                    env_prefix="DB"
                    ;;
                valkey*|keydb*)
                    svc_name="cache"
                    svc_type=$(source "${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/lib/bootstrap/import-gen.sh" 2>/dev/null && get_service_version "$svc" || echo "${svc}@latest")
                    env_prefix="REDIS"
                    ;;
                rabbitmq*|nats*)
                    svc_name="queue"
                    svc_type=$(source "${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/lib/bootstrap/import-gen.sh" 2>/dev/null && get_service_version "$svc" || echo "${svc}@latest")
                    env_prefix="AMQP"
                    ;;
                elasticsearch*)
                    svc_name="search"
                    svc_type=$(source "${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/lib/bootstrap/import-gen.sh" 2>/dev/null && get_service_version "$svc" || echo "${svc}@latest")
                    env_prefix="ES"
                    ;;
                minio*)
                    svc_name="storage"
                    svc_type=$(source "${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/lib/bootstrap/import-gen.sh" 2>/dev/null && get_service_version "$svc" || echo "${svc}@latest")
                    env_prefix="S3"
                    ;;
                *)
                    svc_name="$svc"
                    svc_type="${svc}@latest"
                    env_prefix=$(echo "$svc" | tr '[:lower:]' '[:upper:]')
                    ;;
            esac

            managed_info=$(echo "$managed_info" | jq \
                --arg n "$svc_name" \
                --arg t "$svc_type" \
                --arg e "$env_prefix" \
                '. + [{name: $n, type: $t, env_prefix: $e}]')
        done

        # Build handoff for this service pair
        local handoff
        handoff=$(jq -n \
            --arg dev_host "$dev_host" \
            --arg stage_host "$stage_host" \
            --arg dev_id "$dev_id" \
            --arg stage_id "$stage_id" \
            --arg mount "$mount_path" \
            --arg rt "$runtime" \
            --arg rtv "$runtime_version" \
            --argjson managed "$managed_info" \
            --argjson patterns "$recipe_patterns" \
            '{
                dev_hostname: $dev_host,
                stage_hostname: $stage_host,
                dev_id: $dev_id,
                stage_id: $stage_id,
                mount_path: $mount,
                runtime: $rt,
                runtime_version: $rtv,
                managed_services: $managed,
                recipe_patterns: $patterns
            }')

        handoffs=$(echo "$handoffs" | jq --argjson h "$handoff" '. + [$h]')
    done

    # Count handoffs
    local handoff_count
    handoff_count=$(echo "$handoffs" | jq 'length')

    # Write handoff file
    local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"
    local handoff_data
    handoff_data=$(jq -n \
        --arg session "$(cat "${ZCP_TMP_DIR:-/tmp}/claude_session" 2>/dev/null || echo "unknown")" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        --argjson handoffs "$handoffs" \
        '{
            session_id: $session,
            timestamp: $ts,
            status: "ready_for_code_generation",
            service_handoffs: $handoffs
        }')

    echo "$handoff_data" > "$handoff_file"

    # Also persist to state
    mkdir -p "${BOOTSTRAP_STATE_DIR:-${STATE_DIR:-/tmp}/bootstrap}" 2>/dev/null || true
    cp "$handoff_file" "${BOOTSTRAP_STATE_DIR:-${STATE_DIR:-/tmp}/bootstrap}/handoff.json" 2>/dev/null || true

    local data
    data=$(jq -n \
        --arg f "$handoff_file" \
        --argjson h "$handoffs" \
        '{
            handoff_file: $f,
            service_handoffs: $h
        }')

    record_step "finalize" "complete" "$data"

    local msg
    if [ "$handoff_count" -eq 1 ]; then
        msg="Infrastructure ready - 1 service pair for code generation"
    else
        msg="Infrastructure ready - $handoff_count service pairs for code generation"
    fi

    json_response "finalize" "$msg" "$data" "null"
}

export -f step_finalize
