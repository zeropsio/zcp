#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/import-services.sh
# Step: Import services via zcli
#
# Inputs: import.yml (from generate-import step)
# Outputs: Import result, service IDs

step_import_services() {
    # Get import file from previous step
    local import_step_data
    import_step_data=$(get_step_data "generate-import")

    local import_file
    import_file=$(echo "$import_step_data" | jq -r '.import_file // ""')

    if [ -z "$import_file" ] || [ ! -f "$import_file" ]; then
        import_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml"
    fi

    if [ ! -f "$import_file" ]; then
        json_error "import-services" "import.yml not found - run generate-import step first" '{}' '["Run: .zcp/bootstrap.sh step generate-import"]'
        return 1
    fi

    # Check projectId
    if [ -z "${projectId:-}" ]; then
        json_error "import-services" "projectId not set - are you running inside ZCP?" '{}' '["Set projectId environment variable", "Run from ZCP environment"]'
        return 1
    fi

    # Get plan to check if services already exist
    local plan
    plan=$(get_plan)

    # Get ALL dev and stage hostnames (support both array and singular forms)
    local dev_hostnames stage_hostnames
    dev_hostnames=$(echo "$plan" | jq -r 'if .dev_hostnames then .dev_hostnames[] else .dev_hostname // "appdev" end' 2>/dev/null)
    stage_hostnames=$(echo "$plan" | jq -r 'if .stage_hostnames then .stage_hostnames[] else .stage_hostname // "appstage" end' 2>/dev/null)

    # Check for existing services
    local existing_services
    existing_services=$(zcli service list -P "$projectId" --format json 2>&1 | extract_zcli_json)

    # Check if ALL dev/stage hostnames already exist
    local all_exist=true
    local existing_list=()

    for dev_hostname in $dev_hostnames; do
        local dev_exists
        dev_exists=$(echo "$existing_services" | jq -r --arg h "$dev_hostname" '.services[] | select(.name == $h) | .name' 2>/dev/null || echo "")
        if [ -z "$dev_exists" ]; then
            all_exist=false
        else
            existing_list+=("$dev_hostname")
        fi
    done

    for stage_hostname in $stage_hostnames; do
        local stage_exists
        stage_exists=$(echo "$existing_services" | jq -r --arg h "$stage_hostname" '.services[] | select(.name == $h) | .name' 2>/dev/null || echo "")
        if [ -z "$stage_exists" ]; then
            all_exist=false
        else
            existing_list+=("$stage_hostname")
        fi
    done

    if [ "$all_exist" = true ] && [ ${#existing_list[@]} -gt 0 ]; then
        # All services already exist - skip import
        local existing_json
        existing_json=$(printf '%s\n' "${existing_list[@]}" | jq -R . | jq -s .)

        local data
        data=$(jq -n \
            --argjson existing "$existing_json" \
            '{
                import_result: "skipped",
                reason: "Services already exist",
                services_existing: $existing
            }')

        json_response "import-services" "All services already exist (${existing_list[*]})" "$data" "wait-services"
        return 0
    fi

    # Run zcli import
    local import_output import_exit
    import_output=$(zcli project service-import "$import_file" -P "$projectId" 2>&1) || import_exit=$?

    # EOF errors are normal with service-import, check if import actually worked
    if [ "${import_exit:-0}" -ne 0 ]; then
        # Check if it's just an EOF error (which is OK)
        if echo "$import_output" | grep -qiE "EOF|unexpected end"; then
            # This is normal - continue
            :
        elif echo "$import_output" | grep -qiE "already exists"; then
            # Services already exist
            local data
            data=$(jq -n \
                '{
                    import_result: "already_exists",
                    services_created: 0
                }')

            json_response "import-services" "Services already exist" "$data" "wait-services"
            return 0
        else
            # Real error
            json_error "import-services" "zcli import failed: $import_output" '{}' '["Check import.yml syntax", "Verify zcli authentication", "Check project quota"]'
            return 1
        fi
    fi

    # Count services created
    local expected_services
    expected_services=$(echo "$import_step_data" | jq -r '.services | length' 2>/dev/null || echo "0")

    local data
    data=$(jq -n \
        --arg result "success" \
        --argjson count "${expected_services:-3}" \
        '{
            import_result: $result,
            services_created: $count
        }')

    json_response "import-services" "Import initiated for $expected_services services" "$data" "wait-services"
}

export -f step_import_services
