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

    local dev_hostname stage_hostname
    dev_hostname=$(echo "$plan" | jq -r '.dev_hostname // "appdev"')
    stage_hostname=$(echo "$plan" | jq -r '.stage_hostname // "appstage"')

    # Check for existing services
    local existing_services
    existing_services=$(zcli service list -P "$projectId" --format json 2>&1 | extract_zcli_json)

    local dev_exists stage_exists
    dev_exists=$(echo "$existing_services" | jq -r --arg h "$dev_hostname" '.services[] | select(.name == $h) | .name' 2>/dev/null || echo "")
    stage_exists=$(echo "$existing_services" | jq -r --arg h "$stage_hostname" '.services[] | select(.name == $h) | .name' 2>/dev/null || echo "")

    if [ -n "$dev_exists" ] && [ -n "$stage_exists" ]; then
        # Services already exist - skip import
        local data
        data=$(jq -n \
            --arg dev "$dev_hostname" \
            --arg stage "$stage_hostname" \
            '{
                import_result: "skipped",
                reason: "Services already exist",
                services_existing: [$dev, $stage]
            }')

        record_step "import-services" "complete" "$data"

        json_response "import-services" "Services already exist ($dev_hostname, $stage_hostname)" "$data" "wait-services"
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

            record_step "import-services" "complete" "$data"
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

    record_step "import-services" "complete" "$data"

    json_response "import-services" "Import initiated for $expected_services services" "$data" "wait-services"
}

export -f step_import_services
