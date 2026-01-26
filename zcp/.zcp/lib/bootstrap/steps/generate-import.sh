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

    # Extract parameters from plan
    local runtime services prefix ha_mode

    # Handle both single and multi-runtime plans
    runtime=$(echo "$plan" | jq -r '.runtimes[0] // .runtime // "go"')
    services=$(echo "$plan" | jq -r '.managed_services | join(",")' 2>/dev/null || echo "")
    prefix=$(echo "$plan" | jq -r '.dev_hostname | sub("dev$"; "")' 2>/dev/null || echo "app")
    ha_mode=$(echo "$plan" | jq -r '.ha_mode // false')

    # Build generate_import_yml args
    local gen_args="--runtime $runtime --prefix $prefix"
    [ -n "$services" ] && [ "$services" != "null" ] && gen_args="$gen_args --services $services"
    [ "$ha_mode" = "true" ] && gen_args="$gen_args --ha"

    local import_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml"
    gen_args="$gen_args --output $import_file"

    # Source import-gen.sh and generate
    local import_gen="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/lib/bootstrap/import-gen.sh"
    if [ ! -f "$import_gen" ]; then
        import_gen="$(dirname "${BASH_SOURCE[0]}")/../import-gen.sh"
    fi

    if [ -f "$import_gen" ]; then
        source "$import_gen"
        local result
        result=$(generate_import_yml $gen_args)

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
