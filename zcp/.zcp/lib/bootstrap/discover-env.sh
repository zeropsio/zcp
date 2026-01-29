#!/usr/bin/env bash
# discover-env.sh - Dynamically discover available environment variables from running services
#
# Usage: discover-env.sh <dev_hostname> [service1] [service2] ...
# Output: JSON with discovered variables for each managed service
#
# This replaces hardcoded assumptions with actual runtime discovery.
# Instead of assuming what env vars exist (e.g., cache_password), we query
# the actual running services to see what's really available.

set -euo pipefail

# Discover env vars for a single service prefix
discover_service_vars() {
    local dev_hostname="$1"
    local service_hostname="$2"

    # Query all env vars with this service's prefix
    local vars
    vars=$(ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$dev_hostname" \
        "env | grep -E \"^${service_hostname}_\" | cut -d= -f1 | sort" 2>/dev/null || echo "")

    if [ -z "$vars" ]; then
        echo "[]"
        return
    fi

    # Convert to JSON array
    echo "$vars" | jq -R -s 'split("\n") | map(select(length > 0))'
}

# Discover vars for all services and return structured JSON
discover_all_services() {
    local dev_hostname="$1"
    shift
    local service_hostnames=("$@")

    local result='{}'

    for svc in "${service_hostnames[@]}"; do
        [ -z "$svc" ] && continue

        local vars
        vars=$(discover_service_vars "$dev_hostname" "$svc")

        # Categorize the variables for guidance
        local has_password="false"
        local has_user="false"
        local has_connection_string="false"
        local has_db_name="false"
        local has_tls="false"

        echo "$vars" | grep -q "_password" && has_password="true"
        echo "$vars" | grep -q "_user" && has_user="true"
        echo "$vars" | grep -q "_connectionString" && has_connection_string="true"
        echo "$vars" | grep -q "_dbName" && has_db_name="true"
        echo "$vars" | grep -q "_Tls\|_tls\|_SSL\|_ssl" && has_tls="true"

        result=$(echo "$result" | jq \
            --arg svc "$svc" \
            --argjson vars "$vars" \
            --argjson has_pass "$has_password" \
            --argjson has_user "$has_user" \
            --argjson has_conn "$has_connection_string" \
            --argjson has_db "$has_db_name" \
            --argjson has_tls "$has_tls" \
            '.[$svc] = {
                variables: $vars,
                has_password: $has_pass,
                has_user: $has_user,
                has_connection_string: $has_conn,
                has_db_name: $has_db,
                has_tls: $has_tls
            }')
    done

    echo "$result"
}

# Main
main() {
    local dev_hostname="${1:-}"
    shift || true
    local services=("${@:-}")

    if [ -z "$dev_hostname" ]; then
        echo "Usage: discover-env.sh <dev_hostname> [service1] [service2] ..." >&2
        echo "" >&2
        echo "Examples:" >&2
        echo "  discover-env.sh appdev db cache queue" >&2
        echo "  discover-env.sh appdev  # Auto-discover prefixes" >&2
        exit 1
    fi

    # If no services specified, try to discover from env prefixes
    if [ ${#services[@]} -eq 0 ] || [ -z "${services[0]:-}" ]; then
        # Get unique prefixes from environment (excluding system vars)
        # Look for lowercase prefixes ending with underscore, extract base name
        local discovered_prefixes
        discovered_prefixes=$(ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$dev_hostname" \
            'env | grep -oE "^[a-z]+_" | sort -u | sed "s/_$//"' 2>/dev/null || echo "")

        # Filter out common system prefixes
        services=()
        while IFS= read -r prefix; do
            case "$prefix" in
                HOME|USER|PATH|TERM|SHELL|PWD|LANG|LC_*|SSH_*|XDG_*|DISPLAY|LOGNAME|MAIL|HOSTNAME)
                    continue
                    ;;
                zerops*)
                    # Skip zerops internal vars but keep managed service prefixes
                    continue
                    ;;
                *)
                    [ -n "$prefix" ] && services+=("$prefix")
                    ;;
            esac
        done <<< "$discovered_prefixes"
    fi

    if [ ${#services[@]} -eq 0 ]; then
        echo '{"note": "No service prefixes found", "services": {}}'
        exit 0
    fi

    discover_all_services "$dev_hostname" "${services[@]}"
}

main "$@"
