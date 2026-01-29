#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/recipe-search.sh
# Step: Fetch patterns from recipe repository
#
# PARALLELIZED: All fetches run concurrently for speed
# Total time = longest single fetch (~20-30s) instead of sum (~60-120s)

# Fetch a single runtime recipe (called in background)
fetch_runtime_recipe() {
    local runtime="$1"
    local recipe_script="$2"
    local tmp_dir="$3"
    local result_file="${tmp_dir}/recipe_result_${runtime}.json"

    # Run recipe search
    "$recipe_script" quick "$runtime" >/dev/null 2>&1 || true

    # Process result
    if [ -f "${tmp_dir}/fetched_recipe.md" ]; then
        mv "${tmp_dir}/fetched_recipe.md" "${tmp_dir}/recipe_${runtime}.md" 2>/dev/null || true

        # Get version from plan - use null-safe array access with .[]?
        local rt_version
        rt_version=$(get_plan 2>/dev/null | jq -r --arg r "$runtime" '.runtimes[]? | select(.type == $r) | .version // empty' 2>/dev/null)
        [ -z "$rt_version" ] && rt_version="${runtime}@1"

        if [ -f "${tmp_dir}/fetched_patterns.json" ]; then
            mv "${tmp_dir}/fetched_patterns.json" "${tmp_dir}/patterns_${runtime}.json" 2>/dev/null || true
        fi
        echo "{\"runtime\":\"$runtime\",\"version\":\"$rt_version\",\"recipe_file\":\"${tmp_dir}/recipe_${runtime}.md\",\"found\":true}" > "$result_file"
    elif [ -f "${tmp_dir}/fetched_docs.md" ]; then
        mv "${tmp_dir}/fetched_docs.md" "${tmp_dir}/recipe_${runtime}.md" 2>/dev/null || true

        local rt_version
        rt_version=$(get_plan 2>/dev/null | jq -r --arg r "$runtime" '.runtimes[]? | select(.type == $r) | .version // empty' 2>/dev/null)
        [ -z "$rt_version" ] && rt_version="${runtime}@1"

        if [ -f "${tmp_dir}/fetched_patterns.json" ]; then
            mv "${tmp_dir}/fetched_patterns.json" "${tmp_dir}/patterns_${runtime}.json" 2>/dev/null || true
        fi
        echo "{\"runtime\":\"$runtime\",\"version\":\"$rt_version\",\"recipe_file\":\"${tmp_dir}/recipe_${runtime}.md\",\"source\":\"docs\",\"found\":true}" > "$result_file"
    else
        local rt_version
        rt_version=$(get_plan 2>/dev/null | jq -r --arg r "$runtime" '.runtimes[]? | select(.type == $r) | .version // empty' 2>/dev/null)
        [ -z "$rt_version" ] && rt_version="${runtime}@1"
        echo "{\"runtime\":\"$runtime\",\"version\":\"$rt_version\",\"recipe_file\":null,\"source\":\"default\",\"found\":false}" > "$result_file"
    fi
}

# Fetch a single service doc (called in background)
fetch_service_doc() {
    local svc="$1"
    local tmp_dir="$2"
    local result_file="${tmp_dir}/service_result_${svc}.json"

    local svc_doc_url="https://docs.zerops.io/${svc}/overview.md"
    local svc_doc
    svc_doc=$(curl -sf --max-time 15 "$svc_doc_url" 2>/dev/null) || true

    if [ -n "$svc_doc" ]; then
        echo "$svc_doc" > "${tmp_dir}/service_${svc}.md"

        # Get version from plan (passed via environment or state)
        # Use null-safe array access with .[]? to handle empty/null arrays
        local svc_version
        svc_version=$(get_plan 2>/dev/null | jq -r --arg s "$svc" '.managed_services[]? | select(.type == $s) | .version // empty' 2>/dev/null)
        [ -z "$svc_version" ] && svc_version="${svc}@latest"

        # Determine env vars based on service type
        local env_vars='["hostname", "port", "user", "password"]'
        case "$svc" in
            postgresql*|mysql*|mariadb*|mongodb*)
                env_vars='["hostname", "port", "user", "password", "dbName", "connectionString"]'
                ;;
            valkey*|keydb*)
                env_vars='["hostname", "port", "password", "connectionString"]'
                ;;
            rabbitmq*|nats*)
                env_vars='["hostname", "port", "user", "password"]'
                ;;
            elasticsearch*)
                env_vars='["hostname", "port", "user", "password"]'
                ;;
            minio*)
                env_vars='["hostname", "port", "accessKey", "secretKey"]'
                ;;
        esac

        echo "{\"service\":\"$svc\",\"version\":\"$svc_version\",\"doc_file\":\"${tmp_dir}/service_${svc}.md\",\"env_vars\":$env_vars,\"found\":true}" > "$result_file"
    else
        echo "{\"service\":\"$svc\",\"version\":\"${svc}@latest\",\"doc_file\":null,\"source\":\"default\",\"found\":false}" > "$result_file"
    fi
}

export -f fetch_runtime_recipe fetch_service_doc

step_recipe_search() {
    local plan
    plan=$(get_plan)

    if [ "$plan" = '{}' ]; then
        json_error "recipe-search" "No plan found - run plan step first" '{}' '["Run: .zcp/bootstrap.sh step plan --runtime <type>"]'
        return 1
    fi

    # Get ALL runtimes from plan (new format has objects with .type field)
    local runtimes_list
    runtimes_list=$(echo "$plan" | jq -r 'if .runtimes[0].type then .runtimes[].type else (.runtimes // []) | .[] end' 2>/dev/null)
    [ -z "$runtimes_list" ] && runtimes_list=$(echo "$plan" | jq -r '.runtimes[0].type // .runtimes[0] // "go"')

    # Get ALL managed services (new format has objects with .type field)
    local managed_services_list
    managed_services_list=$(echo "$plan" | jq -r 'if .managed_services[0].type then .managed_services[].type else (.managed_services // []) | .[] end' 2>/dev/null)

    # Find recipe-search.sh script
    local recipe_script="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/recipe-search.sh"
    [ ! -f "$recipe_script" ] && recipe_script="$(dirname "${BASH_SOURCE[0]}")/../../../recipe-search.sh"

    local tmp_dir="${ZCP_TMP_DIR:-/tmp}"
    local pids=()
    local runtimes_count=0
    local services_count=0

    # Clean up old result files
    rm -f "${tmp_dir}"/recipe_result_*.json "${tmp_dir}"/service_result_*.json 2>/dev/null

    echo "Starting parallel recipe fetches..." >&2

    if [ -f "$recipe_script" ]; then
        # Launch ALL runtime fetches in parallel
        for runtime in $runtimes_list; do
            runtimes_count=$((runtimes_count + 1))
            echo "  → Fetching recipe: $runtime (background)" >&2
            fetch_runtime_recipe "$runtime" "$recipe_script" "$tmp_dir" &
            pids+=($!)
        done

        # Launch ALL service doc fetches in parallel
        for svc in $managed_services_list; do
            services_count=$((services_count + 1))
            echo "  → Fetching docs: $svc (background)" >&2
            fetch_service_doc "$svc" "$tmp_dir" &
            pids+=($!)
        done

        # Wait for ALL fetches to complete
        echo "Waiting for ${#pids[@]} parallel fetches..." >&2
        for pid in "${pids[@]}"; do
            wait "$pid" 2>/dev/null || true
        done
        echo "All fetches complete." >&2

        # Collect runtime results
        local runtime_recipes='{}'
        local runtimes_found=0
        for runtime in $runtimes_list; do
            local result_file="${tmp_dir}/recipe_result_${runtime}.json"
            if [ -f "$result_file" ]; then
                local found version recipe_file source
                found=$(jq -r '.found' "$result_file" 2>/dev/null)
                version=$(jq -r '.version' "$result_file" 2>/dev/null)
                recipe_file=$(jq -r '.recipe_file' "$result_file" 2>/dev/null)
                source=$(jq -r '.source // "recipe"' "$result_file" 2>/dev/null)

                [ "$found" = "true" ] && runtimes_found=$((runtimes_found + 1))

                runtime_recipes=$(echo "$runtime_recipes" | jq \
                    --arg rt "$runtime" \
                    --arg v "$version" \
                    --arg f "$recipe_file" \
                    --arg s "$source" \
                    '. + {($rt): {version: $v, recipe_file: (if $f == "null" then null else $f end), source: $s}}')

                rm -f "$result_file"
            fi
        done

        # Collect service results
        local service_versions='{}'
        local service_docs='{}'
        for svc in $managed_services_list; do
            local result_file="${tmp_dir}/service_result_${svc}.json"
            if [ -f "$result_file" ]; then
                local version doc_file env_vars
                version=$(jq -r '.version' "$result_file" 2>/dev/null)
                doc_file=$(jq -r '.doc_file' "$result_file" 2>/dev/null)
                env_vars=$(jq -c '.env_vars // []' "$result_file" 2>/dev/null)

                service_versions=$(echo "$service_versions" | jq \
                    --arg s "$svc" --arg v "$version" '. + {($s): $v}')

                service_docs=$(echo "$service_docs" | jq \
                    --arg s "$svc" \
                    --arg v "$version" \
                    --arg f "$doc_file" \
                    --argjson e "$env_vars" \
                    '. + {($s): {version: $v, doc_file: (if $f == "null" then null else $f end), env_vars: $e}}')

                rm -f "$result_file"
            fi
        done

        # Build result
        local data
        data=$(jq -n \
            --argjson rr "$runtime_recipes" \
            --argjson sv "$service_versions" \
            --argjson sd "$service_docs" \
            --arg rp "$runtimes_count" \
            --arg rf "$runtimes_found" \
            '{
                runtime_recipes: $rr,
                service_versions: $sv,
                service_docs: $sd,
                runtimes_processed: ($rp | tonumber),
                runtimes_with_recipes: ($rf | tonumber)
            }')

        record_step "recipe-search" "complete" "$data"

        local msg="Found patterns for ${runtimes_found}/${runtimes_count} runtimes"
        [ -n "$managed_services_list" ] && msg="$msg + ${services_count} service docs"
        msg="$msg (parallel fetch)"

        json_response "recipe-search" "$msg" "$data" "generate-import"
    else
        # Recipe search script not found - use defaults
        local data
        data=$(jq -n '{
            runtime_recipes: {},
            service_versions: {},
            service_docs: {},
            warning: "recipe-search.sh not found - using defaults"
        }')

        record_step "recipe-search" "complete" "$data"
        json_response "recipe-search" "Recipe search skipped (using defaults)" "$data" "generate-import"
    fi
}

export -f step_recipe_search
