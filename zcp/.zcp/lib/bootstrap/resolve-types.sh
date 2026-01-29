#!/usr/bin/env bash
# .zcp/lib/bootstrap/resolve-types.sh
# Resolves user input to valid Zerops service types using data.json
#
# Uses https://docs.zerops.io/data.json as the AUTHORITATIVE source.
# Only aliases are hardcoded (for user convenience).

set -euo pipefail

DOCS_DATA_URL="https://docs.zerops.io/data.json"
CACHE_FILE="${ZCP_TMP_DIR:-/tmp}/zerops_types.json"
CACHE_TTL=3600  # 1 hour

# Aliases: common names → canonical Zerops names
# This is the ONLY hardcoded mapping - everything else comes from data.json
declare -A ALIASES=(
    ["node"]="nodejs"
    ["js"]="nodejs"
    ["py"]="python"
    ["golang"]="go"
    ["postgres"]="postgresql"
    ["pg"]="postgresql"
    ["redis"]="valkey"
    ["cache"]="valkey"
    ["rabbit"]="rabbitmq"
    ["amqp"]="rabbitmq"
    ["s3"]="objectstorage"
    ["minio"]="objectstorage"
    ["storage"]="objectstorage"
    ["elastic"]="elasticsearch"
    ["es"]="elasticsearch"
    ["maria"]="mariadb"
    ["net"]="dotnet"
)

# Ensure data.json is cached
ensure_data_json() {
    local now cache_time
    now=$(date +%s)

    if [ -f "$CACHE_FILE" ]; then
        # Get file modification time (portable across BSD/GNU stat)
        if stat -c %Y "$CACHE_FILE" >/dev/null 2>&1; then
            # GNU stat (Linux)
            cache_time=$(stat -c %Y "$CACHE_FILE")
        elif stat -f %m "$CACHE_FILE" >/dev/null 2>&1; then
            # BSD stat (macOS)
            cache_time=$(stat -f %m "$CACHE_FILE")
        else
            cache_time=0
        fi
        local age=$((now - cache_time))
        [ "$age" -lt "$CACHE_TTL" ] && return 0
    fi

    curl -sf "$DOCS_DATA_URL" -o "$CACHE_FILE" 2>/dev/null || {
        echo "WARNING: Could not fetch data.json" >&2
        return 1
    }
}

# Check if data.json is available
data_json_available() {
    [ -f "$CACHE_FILE" ] && [ -s "$CACHE_FILE" ]
}

# Check if type exists in data.json
type_exists() {
    local type_name="$1"
    ensure_data_json || return 1
    jq -e --arg t "$type_name" 'has($t)' "$CACHE_FILE" >/dev/null 2>&1
}

# Get default import version from data.json
# Returns first element of first import array: .import[0][0]
get_default_version() {
    local type_name="$1"
    ensure_data_json || return 1
    jq -r --arg t "$type_name" '.[$t].import[0][0] // empty' "$CACHE_FILE"
}

# Get base images array for runtime types
# Returns first base array: .base[0]
get_base_images() {
    local type_name="$1"
    ensure_data_json || return 1
    jq -c --arg t "$type_name" '.[$t].base[0] // []' "$CACHE_FILE"
}

# Check if type has base images (is a runtime that can build code)
is_runtime_type() {
    local type_name="$1"
    ensure_data_json || return 1
    local has_base
    has_base=$(jq -r --arg t "$type_name" '.[$t] | has("base")' "$CACHE_FILE" 2>/dev/null)
    [ "$has_base" = "true" ]
}

# Determine if type is a runtime (can run user code) or managed service
# Uses is_runtime_type() which checks for "base" field in data.json
get_type_category() {
    local type_name="$1"
    ensure_data_json || { echo "unknown"; return; }

    if is_runtime_type "$type_name"; then
        echo "runtime"
    else
        # Has entry in data.json but no base = managed service
        if type_exists "$type_name"; then
            echo "managed"
        else
            echo "unknown"
        fi
    fi
}


# Resolve a type name (apply alias if exists)
resolve_type() {
    local input="$1"
    local input_lower
    input_lower=$(echo "$input" | tr '[:upper:]' '[:lower:]')
    local base_type="${input_lower%%@*}"

    # Apply alias
    local resolved="${ALIASES[$base_type]:-$base_type}"

    # Validate exists
    if type_exists "$resolved"; then
        echo "$resolved"
        return 0
    fi
    return 1
}

# Get suggestions for invalid input
get_suggestions() {
    local input="$1"
    local input_lower
    input_lower=$(echo "$input" | tr '[:upper:]' '[:lower:]')

    ensure_data_json || return

    jq -r --arg i "$input_lower" 'keys[] | select(startswith($i) or contains($i))' "$CACHE_FILE" 2>/dev/null | head -5 | tr '\n' ' '
}

# Main resolution function
# Returns JSON with types, versions, and base images
resolve_types() {
    local input="$*"
    input=$(echo "$input" | tr ',' ' ' | tr '[:upper:]' '[:lower:]')

    # REQUIRE data.json - no fallback mode
    if ! ensure_data_json; then
        jq -n '{
            success: false,
            runtimes: [],
            services: [],
            errors: ["Cannot fetch data.json - network required"],
            warnings: [],
            flags: {runtime: "", services: ""}
        }'
        return 1
    fi

    local runtimes=() services=() errors=() warnings=()
    local runtime_objects='[]'
    local service_objects='[]'

    for term in $input; do
        [[ "$term" == --* ]] && continue
        [ -z "$term" ] && continue

        local base_type="${term%%@*}"

        # Apply alias first
        local resolved="${ALIASES[$base_type]:-$base_type}"
        [ "$base_type" != "$resolved" ] && warnings+=("'$base_type' resolved to '$resolved'")

        # Validate against data.json
        if type_exists "$resolved"; then
            local category version base_json
            category=$(get_type_category "$resolved")
            version=$(get_default_version "$resolved")

            if [ -z "$version" ]; then
                errors+=("No import version found for '$resolved'")
                continue
            fi

            if [ "$category" = "runtime" ]; then
                if [[ ! " ${runtimes[*]:-} " =~ " ${resolved} " ]]; then
                    runtimes+=("$resolved")
                    base_json=$(get_base_images "$resolved")
                    runtime_objects=$(echo "$runtime_objects" | jq \
                        --arg t "$resolved" \
                        --arg v "$version" \
                        --argjson b "$base_json" \
                        '. + [{type: $t, version: $v, base: $b}]')
                fi
            else
                if [[ ! " ${services[*]:-} " =~ " ${resolved} " ]]; then
                    services+=("$resolved")
                    service_objects=$(echo "$service_objects" | jq \
                        --arg t "$resolved" \
                        --arg v "$version" \
                        '. + [{type: $t, version: $v}]')
                fi
            fi
        else
            local suggestions
            suggestions=$(get_suggestions "$term")
            if [ -n "$suggestions" ]; then
                errors+=("Unknown type: '$term'. Try: $suggestions")
            else
                errors+=("Unknown type: '$term'")
            fi
        fi
    done

    # Build JSON output
    local runtimes_csv services_csv
    runtimes_csv=$(printf '%s\n' "${runtimes[@]:-}" | grep -v '^$' | paste -sd ',' -)
    services_csv=$(printf '%s\n' "${services[@]:-}" | grep -v '^$' | paste -sd ',' -)

    local errors_json warnings_json
    errors_json=$(printf '%s\n' "${errors[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')
    warnings_json=$(printf '%s\n' "${warnings[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')

    local success="true"
    [ ${#errors[@]} -gt 0 ] && success="false"
    [ ${#runtimes[@]} -eq 0 ] && { success="false"; errors_json=$(echo "$errors_json" | jq '. + ["No runtimes specified"]'); }

    jq -n \
        --argjson runtimes "$runtime_objects" \
        --argjson services "$service_objects" \
        --argjson errors "$errors_json" \
        --argjson warnings "$warnings_json" \
        --argjson success "$success" \
        --arg rt_csv "$runtimes_csv" \
        --arg svc_csv "$services_csv" \
        '{
            success: $success,
            runtimes: $runtimes,
            services: $services,
            errors: $errors,
            warnings: $warnings,
            flags: {
                runtime: $rt_csv,
                services: $svc_csv
            }
        }'
}

# List all types from data.json
list_types() {
    ensure_data_json || { echo "Could not fetch data.json"; return 1; }

    echo "Available Zerops Service Types (from data.json)"
    echo "================================================"
    echo ""
    echo "ALL TYPES:"
    jq -r 'keys | sort | "  " + join(", ")' "$CACHE_FILE"
    echo ""
    echo "ALIASES (for convenience):"
    for alias in "${!ALIASES[@]}"; do
        printf "  %-12s → %s\n" "$alias" "${ALIASES[$alias]}"
    done | sort
}

# CLI
main() {
    case "${1:-}" in
        --list|-l) list_types ;;
        --help|-h) cat << 'EOF'
Usage: resolve-types.sh [OPTIONS] <types...>

Resolves user input to valid Zerops service types using data.json.

EXAMPLES:
  resolve-types.sh go bun postgres valkey nats
  resolve-types.sh --list

OUTPUT: JSON with resolved runtimes, services, and flags for bootstrap
EOF
            ;;
        "") echo "Usage: resolve-types.sh <types...>" >&2; exit 1 ;;
        *) resolve_types "$@" ;;
    esac
}

export -f ensure_data_json data_json_available type_exists get_default_version get_base_images is_runtime_type get_type_category resolve_type get_suggestions resolve_types

# Only run main if executed directly (not sourced)
# The || true prevents errexit from triggering when sourced
[[ "${BASH_SOURCE[0]}" == "${0}" ]] && main "$@" || true
