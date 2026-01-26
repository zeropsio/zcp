#!/bin/bash
# shellcheck shell=bash
# .zcp/lib/validate.sh
# Input validation functions for security hardening
#
# All validation functions return 0 (success) if input is valid, 1 (failure) otherwise.
# Error messages are sent to stderr.

# ============================================================================
# SERVICE/HOSTNAME VALIDATION
# ============================================================================

# Validate service name (alphanumeric, start with letter, max 63 chars)
# Usage: validate_service_name "appdev" || exit 1
validate_service_name() {
    local name="$1"
    if [[ ! "$name" =~ ^[a-zA-Z][a-zA-Z0-9_-]{0,62}$ ]]; then
        echo "ERROR: Invalid service name: '$name'" >&2
        echo "       Must start with letter, contain only [a-zA-Z0-9_-], max 63 chars" >&2
        return 1
    fi
    return 0
}

# Validate hostname (alphanumeric with hyphens/underscores, no path traversal)
# Usage: validate_hostname "apidev" || exit 1
validate_hostname() {
    local hostname="$1"

    # Check for empty
    if [ -z "$hostname" ]; then
        echo "ERROR: Hostname cannot be empty" >&2
        return 1
    fi

    # Check for path traversal attempts
    if [[ "$hostname" == *".."* ]] || [[ "$hostname" == *"/"* ]]; then
        echo "ERROR: Invalid hostname (path traversal attempt): '$hostname'" >&2
        return 1
    fi

    # Only allow safe characters
    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "ERROR: Invalid hostname: '$hostname'" >&2
        echo "       Must contain only [a-zA-Z0-9_-]" >&2
        return 1
    fi

    return 0
}

# ============================================================================
# NETWORK VALIDATION
# ============================================================================

# Validate port number (1-65535)
# Usage: validate_port "8080" || exit 1
validate_port() {
    local port="$1"

    # Check numeric
    if [[ ! "$port" =~ ^[0-9]+$ ]]; then
        echo "ERROR: Invalid port (not numeric): '$port'" >&2
        return 1
    fi

    # Check range
    if [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
        echo "ERROR: Invalid port (out of range 1-65535): '$port'" >&2
        return 1
    fi

    return 0
}

# Validate SSH hostname (prevents injection via user@host strings)
# Usage: validate_ssh_hostname "appdev" || exit 1
validate_ssh_hostname() {
    local hostname="$1"

    # Must pass basic hostname validation
    if ! validate_hostname "$hostname"; then
        return 1
    fi

    # No @ symbol (prevents user@host injection)
    if [[ "$hostname" == *"@"* ]]; then
        echo "ERROR: Invalid SSH hostname (contains @): '$hostname'" >&2
        return 1
    fi

    # No shell metacharacters
    if [[ "$hostname" == *'$'* ]] || [[ "$hostname" == *'`'* ]] || \
       [[ "$hostname" == *';'* ]] || [[ "$hostname" == *'|'* ]] || \
       [[ "$hostname" == *'&'* ]] || [[ "$hostname" == *'>'* ]] || \
       [[ "$hostname" == *'<'* ]] || [[ "$hostname" == *"'"* ]] || \
       [[ "$hostname" == *'"'* ]]; then
        echo "ERROR: Invalid SSH hostname (shell metacharacters): '$hostname'" >&2
        return 1
    fi

    return 0
}

# ============================================================================
# WORKFLOW VALIDATION
# ============================================================================

# Validate workflow mode
# Usage: validate_mode "full" || exit 1
validate_mode() {
    local mode="$1"
    case "$mode" in
        quick|dev-only|full|hotfix|bootstrap)
            return 0
            ;;
        *)
            echo "ERROR: Invalid workflow mode: '$mode'" >&2
            echo "       Valid modes: quick, dev-only, full, hotfix, bootstrap" >&2
            return 1
            ;;
    esac
}

# Validate workflow phase
# Usage: validate_phase "DEVELOP" || exit 1
validate_phase() {
    local phase="$1"
    case "$phase" in
        INIT|DISCOVER|DEVELOP|DEPLOY|VERIFY|DONE|QUICK)
            return 0
            ;;
        *)
            echo "ERROR: Invalid workflow phase: '$phase'" >&2
            echo "       Valid phases: INIT, DISCOVER, DEVELOP, DEPLOY, VERIFY, DONE, QUICK" >&2
            return 1
            ;;
    esac
}

# ============================================================================
# RUNTIME VALIDATION
# ============================================================================

# Validate runtime type
# Usage: validate_runtime "nodejs" || exit 1
validate_runtime() {
    local runtime="$1"
    case "$runtime" in
        go|nodejs|python|php|rust|bun|java|dotnet|nginx|static|alpine)
            return 0
            ;;
        *)
            echo "ERROR: Invalid runtime type: '$runtime'" >&2
            echo "       Valid types: go, nodejs, python, php, rust, bun, java, dotnet, nginx, static, alpine" >&2
            return 1
            ;;
    esac
}

# Validate prefix (for service naming)
# Usage: validate_prefix "api" || exit 1
validate_prefix() {
    local prefix="$1"

    # Check for empty
    if [ -z "$prefix" ]; then
        echo "ERROR: Prefix cannot be empty" >&2
        return 1
    fi

    # Must be lowercase alphanumeric with optional hyphens
    if [[ ! "$prefix" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]]; then
        echo "ERROR: Invalid prefix: '$prefix'" >&2
        echo "       Must be lowercase alphanumeric, may contain hyphens, cannot start/end with hyphen" >&2
        return 1
    fi

    # Max length (leave room for 'dev'/'stage' suffix)
    if [ ${#prefix} -gt 58 ]; then
        echo "ERROR: Prefix too long (max 58 chars): '$prefix'" >&2
        return 1
    fi

    return 0
}

# ============================================================================
# ID VALIDATION
# ============================================================================

# Validate project/service ID format (typically alphanumeric with hyphens)
# Usage: validate_id "prj-abc123def" || exit 1
validate_id() {
    local id="$1"

    if [ -z "$id" ]; then
        echo "ERROR: ID cannot be empty" >&2
        return 1
    fi

    # IDs are typically alphanumeric with hyphens
    if [[ ! "$id" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "ERROR: Invalid ID format: '$id'" >&2
        echo "       Must contain only [a-zA-Z0-9_-]" >&2
        return 1
    fi

    return 0
}

# Validate environment name (dev or stage)
# Usage: validate_environment "dev" || exit 1
validate_environment() {
    local env="$1"
    case "$env" in
        dev|stage)
            return 0
            ;;
        *)
            echo "ERROR: Invalid environment: '$env'" >&2
            echo "       Valid environments: dev, stage" >&2
            return 1
            ;;
    esac
}

# ============================================================================
# FILE PATH VALIDATION
# ============================================================================

# Validate file path for safe operations (no path traversal)
# Usage: validate_safe_path "/var/www/app/file.txt" || exit 1
validate_safe_path() {
    local path="$1"

    # Check for null bytes
    if [[ "$path" == *$'\0'* ]]; then
        echo "ERROR: Invalid path (null byte): '$path'" >&2
        return 1
    fi

    # Resolve to absolute path and check for traversal
    local resolved
    resolved=$(realpath -m "$path" 2>/dev/null) || {
        echo "ERROR: Invalid path: '$path'" >&2
        return 1
    }

    # Ensure resolved path doesn't contain .. sequences
    if [[ "$resolved" == *".."* ]]; then
        echo "ERROR: Invalid path (traversal detected): '$path'" >&2
        return 1
    fi

    return 0
}

# Validate that a path is within an allowed base directory
# Usage: validate_path_within "/var/www/app/file.txt" "/var/www" || exit 1
validate_path_within() {
    local path="$1"
    local base="$2"

    # Resolve both paths
    local resolved_path resolved_base
    resolved_path=$(realpath -m "$path" 2>/dev/null) || return 1
    resolved_base=$(realpath -m "$base" 2>/dev/null) || return 1

    # Ensure path starts with base
    if [[ "$resolved_path" != "$resolved_base"* ]]; then
        echo "ERROR: Path '$path' is outside allowed directory '$base'" >&2
        return 1
    fi

    return 0
}

# ============================================================================
# JSON KEY VALIDATION
# ============================================================================

# Validate JSON key name (for use in jq filters)
# Usage: validate_json_key "my_key" || exit 1
validate_json_key() {
    local key="$1"

    # Only allow safe characters for JSON keys
    if [[ ! "$key" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]]; then
        echo "ERROR: Invalid JSON key: '$key'" >&2
        echo "       Must start with letter/underscore, contain only [a-zA-Z0-9_]" >&2
        return 1
    fi

    return 0
}

# ============================================================================
# CRYPTOGRAPHIC SESSION ID GENERATION
# ============================================================================

# Generate cryptographically secure session ID
# Usage: session_id=$(generate_secure_session_id)
generate_secure_session_id() {
    local id

    # Try uuidgen first (most portable)
    if command -v uuidgen &>/dev/null; then
        id=$(uuidgen | tr '[:upper:]' '[:lower:]')
    # Try /proc filesystem (Linux)
    elif [ -r /proc/sys/kernel/random/uuid ]; then
        id=$(cat /proc/sys/kernel/random/uuid)
    # Try /dev/urandom with xxd
    elif [ -r /dev/urandom ] && command -v xxd &>/dev/null; then
        id=$(head -c 16 /dev/urandom | xxd -p)
    # Try /dev/urandom with od
    elif [ -r /dev/urandom ] && command -v od &>/dev/null; then
        id=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
    # Fallback: combine multiple entropy sources
    else
        # Combine timestamp (nanoseconds if available), PPID, RANDOM
        local ts
        ts=$(date +%s%N 2>/dev/null || date +%s)
        id="${ts}-$$-${PPID:-0}-$RANDOM$RANDOM$RANDOM$RANDOM"
    fi

    echo "$id"
}

# ============================================================================
# EXPORT FUNCTIONS
# ============================================================================

export -f validate_service_name validate_hostname validate_port validate_ssh_hostname
export -f validate_mode validate_phase validate_runtime validate_prefix
export -f validate_id validate_environment
export -f validate_safe_path validate_path_within validate_json_key
export -f generate_secure_session_id
