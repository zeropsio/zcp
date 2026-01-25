#!/usr/bin/env bash
# .zcp/lib/bootstrap/detect.sh
# Project state detection for bootstrap flow

detect_project_state() {
    # Prerequisite check
    if [ -z "$projectId" ]; then
        echo "ERROR"
        echo "projectId environment variable not set" >&2
        return 1
    fi

    # Get services with proper error handling
    # Strip ANSI codes and log lines that zcli outputs (breaks JSON parsing)
    local raw_output exit_code services
    raw_output=$(zcli service list -P "$projectId" --format json 2>&1)
    exit_code=$?

    if [ $exit_code -ne 0 ]; then
        echo "ERROR"
        echo "zcli failed: $raw_output" >&2
        return 1
    fi

    # Extract JSON (skip log lines before JSON, strip ANSI)
    services=$(echo "$raw_output" | sed 's/\x1b\[[0-9;]*m//g' | awk '/^\s*[\{\[]/{found=1} found{print}')

    # Handle empty/null
    local services_arr
    services_arr=$(echo "$services" | jq '.services // []' 2>/dev/null)
    if [ -z "$services_arr" ] || [ "$services_arr" = "null" ] || [ "$services_arr" = "[]" ]; then
        echo "FRESH"
        return 0
    fi

    # Count runtime services
    local runtime_pattern="^(go|nodejs|php|python|rust|bun|dotnet|java|nginx|static|alpine)@"
    local runtime_count
    runtime_count=$(echo "$services" | jq --arg p "$runtime_pattern" \
        '[.services[] | select(.type | test($p))] | length')

    if [ "$runtime_count" -eq 0 ]; then
        echo "FRESH"
        return 0
    fi

    # Check for actual dev/stage PAIRS (not just any dev + any stage)
    local has_pairs
    has_pairs=$(echo "$services" | jq '
        [.services[] | .name | select(test("dev$")) | sub("dev$"; "")] as $dev_prefixes |
        [.services[] | .name | select(test("stage$")) | sub("stage$"; "")] as $stage_prefixes |
        [$dev_prefixes[] | select(. as $p | $stage_prefixes | index($p) != null)] | length > 0
    ')

    if [ "$has_pairs" = "true" ]; then
        echo "CONFORMANT"
    else
        echo "NON_CONFORMANT"
    fi
}

# Export for use in other scripts
export -f detect_project_state
