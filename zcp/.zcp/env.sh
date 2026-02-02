#!/bin/bash
# Zerops Environment Variable Lookup Tool
# Addresses agent failure cascade: "Environment Variable Discovery Failure"
#
# Usage:
#   .zcp/env.sh cache hostname        # Get specific variable from service
#   .zcp/env.sh cache connectionString # Get connection string
#   .zcp/env.sh --list                 # Show all discovered variables
#   .zcp/env.sh --help                 # Show help
#
# THE TRUTH about environment variables:
#   - Shell variables like $cache_hostname DO NOT exist in ZCP shell
#   - ${service}_VAR pattern is for zerops.yml interpolation only
#   - To get actual values: SSH into the service and echo

set -o pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DISCOVERY_FILE="${ZCP_TMP_DIR:-/tmp}/discovery.json"

# ============================================================================
# HELP
# ============================================================================

show_help() {
    cat <<'EOF'
.zcp/env.sh - Environment variable lookup tool

THE TRUTH ABOUT ENVIRONMENT VARIABLES
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

CRITICAL MISUNDERSTANDING TO AVOID:
Shell variables like $cache_hostname DO NOT exist in ZCP shell.

WHERE VARIABLES ACTUALLY LIVE:
┌─────────────────┬────────────────────────────────────────────────┐
│ Location        │ How to access                                   │
├─────────────────┼────────────────────────────────────────────────┤
│ zerops.yml      │ ${service_variableName} - template syntax       │
│ Inside container│ $VARIABLE - via SSH                            │
│ ZCP shell       │ NOWHERE - variables don't exist here!          │
└─────────────────┴────────────────────────────────────────────────┘

This tool SSHes into services to fetch actual values.

USAGE:
  .zcp/env.sh {service} {variable}   # Get specific value
  .zcp/env.sh --list [service]       # List all available variables
  .zcp/env.sh --discovery            # Show discovery.json variables
  .zcp/env.sh --help                 # Show this help

EXAMPLES:
  .zcp/env.sh cache hostname         # Get cache hostname
  .zcp/env.sh db connectionString    # Get database connection string
  .zcp/env.sh appdev zeropsSubdomain # Get service URL
  .zcp/env.sh --list cache           # List all cache service variables

COMMON VARIABLES:
  hostname          Service hostname (for internal networking)
  connectionString  Full connection URL (databases, caches)
  zeropsSubdomain   Full HTTPS URL for the service
  port              Port the service listens on
  user, password    Credentials (databases)

DATABASE/CACHE ACCESS (from ZCP):
  # Get connection string and use it directly
  CONN=$(.zcp/env.sh db connectionString)
  psql "$CONN" -c "SELECT 1"

  # Or use the lib/env.sh functions
  source .zcp/lib/env.sh
  psql "$(env_from db connectionString)"

EOF
}

# ============================================================================
# VARIABLE FETCHING
# ============================================================================

# Get a variable from a service via SSH
get_service_var() {
    local service="$1"
    local var="$2"

    if [ -z "$service" ] || [ -z "$var" ]; then
        echo "Usage: .zcp/env.sh {service} {variable}" >&2
        return 1
    fi

    # Validate service name (security)
    if [[ ! "$service" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "Invalid service name: '$service'" >&2
        return 1
    fi

    # Detect managed services that don't support SSH (case-insensitive)
    local service_lower="${service,,}"  # Convert to lowercase for comparison
    local managed_patterns="^(db|database|postgresql|mysql|mariadb|mongodb|cache|redis|valkey|keydb|queue|rabbitmq|nats|search|elasticsearch|storage|minio)$"
    if [[ "$service_lower" =~ $managed_patterns ]]; then
        # Try to get dev hostname from discovery for helpful error message
        local dev_hostname="your-dev-service"
        if [ -f "$DISCOVERY_FILE" ] && command -v jq &>/dev/null; then
            local discovered_dev
            discovered_dev=$(jq -r '.dev.name // .services[0].dev.name // ""' "$DISCOVERY_FILE" 2>/dev/null)
            [ -n "$discovered_dev" ] && dev_hostname="$discovered_dev"
        fi

        echo >&2
        echo "❌ Cannot SSH into '$service' - it's a managed service (no SSH access)" >&2
        echo >&2
        echo "Managed services don't support SSH. Get env vars from a runtime container:" >&2
        echo >&2
        echo "  ssh $dev_hostname 'echo \$${service}_connectionString'" >&2
        echo >&2
        echo "Or use client tools directly from ZCP:" >&2
        echo "  CONN=\$(ssh $dev_hostname 'echo \$${service}_connectionString')" >&2
        echo "  psql \"\$CONN\" -c \"SELECT 1\"" >&2
        echo >&2
        return 1
    fi

    # SSH to service and echo the variable
    local value
    value=$(ssh "$service" "echo \$$var" 2>/dev/null)

    if [ -z "$value" ]; then
        echo "" >&2
        echo "Variable '$var' is empty or doesn't exist on '$service'" >&2
        echo "" >&2
        echo "Try: .zcp/env.sh --list $service" >&2
        return 1
    fi

    echo "$value"
}

# List available variables from a service
list_service_vars() {
    local service="$1"

    if [ -z "$service" ]; then
        # List variables for all services in discovery
        if [ ! -f "$DISCOVERY_FILE" ]; then
            echo "No discovery.json found. Run: .zcp/workflow.sh init" >&2
            return 1
        fi

        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "DISCOVERED SERVICES"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""

        local count
        count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

        local i=0
        while [ "$i" -lt "$count" ]; do
            local dev_name stage_name
            if [ "$count" -eq 1 ]; then
                dev_name=$(jq -r '.dev.name // ""' "$DISCOVERY_FILE")
                stage_name=$(jq -r '.stage.name // ""' "$DISCOVERY_FILE")
            else
                dev_name=$(jq -r ".services[$i].dev.name // \"\"" "$DISCOVERY_FILE")
                stage_name=$(jq -r ".services[$i].stage.name // \"\"" "$DISCOVERY_FILE")
            fi

            echo "  Dev:   $dev_name"
            echo "  Stage: $stage_name"
            echo ""
            i=$((i + 1))
        done

        echo "To list variables for a service:"
        echo "  .zcp/env.sh --list {service}"
        return 0
    fi

    # Validate service name
    if [[ ! "$service" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "Invalid service name: '$service'" >&2
        return 1
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "VARIABLES FOR: $service"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Try to SSH and get common variables
    # Using a safe subset of common Zerops variables
    local common_vars="hostname serviceId projectId zeropsSubdomain port connectionString user password dbName"

    local found=0
    for var in $common_vars; do
        local value
        value=$(ssh "$service" "echo \$$var" 2>/dev/null)
        if [ -n "$value" ]; then
            # Mask passwords
            if [[ "$var" == *"password"* ]] || [[ "$var" == *"Password"* ]]; then
                echo "  $var = ********"
            elif [[ "$var" == *"connectionString"* ]]; then
                # Show truncated connection string
                echo "  $var = ${value:0:50}..."
            else
                echo "  $var = $value"
            fi
            found=$((found + 1))
        fi
    done

    if [ "$found" -eq 0 ]; then
        echo "  (No common variables found - service may not be accessible)"
        echo ""
        echo "  Try: ssh $service 'env | head -20'"
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "USAGE:"
    echo "  .zcp/env.sh $service hostname"
    echo "  .zcp/env.sh $service connectionString"
    echo ""
    echo "DO NOT USE:"
    echo "  echo \$${service}_hostname  # Does NOT work in ZCP shell!"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# Show discovery.json content
show_discovery() {
    if [ ! -f "$DISCOVERY_FILE" ]; then
        echo "No discovery.json found." >&2
        echo "Run: .zcp/workflow.sh init" >&2
        return 1
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "DISCOVERY.JSON CONTENTS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Pretty print with jq if available
    if command -v jq &>/dev/null; then
        jq '.' "$DISCOVERY_FILE"
    else
        cat "$DISCOVERY_FILE"
    fi
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    case "$1" in
        --help|-h)
            show_help
            exit 0
            ;;
        --list|-l)
            shift
            list_service_vars "$1"
            ;;
        --discovery|-d)
            show_discovery
            ;;
        "")
            show_help
            exit 1
            ;;
        *)
            local service="$1"
            local var="$2"

            if [ -z "$var" ]; then
                echo "Missing variable name" >&2
                echo "Usage: .zcp/env.sh {service} {variable}" >&2
                echo "" >&2
                echo "Examples:" >&2
                echo "  .zcp/env.sh cache hostname" >&2
                echo "  .zcp/env.sh db connectionString" >&2
                exit 1
            fi

            get_service_var "$service" "$var"
            ;;
    esac
}

main "$@"
