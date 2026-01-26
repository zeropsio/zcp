#!/usr/bin/env bash
# .zcp/lib/bootstrap/output.sh
# JSON output helpers for bootstrap steps
# All steps follow the same JSON contract for consistent agent parsing

set -euo pipefail

# JSON response for successful step completion
# Usage: json_response "step_name" "message" '{"key": "value"}'
json_response() {
    local step="$1"
    local message="$2"
    local data="${3:-{}}"
    local next="${4:-null}"
    local checkpoint="${5:-$step}"

    # Validate data is valid JSON
    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    # Build next field - quote if string, leave null as-is
    local next_field="null"
    if [ "$next" != "null" ] && [ -n "$next" ]; then
        next_field="\"$next\""
    fi

    jq -n \
        --arg status "complete" \
        --arg step "$step" \
        --arg checkpoint "$checkpoint" \
        --argjson data "$data" \
        --argjson next "$next_field" \
        --arg message "$message" \
        '{
            status: $status,
            step: $step,
            checkpoint: $checkpoint,
            data: $data,
            next: $next,
            message: $message
        }'
}

# JSON response for in-progress step (e.g., wait-services polling)
# Usage: json_progress "step_name" "message" '{"key": "value"}' "next_step"
json_progress() {
    local step="$1"
    local message="$2"
    local data="${3:-{}}"
    local next="${4:-$step}"

    # Validate data is valid JSON
    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    jq -n \
        --arg status "in_progress" \
        --arg step "$step" \
        --argjson data "$data" \
        --arg next "$next" \
        --arg message "$message" \
        '{
            status: $status,
            step: $step,
            data: $data,
            next: $next,
            message: $message
        }'
}

# JSON response for step failure
# Usage: json_error "step_name" "error message" '{"additional": "context"}'
json_error() {
    local step="$1"
    local error_msg="$2"
    local data="${3:-{}}"
    local recovery_options="${4:-[]}"

    # Validate data is valid JSON
    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    # Validate recovery_options is valid JSON array
    if ! echo "$recovery_options" | jq -e 'type == "array"' >/dev/null 2>&1; then
        recovery_options='[]'
    fi

    jq -n \
        --arg status "failed" \
        --arg step "$step" \
        --arg error "$error_msg" \
        --argjson data "$data" \
        --argjson recovery "$recovery_options" \
        '{
            status: $status,
            step: $step,
            data: ($data + {error: $error, recovery_options: $recovery}),
            next: null,
            message: $error
        }'
}

# JSON response for step needing user action
# Usage: json_needs_action "step_name" "message" "action description" '{"context": "data"}'
json_needs_action() {
    local step="$1"
    local message="$2"
    local action_required="$3"
    local data="${4:-{}}"

    # Validate data is valid JSON
    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    jq -n \
        --arg status "needs_action" \
        --arg step "$step" \
        --arg action "$action_required" \
        --argjson data "$data" \
        --arg message "$message" \
        '{
            status: $status,
            step: $step,
            data: ($data + {action_required: $action}),
            next: null,
            message: $message
        }'
}

# Extract JSON from zcli output (skips log lines before JSON, strips ANSI codes)
# zcli outputs log messages before JSON even with --format json
extract_zcli_json() {
    # Strip ANSI codes, then find first line starting with { or [ and output from there
    sed 's/\x1b\[[0-9;]*m//g' | awk '/^\s*[\{\[]/{found=1} found{print}'
}

# Safe JSON merge - combines two JSON objects
# Usage: json_merge '{"a": 1}' '{"b": 2}' -> '{"a": 1, "b": 2}'
json_merge() {
    local base="$1"
    local overlay="$2"

    # Validate both are valid JSON
    if ! echo "$base" | jq -e . >/dev/null 2>&1; then
        base='{}'
    fi
    if ! echo "$overlay" | jq -e . >/dev/null 2>&1; then
        overlay='{}'
    fi

    echo "$base" | jq --argjson overlay "$overlay" '. + $overlay'
}

# Export functions for use in other scripts
export -f json_response json_progress json_error json_needs_action extract_zcli_json json_merge
