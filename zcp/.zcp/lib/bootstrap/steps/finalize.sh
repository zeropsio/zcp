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

    # Get recipe patterns - will be fetched per-runtime below
    local global_recipe_patterns='{}'
    local global_recipe_file="${ZCP_TMP_DIR:-/tmp}/recipe_review.json"
    if [ -f "$global_recipe_file" ]; then
        global_recipe_patterns=$(cat "$global_recipe_file")
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

    # P2: Get recipe-search step data for runtime recipes and service docs
    local recipe_search_data
    recipe_search_data=$(get_step_data "recipe-search")

    # Get runtimes with versions from plan (new format: [{type, version, base}])
    local runtime_objects
    runtime_objects=$(echo "$plan" | jq '.runtimes // []')

    # Get managed services with versions (new format: [{type, version}])
    local service_objects
    service_objects=$(echo "$plan" | jq '.managed_services // []')

    # Extract hostname lists
    local dev_hostnames stage_hostnames
    dev_hostnames=$(echo "$plan" | jq -r '.dev_hostnames // [.dev_hostname] | .[]')
    stage_hostnames=$(echo "$plan" | jq -r '.stage_hostnames // [.stage_hostname] | .[]')

    # Extract runtime names for iteration
    local runtimes
    runtimes=$(echo "$runtime_objects" | jq -r '.[].type' 2>/dev/null)

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

        # Get runtime info from plan objects (use .[]? for null-safe iteration)
        local runtime_obj runtime_version base_images recipe_file
        runtime_obj=$(echo "$runtime_objects" | jq --arg rt "$runtime" '.[]? | select(.type == $rt)')
        # Use jq --arg for variable interpolation in fallback
        runtime_version=$(echo "$runtime_obj" | jq -r --arg rt "$runtime" '.version // ($rt + "@1")')
        base_images=$(echo "$runtime_obj" | jq -c '.base // []')

        # Recipe file from recipe-search step
        local runtime_recipes
        runtime_recipes=$(echo "$recipe_search_data" | jq '.runtime_recipes // {}')
        recipe_file=$(echo "$runtime_recipes" | jq -r --arg rt "$runtime" '.[$rt].recipe_file // ""')
        [ -z "$recipe_file" ] && recipe_file="${ZCP_TMP_DIR:-/tmp}/recipe_${runtime}.md"

        # Build managed services info - USE DISCOVERY DATA when available
        local managed_info='[]'

        # Get service docs from recipe-search step (uses recipe_search_data from earlier)
        local service_docs_data
        service_docs_data=$(echo "$recipe_search_data" | jq '.service_docs // {}')

        # CRITICAL: Load discovery data to get ACTUAL env vars (not assumptions)
        local discovery_file="${ZCP_TMP_DIR:-/tmp}/service_discovery.json"
        local discovery_data='{}'
        if [ -f "$discovery_file" ]; then
            discovery_data=$(cat "$discovery_file")
        fi

        # Use while read to safely iterate JSON objects (avoids word-splitting issues)
        while IFS= read -r svc_obj; do
            [ -z "$svc_obj" ] && continue

            local svc svc_type svc_name env_prefix env_vars reference_doc

            svc=$(echo "$svc_obj" | jq -r '.type')
            svc_type=$(echo "$svc_obj" | jq -r '.version')

            # Determine service name and prefix based on type
            case "$svc" in
                postgresql*|mysql*|mariadb*|mongodb*)
                    svc_name="db"
                    env_prefix="DB"
                    ;;
                valkey*|keydb*)
                    svc_name="cache"
                    env_prefix="REDIS"
                    ;;
                rabbitmq*|nats*)
                    svc_name="queue"
                    env_prefix="AMQP"
                    ;;
                elasticsearch*)
                    svc_name="search"
                    env_prefix="ES"
                    ;;
                minio*|objectstorage*)
                    svc_name="storage"
                    env_prefix="S3"
                    ;;
                *)
                    svc_name="$svc"
                    env_prefix=$(echo "$svc" | tr '[:lower:]' '[:upper:]')
                    ;;
            esac

            # CRITICAL: Get env_vars from DISCOVERY data, not hardcoded assumptions
            # Discovery data has format: services.<hostname>.variables = ["cache_hostname", "cache_port", ...]
            # We strip the prefix to get clean var names: ["hostname", "port", ...]
            local discovered_vars
            discovered_vars=$(echo "$discovery_data" | jq -r --arg s "$svc_name" \
                '.services[$s].variables // [] | map(. | split("_") | .[1:] | join("_")) | unique')

            if [ "$discovered_vars" != "[]" ] && [ "$discovered_vars" != "null" ] && [ -n "$discovered_vars" ]; then
                # Use discovered vars (stripped of prefix)
                env_vars="$discovered_vars"
            else
                # Fallback to minimal assumptions only if discovery failed
                # NOTE: These are COMMON vars - actual availability may differ!
                case "$svc" in
                    postgresql*|mysql*|mariadb*|mongodb*)
                        env_vars='["hostname", "port", "connectionString"]'
                        ;;
                    valkey*|keydb*)
                        # CRITICAL: Don't assume password exists for Valkey!
                        env_vars='["hostname", "port", "connectionString"]'
                        ;;
                    rabbitmq*|nats*)
                        env_vars='["hostname", "port", "connectionString"]'
                        ;;
                    *)
                        env_vars='["hostname", "port"]'
                        ;;
                esac
            fi

            # Get reference doc path from service_docs if available
            reference_doc=$(echo "$service_docs_data" | jq -r --arg s "$svc" '.[$s].doc_file // ""')
            [ -z "$reference_doc" ] && reference_doc="${ZCP_TMP_DIR:-/tmp}/service_${svc}.md"

            managed_info=$(echo "$managed_info" | jq \
                --arg n "$svc_name" \
                --arg t "$svc_type" \
                --arg e "$env_prefix" \
                --argjson ev "$env_vars" \
                --arg rd "$reference_doc" \
                '. + [{name: $n, type: $t, env_prefix: $e, env_vars: $ev, reference_doc: $rd}]')
        done < <(echo "$service_objects" | jq -c '.[]')

        # Get per-runtime recipe patterns (Issue 7: prevent cross-contamination)
        local recipe_patterns="$global_recipe_patterns"
        local runtime_recipe_file="${ZCP_TMP_DIR:-/tmp}/recipe_${runtime}.json"
        local runtime_patterns_file="${ZCP_TMP_DIR:-/tmp}/patterns_${runtime}.json"

        # Prefer runtime-specific patterns if available
        if [ -f "$runtime_patterns_file" ]; then
            recipe_patterns=$(cat "$runtime_patterns_file")
        elif [ -f "$runtime_recipe_file" ]; then
            # Try to extract patterns from the recipe file if it's JSON
            local extracted
            extracted=$(jq '.' "$runtime_recipe_file" 2>/dev/null) && recipe_patterns="$extracted"
        fi

        # Build handoff for this service pair (P2: enhanced with recipe_file)
        local handoff
        handoff=$(jq -n \
            --arg dev_host "$dev_host" \
            --arg stage_host "$stage_host" \
            --arg dev_id "$dev_id" \
            --arg stage_id "$stage_id" \
            --arg mount "$mount_path" \
            --arg rt "$runtime" \
            --arg rtv "$runtime_version" \
            --arg rf "$recipe_file" \
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
                recipe_file: $rf,
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

    json_response "finalize" "$msg" "$data" "spawn-subagents"
}

export -f step_finalize
