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

    # Get runtimes from plan
    local runtimes
    runtimes=$(echo "$plan" | jq -r '.runtimes // [.runtime] | .[]' 2>/dev/null | head -1)

    if [ -z "$runtimes" ]; then
        runtimes=$(echo "$plan" | jq -r '.runtimes[0] // "go"')
    fi

    # Get managed services
    local managed_services
    managed_services=$(echo "$plan" | jq -r '.managed_services // [] | join(" ")')

    # Run recipe-search.sh quick
    local recipe_script="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/recipe-search.sh"

    if [ ! -f "$recipe_script" ]; then
        # Try alternate path
        recipe_script="$(dirname "${BASH_SOURCE[0]}")/../../../recipe-search.sh"
    fi

    if [ -f "$recipe_script" ]; then
        # Run recipe search, capture output but don't fail on warnings
        local search_output
        search_output=$("$recipe_script" quick "$runtimes" $managed_services 2>&1) || true

        # Check if recipe_review.json was created
        local recipe_file="${ZCP_TMP_DIR:-/tmp}/recipe_review.json"

        if [ -f "$recipe_file" ]; then
            # Get runtime version from recipe file
            local runtime_version
            runtime_version=$(jq -r '.patterns_extracted.runtime_patterns | to_entries[0].value.dev_runtime_base // "unknown"' "$recipe_file" 2>/dev/null || echo "unknown")

            # Get managed service versions
            local service_versions='{}'
            local managed_list
            managed_list=$(echo "$plan" | jq -r '.managed_services[]' 2>/dev/null)

            if [ -n "$managed_list" ]; then
                for svc in $managed_list; do
                    local svc_version
                    svc_version=$(source "$SCRIPT_DIR/lib/bootstrap/import-gen.sh" 2>/dev/null && get_service_version "$svc" || echo "${svc}@latest")
                    service_versions=$(echo "$service_versions" | jq --arg s "$svc" --arg v "$svc_version" '. + {($s): $v}')
                done
            fi

            local data
            data=$(jq -n \
                --arg rv "$runtime_version" \
                --argjson sv "$service_versions" \
                --arg pf "$recipe_file" \
                '{
                    runtime_version: $rv,
                    service_versions: $sv,
                    patterns_file: $pf
                }')

            record_step "recipe-search" "complete" "$data"

            json_response "recipe-search" "Found patterns for $runtimes" "$data" "generate-import"
        else
            # Recipe search ran but no file created - might be missing recipe
            local data
            data=$(jq -n \
                --arg rt "$runtimes" \
                '{
                    runtime: $rt,
                    patterns_file: null,
                    warning: "No recipe patterns found - using defaults"
                }')

            record_step "recipe-search" "complete" "$data"

            json_response "recipe-search" "No recipe found for $runtimes (using defaults)" "$data" "generate-import"
        fi
    else
        # Recipe search script not found - use defaults
        local data
        data=$(jq -n \
            --arg rt "$runtimes" \
            '{
                runtime: $rt,
                patterns_file: null,
                warning: "recipe-search.sh not found - using defaults"
            }')

        record_step "recipe-search" "complete" "$data"

        json_response "recipe-search" "Recipe search skipped (using defaults)" "$data" "generate-import"
    fi
}

export -f step_recipe_search
