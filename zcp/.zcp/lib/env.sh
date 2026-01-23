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

# Validate that a variable exists (non-empty) on a service
# Usage: env_exists <service> <variable_name>
# Returns: 0 if exists and non-empty, 1 otherwise
env_exists() {
    local service="$1"
    local var="$2"

    if [ -z "$service" ] || [ -z "$var" ]; then
        return 1
    fi

    local value
    value=$(ssh "$service" "echo \$$var" 2>/dev/null)
    [ -n "$value" ]
}

# Run a command on ZCP with an environment variable from another service
# Usage: with_env <service> <var_name> <local_var_name> -- <command>
# Example: with_env appdev db_connectionString DB_CONN -- psql "$DB_CONN" -c "SELECT 1"
with_env() {
    local service="$1"
    local remote_var="$2"
    local local_var="$3"
    shift 3

    if [ "$1" = "--" ]; then
        shift
    fi

    local value
    value=$(env_from "$service" "$remote_var")

    if [ -z "$value" ]; then
        echo "Error: $remote_var is empty or not set on $service" >&2
        return 1
    fi

    # Export and run command
    export "$local_var=$value"
    "$@"
    local rc=$?
    unset "$local_var"
    return $rc
}
