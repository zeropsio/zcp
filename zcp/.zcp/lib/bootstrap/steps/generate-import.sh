#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/generate-import.sh
# Step: Generate import.yml from plan and recipes
#
# Inputs: Plan (from state), recipe_review.json
# Outputs: import.yml file path

step_generate_import() {
    # Get plan from state
    local plan
    plan=$(get_plan)

    if [ "$plan" = '{}' ]; then
        json_error "generate-import" "No plan found - run plan step first" '{}' '["Run: .zcp/bootstrap.sh step plan --runtime <type>"]'
        return 1
    fi

    # Extract runtime versions from plan objects
    # Plan now has: {runtimes: [{type, version, base}], managed_services: [{type, version}]}
    local runtime_versions service_versions

    # Build runtime version map: {"go": "go@1.22", "bun": "bun@1.2"}
    # Uses null-safe access with fallback to empty object
    runtime_versions=$(echo "$plan" | jq '(.runtimes // []) | [.[] | {(.type): .version}] | add // {}')

    # Build service version map: {"valkey": "valkey@7.2", "postgresql": "postgresql@17"}
    service_versions=$(echo "$plan" | jq '(.managed_services // []) | [.[] | {(.type): .version}] | add // {}')

    # Extract prefixes from dev_hostnames
    local prefixes
    prefixes=$(echo "$plan" | jq -r 'if .dev_hostnames then .dev_hostnames | map(sub("dev$"; "")) | join(",") else "app" end')

    local ha_mode
    ha_mode=$(echo "$plan" | jq -r '.ha_mode // false')

    local import_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml"

    # Source import-gen.sh and generate
    local import_gen="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/lib/bootstrap/import-gen.sh"
    if [ ! -f "$import_gen" ]; then
        import_gen="$(dirname "${BASH_SOURCE[0]}")/../import-gen.sh"
    fi

    if [ -f "$import_gen" ]; then
        source "$import_gen"
        local result
        # Direct function call - avoid eval with JSON arguments
        local gen_ha_flag=""
        [ "$ha_mode" = "true" ] && gen_ha_flag="--ha"

        result=$(generate_import_yml \
            --runtime-versions "$runtime_versions" \
            --service-versions "$service_versions" \
            --prefix "$prefixes" \
            --output "$import_file" \
            $gen_ha_flag)

        if [ -f "$import_file" ]; then
            # Get list of services that will be created
            local service_list
            service_list=$(grep -E "^\s*-?\s*hostname:" "$import_file" | sed 's/.*hostname:\s*//' | tr -d ' "' | tr '\n' ',' | sed 's/,$//')

            local data
            data=$(jq -n \
                --arg f "$import_file" \
                --arg s "$service_list" \
                '{
                    import_file: $f,
                    services: ($s | split(","))
                }')

            record_step "generate-import" "complete" "$data"

            json_response "generate-import" "Generated import.yml with services: $service_list" "$data" "import-services"
        else
            json_error "generate-import" "Failed to create import.yml" '{}' '["Check import-gen.sh", "Verify plan parameters"]'
            return 1
        fi
    else
        json_error "generate-import" "import-gen.sh not found" '{}' '["Check .zcp/lib/bootstrap/import-gen.sh exists"]'
        return 1
    fi
}

export -f step_generate_import
