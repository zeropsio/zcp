#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/recipe-search.sh
# Step: Fetch patterns from recipe repository
#
# Inputs: Plan (from state)
# Outputs: recipe_review.json with runtime patterns

step_recipe_search() {
    # Get plan from state
    local plan
    plan=$(get_plan)

    if [ "$plan" = '{}' ]; then
        json_error "recipe-search" "No plan found - run plan step first" '{}' '["Run: .zcp/bootstrap.sh step plan --runtime <type>"]'
        return 1
    fi

    # Get ALL runtimes from plan (P0 FIX: removed head -1)
    local runtimes_list
    runtimes_list=$(echo "$plan" | jq -r '.runtimes // [.runtime] | .[]' 2>/dev/null)

    if [ -z "$runtimes_list" ]; then
        runtimes_list=$(echo "$plan" | jq -r '.runtimes[0] // "go"')
    fi

    # Get ALL managed services
    local managed_services_list
    managed_services_list=$(echo "$plan" | jq -r '.managed_services // [] | .[]' 2>/dev/null)

    # Find recipe-search.sh script
    local recipe_script="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/recipe-search.sh"
    if [ ! -f "$recipe_script" ]; then
        recipe_script="$(dirname "${BASH_SOURCE[0]}")/../../../recipe-search.sh"
    fi

    local tmp_dir="${ZCP_TMP_DIR:-/tmp}"
    local runtime_recipes='{}'
    local service_versions='{}'
    local service_docs='{}'
    local runtimes_processed=0
    local runtimes_found=0

    if [ -f "$recipe_script" ]; then
        # P0 FIX: Loop through ALL runtimes
        for runtime in $runtimes_list; do
            runtimes_processed=$((runtimes_processed + 1))
            echo "Fetching recipe for runtime: $runtime" >&2

            # Run recipe search for this runtime
            local search_output
            search_output=$("$recipe_script" quick "$runtime" 2>&1) || true

            # Check if recipe was fetched
            if [ -f "${tmp_dir}/fetched_recipe.md" ]; then
                # Move to runtime-specific file
                mv "${tmp_dir}/fetched_recipe.md" "${tmp_dir}/recipe_${runtime}.md" 2>/dev/null || true
                runtimes_found=$((runtimes_found + 1))

                # Get version from patterns
                local rt_version
                if [ -f "${tmp_dir}/fetched_patterns.json" ]; then
                    rt_version=$(jq -r '.runtime_base // "'"${runtime}@1"'"' "${tmp_dir}/fetched_patterns.json" 2>/dev/null)
                    mv "${tmp_dir}/fetched_patterns.json" "${tmp_dir}/patterns_${runtime}.json" 2>/dev/null || true
                else
                    rt_version="${runtime}@1"
                fi

                runtime_recipes=$(echo "$runtime_recipes" | jq \
                    --arg rt "$runtime" \
                    --arg v "$rt_version" \
                    --arg f "${tmp_dir}/recipe_${runtime}.md" \
                    '. + {($rt): {version: $v, recipe_file: $f}}')
            elif [ -f "${tmp_dir}/fetched_docs.md" ]; then
                # Docs fallback
                mv "${tmp_dir}/fetched_docs.md" "${tmp_dir}/recipe_${runtime}.md" 2>/dev/null || true
                runtimes_found=$((runtimes_found + 1))

                local rt_version="${runtime}@1"
                if [ -f "${tmp_dir}/fetched_patterns.json" ]; then
                    rt_version=$(jq -r '.runtime_base // "'"${runtime}@1"'"' "${tmp_dir}/fetched_patterns.json" 2>/dev/null)
                    mv "${tmp_dir}/fetched_patterns.json" "${tmp_dir}/patterns_${runtime}.json" 2>/dev/null || true
                fi

                runtime_recipes=$(echo "$runtime_recipes" | jq \
                    --arg rt "$runtime" \
                    --arg v "$rt_version" \
                    --arg f "${tmp_dir}/recipe_${runtime}.md" \
                    '. + {($rt): {version: $v, recipe_file: $f, source: "docs"}}')
            else
                # No recipe found for this runtime
                runtime_recipes=$(echo "$runtime_recipes" | jq \
                    --arg rt "$runtime" \
                    '. + {($rt): {version: ($rt + "@1"), recipe_file: null, source: "default"}}')
            fi
        done

        # P1 FIX: Fetch docs for ALL managed services
        for svc in $managed_services_list; do
            echo "Fetching docs for managed service: $svc" >&2

            local svc_doc_url="https://docs.zerops.io/${svc}/overview.md"
            local svc_doc
            svc_doc=$(curl -sf "$svc_doc_url" 2>/dev/null)

            if [ -n "$svc_doc" ]; then
                echo "$svc_doc" > "${tmp_dir}/service_${svc}.md"

                # Extract version from docs
                local svc_version
                svc_version=$(echo "$svc_doc" | grep -oE "${svc}@[0-9a-z.]+" | head -1)
                [ -z "$svc_version" ] && svc_version="${svc}@latest"

                # Extract env vars pattern from docs
                local env_vars='[]'
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

                service_docs=$(echo "$service_docs" | jq \
                    --arg s "$svc" \
                    --arg v "$svc_version" \
                    --arg f "${tmp_dir}/service_${svc}.md" \
                    --argjson e "$env_vars" \
                    '. + {($s): {version: $v, doc_file: $f, env_vars: $e}}')

                service_versions=$(echo "$service_versions" | jq \
                    --arg s "$svc" --arg v "$svc_version" '. + {($s): $v}')
            else
                # Doc fetch failed - use defaults
                local svc_version
                svc_version=$(source "${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/lib/bootstrap/import-gen.sh" 2>/dev/null && get_service_version "$svc" || echo "${svc}@latest")

                service_versions=$(echo "$service_versions" | jq \
                    --arg s "$svc" --arg v "$svc_version" '. + {($s): $v}')
                service_docs=$(echo "$service_docs" | jq \
                    --arg s "$svc" \
                    --arg v "$svc_version" \
                    '. + {($s): {version: $v, doc_file: null, source: "default"}}')
            fi
        done

        # Build comprehensive result
        local data
        data=$(jq -n \
            --argjson rr "$runtime_recipes" \
            --argjson sv "$service_versions" \
            --argjson sd "$service_docs" \
            --arg rp "$runtimes_processed" \
            --arg rf "$runtimes_found" \
            '{
                runtime_recipes: $rr,
                service_versions: $sv,
                service_docs: $sd,
                runtimes_processed: ($rp | tonumber),
                runtimes_with_recipes: ($rf | tonumber)
            }')

        record_step "recipe-search" "complete" "$data"

        local msg="Found patterns for ${runtimes_found}/${runtimes_processed} runtimes"
        [ -n "$managed_services_list" ] && msg="$msg, fetched docs for managed services"

        json_response "recipe-search" "$msg" "$data" "generate-import"
    else
        # Recipe search script not found - use defaults
        local data
        data=$(jq -n \
            --arg rt "$(echo "$runtimes_list" | head -1)" \
            '{
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
