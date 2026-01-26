#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/mount-dev.sh
# Step: Set up SSHFS mount for a dev service
#
# Inputs: hostname (positional argument)
# Outputs: Mount path, writable status
#
# This step delegates to mount.sh for the actual mount operation.
# It can be called multiple times for multi-service bootstraps.

step_mount_dev() {
    local hostname="${1:-}"

    if [ -z "$hostname" ]; then
        json_error "mount-dev" "Hostname required" '{}' '["Specify hostname: .zcp/bootstrap.sh step mount-dev apidev"]'
        return 1
    fi

    # HIGH-12: Validate hostname to prevent path traversal
    if [[ ! "$hostname" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        json_error "mount-dev" "Invalid hostname format: $hostname" '{}' '["Hostname must contain only alphanumeric characters, hyphens, and underscores"]'
        return 1
    fi

    local mount_path="/var/www/$hostname"

    # Check if already mounted and accessible
    if [ -d "$mount_path" ] && ls "$mount_path" >/dev/null 2>&1; then
        # Check if it's actually a mount (not just a directory)
        if mountpoint -q "$mount_path" 2>/dev/null || mount | grep -q "$mount_path"; then
            # Test write access
            local writable="false"
            if touch "$mount_path/.zcp_test" 2>/dev/null; then
                rm -f "$mount_path/.zcp_test"
                writable="true"
            fi

            local data
            data=$(jq -n \
                --arg h "$hostname" \
                --arg p "$mount_path" \
                --argjson w "$writable" \
                '{
                    hostname: $h,
                    mount_path: $p,
                    writable: $w
                }')

            # Update mounts in state (aggregate all mounts)
            local mounts_data
            mounts_data=$(get_step_data "mount-dev")
            if [ "$mounts_data" = '{}' ]; then
                mounts_data='{"mounts": {}}'
            fi
            mounts_data=$(echo "$mounts_data" | jq --arg h "$hostname" --argjson d "$data" '.mounts[$h] = $d')
            record_step "mount-dev" "complete" "$mounts_data"

            json_response "mount-dev" "Already mounted: $mount_path" "$data" "finalize"
            return 0
        fi
    fi

    # Need to create mount - call mount.sh
    local mount_script="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/mount.sh"
    if [ ! -f "$mount_script" ]; then
        mount_script="$(dirname "${BASH_SOURCE[0]}")/../../../mount.sh"
    fi

    if [ -f "$mount_script" ]; then
        local mount_output
        mount_output=$("$mount_script" "$hostname" 2>&1) || true

        # Verify mount worked
        sleep 1
        if [ -d "$mount_path" ] && ls "$mount_path" >/dev/null 2>&1; then
            # Test write access
            local writable="false"
            if touch "$mount_path/.zcp_test" 2>/dev/null; then
                rm -f "$mount_path/.zcp_test"
                writable="true"
            fi

            local data
            data=$(jq -n \
                --arg h "$hostname" \
                --arg p "$mount_path" \
                --argjson w "$writable" \
                '{
                    hostname: $h,
                    mount_path: $p,
                    writable: $w
                }')

            # Update mounts in state
            local mounts_data
            mounts_data=$(get_step_data "mount-dev")
            if [ "$mounts_data" = '{}' ]; then
                mounts_data='{"mounts": {}}'
            fi
            mounts_data=$(echo "$mounts_data" | jq --arg h "$hostname" --argjson d "$data" '.mounts[$h] = $d')
            record_step "mount-dev" "complete" "$mounts_data"

            json_response "mount-dev" "Mounted $mount_path" "$data" "finalize"
        else
            # Mount failed
            json_needs_action "mount-dev" "Mount failed for $hostname" "Run: $mount_script $hostname" \
                "{\"hostname\": \"$hostname\", \"mount_path\": \"$mount_path\", \"output\": \"$mount_output\"}"
            return 1
        fi
    else
        # mount.sh not found - provide manual instructions
        json_needs_action "mount-dev" "mount.sh not found" \
            "Run: mkdir -p $mount_path && sudo -E zsc unit create sshfs-$hostname \"sshfs -f -o reconnect $hostname:/var/www $mount_path\"" \
            "{\"hostname\": \"$hostname\", \"mount_path\": \"$mount_path\"}"
        return 1
    fi
}

export -f step_mount_dev
