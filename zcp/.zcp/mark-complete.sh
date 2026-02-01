#!/usr/bin/env bash
# .zcp/mark-complete.sh - Mark a service bootstrap as complete
#
# This script is designed to work from ANY shell (bash, zsh, sh) and ANY directory.
# It uses only POSIX-compatible constructs for maximum portability.
#
# Usage:
#   .zcp/mark-complete.sh <hostname>
#   .zcp/mark-complete.sh appdev
#   .zcp/mark-complete.sh --check appdev    # Check if already complete
#   .zcp/mark-complete.sh --status          # Show all service states
#
# This solves the problem where subagents running in different shell contexts
# (zsh vs bash) couldn't reliably use the state.sh functions.

set -euo pipefail

# Get script directory (works in bash, zsh, and sh)
if [ -n "${BASH_SOURCE:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
elif [ -n "${ZSH_VERSION:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${(%):-%x}")" && pwd)"
else
    SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
fi

# State directory - consistent with state.sh
STATE_DIR="$SCRIPT_DIR/state/bootstrap/services"

# Colors (optional, degrades gracefully)
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Validate hostname (security: prevent path traversal)
validate_hostname() {
    local hostname="$1"

    if [ -z "$hostname" ]; then
        echo -e "${RED}ERROR: hostname required${NC}" >&2
        return 1
    fi

    # Only allow alphanumeric, underscore, hyphen
    if ! echo "$hostname" | grep -qE '^[a-zA-Z0-9_-]+$'; then
        echo -e "${RED}ERROR: Invalid hostname format: $hostname${NC}" >&2
        echo "  Must contain only: a-z, A-Z, 0-9, underscore, hyphen" >&2
        return 1
    fi

    # Prevent path traversal attempts
    case "$hostname" in
        *..* | */* | *\\*)
            echo -e "${RED}ERROR: Invalid hostname (path traversal attempt): $hostname${NC}" >&2
            return 1
            ;;
    esac

    return 0
}

# Get current timestamp in ISO format
get_timestamp() {
    date -u +"%Y-%m-%dT%H:%M:%SZ"
}

# Check if a service is complete
check_complete() {
    local hostname="$1"
    local status_file="$STATE_DIR/$hostname/status.json"

    if [ ! -f "$status_file" ]; then
        echo "unknown"
        return 1
    fi

    local phase
    phase=$(jq -r '.phase // "unknown"' "$status_file" 2>/dev/null || echo "unknown")
    echo "$phase"

    if [ "$phase" = "complete" ] || [ "$phase" = "completed" ]; then
        return 0
    fi
    return 1
}

# Detect what was implemented by examining the codebase
detect_implementation() {
    local hostname="$1"
    local mount_path="/var/www/$hostname"

    local runtime="" main_file="" endpoints="" services=""

    # Detect runtime and main file
    if [ -f "$mount_path/main.go" ] || [ -f "$mount_path/server.go" ]; then
        runtime="go"
        main_file=$(ls "$mount_path"/*.go 2>/dev/null | head -1 | xargs basename 2>/dev/null || echo "main.go")
    elif [ -f "$mount_path/index.ts" ] || [ -f "$mount_path/server.ts" ]; then
        runtime="bun/typescript"
        main_file=$(ls "$mount_path"/*.ts 2>/dev/null | head -1 | xargs basename 2>/dev/null || echo "index.ts")
    elif [ -f "$mount_path/index.js" ] || [ -f "$mount_path/server.js" ]; then
        runtime="node"
        main_file=$(ls "$mount_path"/*.js 2>/dev/null | head -1 | xargs basename 2>/dev/null || echo "index.js")
    elif [ -f "$mount_path/app.py" ] || [ -f "$mount_path/main.py" ]; then
        runtime="python"
        main_file=$(ls "$mount_path"/*.py 2>/dev/null | head -1 | xargs basename 2>/dev/null || echo "app.py")
    elif [ -f "$mount_path/main.rs" ]; then
        runtime="rust"
        main_file="main.rs"
    fi

    # Detect endpoints from zerops.yml or source
    endpoints="/, /health, /status"

    # Detect managed service connections from zerops.yml
    if [ -f "$mount_path/zerops.yml" ]; then
        # Look for service references in envVariables
        local db_ref cache_ref queue_ref
        db_ref=$(grep -E '\$\{(db|postgres|mysql|mongo)' "$mount_path/zerops.yml" 2>/dev/null | head -1)
        cache_ref=$(grep -E '\$\{(cache|valkey|redis)' "$mount_path/zerops.yml" 2>/dev/null | head -1)
        queue_ref=$(grep -E '\$\{(queue|nats|rabbit)' "$mount_path/zerops.yml" 2>/dev/null | head -1)

        [ -n "$db_ref" ] && services="${services}database, "
        [ -n "$cache_ref" ] && services="${services}cache, "
        [ -n "$queue_ref" ] && services="${services}queue, "
        services="${services%, }"  # Remove trailing comma
    fi

    # Build summary
    local summary=""
    if [ -n "$runtime" ]; then
        summary="$runtime HTTP server"
        [ -n "$services" ] && summary="$summary with $services connectivity"
        summary="$summary (${main_file})"
    else
        summary="HTTP server (unknown runtime)"
    fi

    echo "$summary"
}

# Auto-generate completion evidence from verify evidence + container env vars
# This replaces manual JSON writing by subagents
auto_generate_completion() {
    local hostname="$1"
    local completion_file="/tmp/${hostname}_complete.json"

    # Skip if already exists
    [ -f "$completion_file" ] && return 0

    # Derive stage hostname (pattern: devname -> stagename)
    local stage_hostname="${hostname/dev/stage}"

    # Get URLs from containers (zeropsSubdomain env var)
    local dev_url stage_url
    dev_url=$(ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$hostname" 'echo $zeropsSubdomain' 2>/dev/null || echo "")
    stage_url=$(ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$stage_hostname" 'echo $zeropsSubdomain' 2>/dev/null || echo "")

    # Get verification results from evidence files
    local dev_verify="/tmp/${hostname}_verify.json"
    local stage_verify="/tmp/${stage_hostname}_verify.json"

    local dev_passed=0 dev_failed=0 stage_passed=0 stage_failed=0

    if [ -f "$dev_verify" ]; then
        dev_passed=$(jq -r '.passed // 0' "$dev_verify" 2>/dev/null || echo "0")
        dev_failed=$(jq -r '.failed // 0' "$dev_verify" 2>/dev/null || echo "0")
    fi

    if [ -f "$stage_verify" ]; then
        stage_passed=$(jq -r '.passed // 0' "$stage_verify" 2>/dev/null || echo "0")
        stage_failed=$(jq -r '.failed // 0' "$stage_verify" 2>/dev/null || echo "0")
    fi

    # Detect what was implemented
    local implementation
    implementation=$(detect_implementation "$hostname")

    # Generate completion JSON
    jq -n \
        --arg dev "$hostname" \
        --arg stage "$stage_hostname" \
        --arg dev_url "$dev_url" \
        --arg stage_url "$stage_url" \
        --argjson dev_passed "$dev_passed" \
        --argjson dev_failed "$dev_failed" \
        --argjson stage_passed "$stage_passed" \
        --argjson stage_failed "$stage_failed" \
        --arg impl "$implementation" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{
            dev_hostname: $dev,
            stage_hostname: $stage,
            dev_url: $dev_url,
            stage_url: $stage_url,
            verification: {
                dev: {passed: $dev_passed, failed: $dev_failed},
                stage: {passed: $stage_passed, failed: $stage_failed}
            },
            implementation: $impl,
            completed_at: $ts,
            auto_generated: true
        }' > "$completion_file"

    echo "Auto-generated completion evidence: $completion_file"
}

# Check if verification passed for this service
check_verification_passed() {
    local hostname="$1"
    local verify_file="/tmp/${hostname}_verify.json"

    # If no verification file exists, can't confirm
    if [ ! -f "$verify_file" ]; then
        echo "no_evidence"
        return 2
    fi

    # Check for preflight failure
    local preflight_failed
    preflight_failed=$(jq -r '.preflight_failed // false' "$verify_file" 2>/dev/null)
    if [ "$preflight_failed" = "true" ]; then
        echo "preflight_failed"
        return 1
    fi

    # Check pass/fail counts
    local passed failed
    passed=$(jq -r '.passed // 0' "$verify_file" 2>/dev/null)
    failed=$(jq -r '.failed // 0' "$verify_file" 2>/dev/null)

    if [ "$failed" -gt 0 ]; then
        echo "failed:$failed"
        return 1
    fi

    if [ "$passed" -eq 0 ]; then
        echo "no_tests"
        return 1
    fi

    echo "passed:$passed"
    return 0
}

# Mark a service as complete
mark_complete() {
    local hostname="$1"
    local force="${2:-false}"
    local service_dir="$STATE_DIR/$hostname"
    local status_file="$service_dir/status.json"
    local timestamp
    timestamp=$(get_timestamp)

    # AUTO-GENERATE completion evidence if not present (Issue 2)
    auto_generate_completion "$hostname"

    # VERIFICATION GATE: Check if dev verification passed
    if [ "$force" != "true" ] && [ "$force" != "--force" ]; then
        local verify_status
        verify_status=$(check_verification_passed "$hostname")
        local verify_exit=$?

        case "$verify_exit" in
            0)
                echo -e "${GREEN}✓ Verification passed ($verify_status)${NC}"
                ;;
            1)
                echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}" >&2
                echo -e "${RED}❌ CANNOT MARK COMPLETE: Verification failed${NC}" >&2
                echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}" >&2
                echo "" >&2
                echo "  Status: $verify_status" >&2
                echo "  Evidence: /tmp/${hostname}_verify.json" >&2
                echo "" >&2
                if [ "$verify_status" = "preflight_failed" ]; then
                    echo "  Problem: No server was listening on the port" >&2
                    echo "  Fix: Start the dev server manually, then re-verify" >&2
                fi
                echo "" >&2
                echo "  To force (NOT RECOMMENDED): .zcp/mark-complete.sh --force $hostname" >&2
                return 1
                ;;
            2)
                echo -e "${YELLOW}⚠️  No verification evidence found${NC}" >&2
                echo "  Run: .zcp/verify.sh $hostname 8080 / /health /status" >&2
                echo "  Or force: .zcp/mark-complete.sh --force $hostname" >&2
                return 1
                ;;
        esac
    else
        echo -e "${YELLOW}⚠️  Force mode: skipping verification check${NC}"
    fi

    # Check for completion file (subagent should write this for URL handoff)
    local completion_file="/tmp/${hostname}_complete.json"
    if [ ! -f "$completion_file" ]; then
        echo -e "${YELLOW}⚠️  Missing completion file: $completion_file${NC}" >&2
        echo "  URLs will not be available in discovery.json" >&2
        echo "  Write this file before mark-complete for full data" >&2
    fi

    # Create directory
    if ! mkdir -p "$service_dir" 2>/dev/null; then
        echo -e "${RED}ERROR: Cannot create state directory: $service_dir${NC}" >&2
        return 1
    fi

    # Write status file atomically (write to temp, then move)
    local tmp_file="${status_file}.tmp.$$"

    if ! cat > "$tmp_file" << EOF
{
    "phase": "complete",
    "completed_at": "$timestamp",
    "marked_by": "mark-complete.sh"
}
EOF
    then
        echo -e "${RED}ERROR: Cannot write status file${NC}" >&2
        rm -f "$tmp_file" 2>/dev/null
        return 1
    fi

    if ! mv "$tmp_file" "$status_file"; then
        echo -e "${RED}ERROR: Cannot finalize status file${NC}" >&2
        rm -f "$tmp_file" 2>/dev/null
        return 1
    fi

    echo -e "${GREEN}✓ Marked $hostname as complete${NC}"
    echo "  State file: $status_file"
    return 0
}

# Mark a service with a specific phase
mark_phase() {
    local hostname="$1"
    local phase="$2"
    local service_dir="$STATE_DIR/$hostname"
    local status_file="$service_dir/status.json"
    local timestamp
    timestamp=$(get_timestamp)

    mkdir -p "$service_dir" 2>/dev/null || true

    local tmp_file="${status_file}.tmp.$$"

    cat > "$tmp_file" << EOF
{
    "phase": "$phase",
    "updated_at": "$timestamp",
    "marked_by": "mark-complete.sh"
}
EOF

    mv "$tmp_file" "$status_file"
    echo -e "${GREEN}✓ Marked $hostname phase: $phase${NC}"
}

# Show status of all services
show_status() {
    echo "Bootstrap Service States"
    echo "========================"
    echo ""

    if [ ! -d "$STATE_DIR" ]; then
        echo "No service states found."
        echo "State directory: $STATE_DIR"
        return 0
    fi

    local found=0
    for service_dir in "$STATE_DIR"/*/; do
        if [ -d "$service_dir" ]; then
            local hostname
            hostname=$(basename "$service_dir")
            local status_file="$service_dir/status.json"

            if [ -f "$status_file" ]; then
                local phase completed_at
                phase=$(jq -r '.phase // "unknown"' "$status_file" 2>/dev/null || echo "unknown")
                completed_at=$(jq -r '.completed_at // .updated_at // "unknown"' "$status_file" 2>/dev/null || echo "unknown")

                case "$phase" in
                    complete|completed)
                        echo -e "  ${GREEN}✓${NC} $hostname: $phase ($completed_at)"
                        ;;
                    failed|error)
                        echo -e "  ${RED}✗${NC} $hostname: $phase"
                        ;;
                    *)
                        echo -e "  ${YELLOW}○${NC} $hostname: $phase"
                        ;;
                esac
                found=$((found + 1))
            fi
        fi
    done

    if [ $found -eq 0 ]; then
        echo "No service states found."
    fi

    echo ""
    echo "State directory: $STATE_DIR"
}

# Show help
show_help() {
    cat << 'EOF'
.zcp/mark-complete.sh - Mark bootstrap service completion

USAGE:
    .zcp/mark-complete.sh <hostname>              Mark service as complete
    .zcp/mark-complete.sh --force <hostname>      Mark complete (skip verification)
    .zcp/mark-complete.sh --check <hostname>      Check if service is complete
    .zcp/mark-complete.sh --phase <hostname> <phase>  Set specific phase
    .zcp/mark-complete.sh --status                Show all service states
    .zcp/mark-complete.sh --help                  Show this help

VERIFICATION GATE:
    By default, mark-complete checks /tmp/{hostname}_verify.json.
    If verification failed or is missing, completion is blocked.
    Use --force to override (NOT RECOMMENDED).

EXAMPLES:
    .zcp/mark-complete.sh appdev
    .zcp/mark-complete.sh --force appdev
    .zcp/mark-complete.sh --check appdev
    .zcp/mark-complete.sh --phase appdev deploying
    .zcp/mark-complete.sh --status

WHY THIS EXISTS:
    The bootstrap system uses state files to track subagent completion.
    The original approach required sourcing bash scripts, which failed
    in zsh or other shell contexts. This script works everywhere.

STATE FILE LOCATION:
    .zcp/state/bootstrap/services/{hostname}/status.json

INTEGRATION:
    Called by subagents at the end of code generation:
        .zcp/mark-complete.sh appdev

    Checked by aggregate-results step:
        .zcp/bootstrap.sh step aggregate-results
EOF
}

# Main
main() {
    case "${1:-}" in
        --help|-h)
            show_help
            ;;
        --check)
            if [ -z "${2:-}" ]; then
                echo "Usage: $0 --check <hostname>" >&2
                exit 1
            fi
            validate_hostname "$2" || exit 1
            phase=$(check_complete "$2")
            echo "$2: $phase"
            if [ "$phase" = "complete" ] || [ "$phase" = "completed" ]; then
                exit 0
            else
                exit 1
            fi
            ;;
        --phase)
            if [ -z "${2:-}" ] || [ -z "${3:-}" ]; then
                echo "Usage: $0 --phase <hostname> <phase>" >&2
                exit 1
            fi
            validate_hostname "$2" || exit 1
            mark_phase "$2" "$3"
            ;;
        --status)
            show_status
            ;;
        --force)
            if [ -z "${2:-}" ]; then
                echo "Usage: $0 --force <hostname>" >&2
                exit 1
            fi
            validate_hostname "$2" || exit 1
            mark_complete "$2" "--force"
            ;;
        "")
            echo "Usage: $0 <hostname>" >&2
            echo "       $0 --help for more options" >&2
            exit 1
            ;;
        -*)
            echo "Unknown option: $1" >&2
            echo "Run '$0 --help' for usage" >&2
            exit 1
            ;;
        *)
            validate_hostname "$1" || exit 1
            mark_complete "$1"
            ;;
    esac
}

main "$@"
