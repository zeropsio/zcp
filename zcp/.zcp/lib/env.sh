#!/bin/bash
# Secure environment variable fetching from SSH services
# Prevents accidental exposure of secrets in command output

# Fetch a single environment variable from a service
# Usage: env_from <service> <variable_name>
# Example: psql "$(env_from appdev db_connectionString)" -c "SELECT 1"
env_from() {
    local service="$1"
    local var="$2"

    if [ -z "$service" ] || [ -z "$var" ]; then
        echo "Usage: env_from <service> <variable_name>" >&2
        return 1
    fi

    # Fetch the variable via SSH - value goes to stdout for substitution
    # Single quotes ensure $var is evaluated on remote, not locally
    ssh "$service" "echo \$$var" 2>/dev/null
}

