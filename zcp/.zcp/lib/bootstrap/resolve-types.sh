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

# Known runtime types (fallback when data.json unavailable)
KNOWN_RUNTIMES="go golang nodejs bun python php rust java dotnet"
KNOWN_SERVICES="postgresql mariadb mysql valkey keydb rabbitmq nats elasticsearch meilisearch typesense kafka clickhouse qdrant objectstorage sharedstorage mongodb"

# Check if type is a known runtime (fallback mode)
is_known_runtime() {
    local type_name="$1"
    [[ " $KNOWN_RUNTIMES " =~ " $type_name " ]]
}

# Check if type is a known service (fallback mode)
is_known_service() {
    local type_name="$1"
    [[ " $KNOWN_SERVICES " =~ " $type_name " ]]
}

# Determine if type is a runtime (can run user code) or managed service
# Runtimes have "build" configuration, managed services don't
get_type_category() {
    local type_name="$1"
    ensure_data_json || { echo "unknown"; return; }

    # Check if it has build-related fields (indicates runtime)
    # Runtimes in data.json typically have "base" arrays for build/run
    local has_base
    has_base=$(jq -r --arg t "$type_name" '.[$t] | has("base")' "$CACHE_FILE" 2>/dev/null || echo "false")

    if [ "$has_base" = "true" ]; then
        # Could be runtime or managed - check if it's a known managed service pattern
        case "$type_name" in
            postgresql|mariadb|valkey|keydb|rabbitmq|nats|elasticsearch|meilisearch|typesense|kafka|clickhouse|qdrant|objectstorage|sharedstorage)
                echo "managed"
                ;;
            *)
                echo "runtime"
                ;;
        esac
    else
        echo "unknown"
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
resolve_types() {
    local input="$*"
    input=$(echo "$input" | tr ',' ' ' | tr '[:upper:]' '[:lower:]')

    # Try to get data.json, but continue in fallback mode if unavailable
    local fallback_mode="false"
    if ! ensure_data_json; then
        fallback_mode="true"
        echo "WARNING: data.json unavailable - using fallback mode" >&2
    fi

    local runtimes=() services=() errors=() warnings=()

    for term in $input; do
        [[ "$term" == --* ]] && continue
        [ -z "$term" ] && continue

        local base_type="${term%%@*}"

        # Apply alias first (always works)
        local resolved="${ALIASES[$base_type]:-$base_type}"
        [ "$term" != "$resolved" ] && warnings+=("'$term' resolved to '$resolved'")

        if [ "$fallback_mode" = "true" ]; then
            # Fallback: use hardcoded known types
            if is_known_runtime "$resolved"; then
                [[ ! " ${runtimes[*]:-} " =~ " ${resolved} " ]] && runtimes+=("$resolved")
            elif is_known_service "$resolved"; then
                [[ ! " ${services[*]:-} " =~ " ${resolved} " ]] && services+=("$resolved")
            else
                # Unknown in fallback mode - assume it's a service (safer default)
                warnings+=("'$resolved' not recognized - treating as service")
                [[ ! " ${services[*]:-} " =~ " ${resolved} " ]] && services+=("$resolved")
            fi
        else
            # Normal mode: validate against data.json
            if type_exists "$resolved"; then
                local category
                category=$(get_type_category "$resolved")

                if [ "$category" = "runtime" ]; then
                    [[ ! " ${runtimes[*]:-} " =~ " ${resolved} " ]] && runtimes+=("$resolved")
                else
                    [[ ! " ${services[*]:-} " =~ " ${resolved} " ]] && services+=("$resolved")
                fi
            else
                local suggestions
                suggestions=$(get_suggestions "$term")
                if [ -n "$suggestions" ]; then
                    errors+=("Unknown: '$term'. Try: $suggestions")
                else
                    errors+=("Unknown: '$term'")
                fi
            fi
        fi
    done

    # Build JSON
    local runtimes_json services_json errors_json warnings_json
    runtimes_json=$(printf '%s\n' "${runtimes[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')
    services_json=$(printf '%s\n' "${services[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')
    errors_json=$(printf '%s\n' "${errors[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')
    warnings_json=$(printf '%s\n' "${warnings[@]:-}" | jq -R . | jq -s 'map(select(. != ""))')

    local success="true"
    [ ${#errors[@]} -gt 0 ] && success="false"
    [ ${#runtimes[@]} -eq 0 ] && { success="false"; errors_json=$(echo "$errors_json" | jq '. + ["No runtimes specified"]'); }

    jq -n \
        --argjson runtimes "$runtimes_json" \
        --argjson services "$services_json" \
        --argjson errors "$errors_json" \
        --argjson warnings "$warnings_json" \
        --argjson success "$success" \
        '{
            success: $success,
            runtimes: $runtimes,
            services: $services,
            errors: $errors,
            warnings: $warnings,
            flags: {
                runtime: ($runtimes | join(",")),
                services: ($services | join(","))
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

export -f ensure_data_json data_json_available type_exists is_known_runtime is_known_service get_type_category resolve_type resolve_types

# Only run main if executed directly (not sourced)
# The || true prevents errexit from triggering when sourced
[[ "${BASH_SOURCE[0]}" == "${0}" ]] && main "$@" || true
