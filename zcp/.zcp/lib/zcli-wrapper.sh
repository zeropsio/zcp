#!/usr/bin/env bash
# zcli wrapper with automatic re-authentication
#
# Usage: source this file, then use zcli_with_auth instead of zcli
#
# Example:
#   source .zcp/lib/zcli-wrapper.sh
#   zcli_with_auth service list -P $projectId
#
# This wrapper automatically detects authentication failures and re-authenticates
# before retrying the command. This is useful for long-running bootstrap operations
# where the zcli token may expire.

# Re-authenticate zcli with the ZCP API key
zcli_reauth() {
    if [ -z "$ZEROPS_ZCP_API_KEY" ]; then
        echo "ERROR: ZEROPS_ZCP_API_KEY not set - cannot re-authenticate" >&2
        return 1
    fi

    zcli login \
        --region=gomibako \
        --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" \
        "$ZEROPS_ZCP_API_KEY" 2>/dev/null
}

# Execute zcli command with automatic re-authentication on auth failure
zcli_with_auth() {
    local max_retries=2
    local retry=0
    local output
    local exit_code

    while [ $retry -lt $max_retries ]; do
        output=$(zcli "$@" 2>&1)
        exit_code=$?

        if [ $exit_code -eq 0 ]; then
            echo "$output"
            return 0
        fi

        # Check if it was an auth error
        if echo "$output" | grep -qiE "unauthenticated|unauthorized|login|token|403|401"; then
            echo "zcli authentication expired, re-authenticating..." >&2
            if zcli_reauth; then
                retry=$((retry + 1))
                echo "Re-authenticated successfully, retrying command..." >&2
                continue
            else
                echo "ERROR: zcli re-authentication failed" >&2
                echo "$output"
                return 1
            fi
        fi

        # Not an auth error, return the original error
        echo "$output"
        return $exit_code
    done

    echo "ERROR: zcli authentication failed after $max_retries retries" >&2
    return 1
}

# Execute zcli command inside a container via SSH with automatic re-auth
# Usage: zcli_ssh_with_auth <hostname> <zcli_args...>
zcli_ssh_with_auth() {
    local hostname="$1"
    shift
    local zcli_args="$*"

    local max_retries=2
    local retry=0
    local output
    local exit_code

    while [ $retry -lt $max_retries ]; do
        output=$(ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=no "$hostname" \
            "zcli $zcli_args" 2>&1)
        exit_code=$?

        if [ $exit_code -eq 0 ]; then
            echo "$output"
            return 0
        fi

        # Check if it was an auth error
        if echo "$output" | grep -qiE "unauthenticated|unauthorized|login|token|403|401"; then
            echo "zcli authentication expired in container, re-authenticating..." >&2

            # Re-authenticate inside the container
            local auth_result
            auth_result=$(ssh -o ConnectTimeout=10 -o StrictHostKeyChecking=no "$hostname" \
                'zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "$ZEROPS_ZCP_API_KEY"' 2>&1)

            if [ $? -eq 0 ]; then
                retry=$((retry + 1))
                echo "Re-authenticated successfully, retrying command..." >&2
                continue
            else
                echo "ERROR: zcli re-authentication failed in container" >&2
                echo "$auth_result" >&2
                echo "$output"
                return 1
            fi
        fi

        # Not an auth error, return the original error
        echo "$output"
        return $exit_code
    done

    echo "ERROR: zcli authentication failed after $max_retries retries" >&2
    return 1
}

# Check if zcli is authenticated (returns 0 if authenticated)
zcli_check_auth() {
    local test_output
    test_output=$(zcli project list --format json 2>&1)

    if echo "$test_output" | grep -qiE "unauthenticated|unauthorized|login|token|403|401"; then
        return 1
    fi
    return 0
}

# Ensure zcli is authenticated, re-auth if needed
zcli_ensure_auth() {
    if ! zcli_check_auth; then
        echo "zcli not authenticated, authenticating..." >&2
        zcli_reauth
        return $?
    fi
    return 0
}

export -f zcli_with_auth zcli_ssh_with_auth zcli_reauth zcli_check_auth zcli_ensure_auth
