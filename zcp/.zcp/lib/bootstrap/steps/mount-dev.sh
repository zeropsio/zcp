#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/mount-dev.sh
# Step: Set up SSHFS mount for dev service(s)
#
# Inputs: hostname (optional - if omitted, mounts ALL dev services from plan)
# Outputs: Mount path(s), writable status
#
# This step delegates to mount.sh for the actual mount operation.

# Mount a single hostname (internal helper)
_mount_single_dev() {
    local hostname="$1"
    local mount_path="/var/www/$hostname"

    # Check if already mounted and accessible
    if [ -d "$mount_path" ] && ls "$mount_path" >/dev/null 2>&1; then
        if mountpoint -q "$mount_path" 2>/dev/null || mount | grep -q "$mount_path"; then
            local writable="false"
            if touch "$mount_path/.zcp_test" 2>/dev/null; then
                rm -f "$mount_path/.zcp_test"
                writable="true"
            fi
            echo "{\"hostname\": \"$hostname\", \"mount_path\": \"$mount_path\", \"writable\": $writable, \"status\": \"already_mounted\"}"
            return 0
        fi
    fi

    # Need to create mount
    local mount_script="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}/mount.sh"
    if [ ! -f "$mount_script" ]; then
        mount_script="$(dirname "${BASH_SOURCE[0]}")/../../../mount.sh"
    fi

    if [ -f "$mount_script" ]; then
        "$mount_script" "$hostname" >/dev/null 2>&1 || true
        sleep 1

        if [ -d "$mount_path" ] && ls "$mount_path" >/dev/null 2>&1; then
            local writable="false"
            if touch "$mount_path/.zcp_test" 2>/dev/null; then
                rm -f "$mount_path/.zcp_test"
                writable="true"
            fi
            echo "{\"hostname\": \"$hostname\", \"mount_path\": \"$mount_path\", \"writable\": $writable, \"status\": \"mounted\"}"
            return 0
        fi
    fi

    echo "{\"hostname\": \"$hostname\", \"mount_path\": \"$mount_path\", \"status\": \"failed\"}"
    return 1
}

step_mount_dev() {
    local hostname="${1:-}"
    local hostnames=()

    # If no hostname provided, get all dev hostnames from plan
    if [ -z "$hostname" ]; then
        local plan
        plan=$(get_plan)

        if [ -z "$plan" ] || [ "$plan" = '{}' ]; then
            json_error "mount-dev" "No plan found - run init first" '{}' '[]'
            return 1
        fi

        # Get dev hostnames (handles both array and single value)
        while IFS= read -r h; do
            [[ -n "$h" ]] && hostnames+=("$h")
        done < <(echo "$plan" | jq -r '.dev_hostnames // [.dev_hostname] | .[]' 2>/dev/null)

        if [ ${#hostnames[@]} -eq 0 ]; then
            json_error "mount-dev" "No dev hostnames found in plan" '{}' '[]'
            return 1
        fi
    else
        hostnames=("$hostname")
    fi

    # Mount all hostnames
    local mounts_data='{"mounts": {}}'
    local all_success=true
    local failed_hosts=()
    local mounted_count=0

    for h in "${hostnames[@]}"; do
        # Validate hostname to prevent path traversal
        if [[ ! "$h" =~ ^[a-zA-Z0-9_-]+$ ]]; then
            failed_hosts+=("$h (invalid format)")
            all_success=false
            continue
        fi

        local result
        result=$(_mount_single_dev "$h")
        local status
        status=$(echo "$result" | jq -r '.status' 2>/dev/null)

        if [[ "$status" == "mounted" ]] || [[ "$status" == "already_mounted" ]]; then
            mounts_data=$(echo "$mounts_data" | jq --arg h "$h" --argjson d "$result" '.mounts[$h] = $d')
            ((mounted_count++))
        else
            failed_hosts+=("$h")
            all_success=false
        fi
    done

    if [ "$all_success" = true ]; then
        local msg="Mounted ${mounted_count} service(s): ${hostnames[*]}"
        json_response "mount-dev" "$msg" "$mounts_data" "discover-services"
    else
        local failed_list
        failed_list=$(printf '%s\n' "${failed_hosts[@]}" | jq -R . | jq -s .)
        json_error "mount-dev" "Failed to mount: ${failed_hosts[*]}" \
            "{\"mounted\": $mounted_count, \"failed\": $failed_list}" \
            '["Check SSH connectivity", "Verify service is running"]'
        return 1
    fi
}

export -f step_mount_dev
