#!/usr/bin/env bash
# .zcp/wait-for-subagents.sh - Wait for bootstrap subagents to complete
#
# This script polls aggregate-results until all subagents are complete.
# Similar to status.sh --wait for deployments, but for subagent completion.
#
# Usage:
#   .zcp/wait-for-subagents.sh                    # Default 10 min timeout
#   .zcp/wait-for-subagents.sh --timeout 600      # Custom timeout (seconds)
#   .zcp/wait-for-subagents.sh --check            # Just check status, don't wait
#
# Returns:
#   0 - All subagents complete
#   1 - Timeout or failure
#   2 - Usage error

set -euo pipefail

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default timeout: 10 minutes
DEFAULT_TIMEOUT=600
CHECK_INTERVAL=5

show_help() {
    cat << 'EOF'
.zcp/wait-for-subagents.sh - Wait for bootstrap subagents to complete

USAGE:
    .zcp/wait-for-subagents.sh                    Wait with 10 min timeout
    .zcp/wait-for-subagents.sh --timeout 600      Custom timeout (seconds)
    .zcp/wait-for-subagents.sh --check            Check status without waiting
    .zcp/wait-for-subagents.sh --help             Show this help

WHAT IT DOES:
    Polls .zcp/bootstrap.sh step aggregate-results until:
    - All services report phase: complete
    - Or timeout is reached
    - Or a failure is detected

    Uses auto-detection: if state file is missing but zerops.yml + source
    code exist, automatically marks service as complete.

EXAMPLES:
    # Wait for all subagents (default 10 min timeout)
    .zcp/wait-for-subagents.sh

    # Wait with 5 minute timeout
    .zcp/wait-for-subagents.sh --timeout 300

    # Just check current status
    .zcp/wait-for-subagents.sh --check

RETURN CODES:
    0 - All subagents complete
    1 - Timeout, failure, or error
    2 - Usage error

INTEGRATION:
    After spawning subagents:
        1. Spawn subagent via Task tool
        2. .zcp/wait-for-subagents.sh --timeout 600
        3. Workflow continues to DEVELOP phase
EOF
}

check_status() {
    local output
    output=$("$SCRIPT_DIR/bootstrap.sh" step aggregate-results 2>&1) || true

    local status
    status=$(echo "$output" | jq -r '.status // "unknown"' 2>/dev/null || echo "parse_error")

    local complete total
    complete=$(echo "$output" | jq -r '.data.complete // 0' 2>/dev/null || echo "0")
    total=$(echo "$output" | jq -r '.data.total // 0' 2>/dev/null || echo "0")

    local pending
    pending=$(echo "$output" | jq -r '.data.pending // [] | length' 2>/dev/null || echo "0")

    echo "$status:$complete:$total:$pending"
}

wait_for_completion() {
    local timeout="${1:-$DEFAULT_TIMEOUT}"
    local start_time elapsed

    start_time=$(date +%s)

    echo -e "${BLUE}Waiting for subagents to complete...${NC}"
    echo "Timeout: ${timeout}s | Check interval: ${CHECK_INTERVAL}s"
    echo ""

    while true; do
        elapsed=$(($(date +%s) - start_time))

        if [ $elapsed -ge $timeout ]; then
            echo ""
            echo -e "${RED}TIMEOUT after ${timeout}s${NC}"
            echo ""
            echo "Recovery options:"
            echo "  1. Check if files exist: ls /var/www/{hostname}/zerops.yml"
            echo "  2. Mark complete manually: .zcp/mark-complete.sh {hostname}"
            echo "  3. Re-run: .zcp/bootstrap.sh step aggregate-results"
            return 1
        fi

        local result
        result=$(check_status)

        local status complete total pending
        status=$(echo "$result" | cut -d: -f1)
        complete=$(echo "$result" | cut -d: -f2)
        total=$(echo "$result" | cut -d: -f3)
        pending=$(echo "$result" | cut -d: -f4)

        case "$status" in
            complete)
                echo ""
                echo -e "${GREEN}All $total service(s) complete!${NC}"
                echo "Workflow transitioned to DEVELOP phase."
                return 0
                ;;
            in_progress)
                printf "\r  [%3d/%3ds] %s/%s complete, %s pending...   " \
                    "$elapsed" "$timeout" "$complete" "$total" "$pending"
                ;;
            failed)
                echo ""
                echo -e "${RED}Subagent failure detected${NC}"
                echo "Run: .zcp/bootstrap.sh step aggregate-results"
                echo "For details and recovery steps."
                return 1
                ;;
            parse_error)
                echo -e "${YELLOW}  [${elapsed}/${timeout}s] Waiting for aggregate-results...${NC}"
                ;;
            *)
                echo -e "${YELLOW}  [${elapsed}/${timeout}s] Status: $status${NC}"
                ;;
        esac

        sleep $CHECK_INTERVAL
    done
}

just_check() {
    local output
    output=$("$SCRIPT_DIR/bootstrap.sh" step aggregate-results 2>&1) || true

    local status
    status=$(echo "$output" | jq -r '.status // "unknown"' 2>/dev/null || echo "error")

    case "$status" in
        complete)
            echo -e "${GREEN}All subagents complete${NC}"
            echo "$output" | jq -r '.message // empty' 2>/dev/null
            return 0
            ;;
        in_progress)
            echo -e "${YELLOW}Subagents in progress${NC}"
            echo "$output" | jq -r '.data.pending[]' 2>/dev/null | while read -r p; do
                echo "  Pending: $p"
            done
            return 1
            ;;
        failed)
            echo -e "${RED}Subagent failure${NC}"
            echo "$output" | jq -r '.message // empty' 2>/dev/null
            return 1
            ;;
        *)
            echo -e "${YELLOW}Status: $status${NC}"
            echo "$output" | jq '.' 2>/dev/null || echo "$output"
            return 1
            ;;
    esac
}

main() {
    local timeout=$DEFAULT_TIMEOUT
    local check_only=false

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --help|-h)
                show_help
                exit 0
                ;;
            --timeout)
                if [ -z "${2:-}" ]; then
                    echo "ERROR: --timeout requires a value" >&2
                    exit 2
                fi
                timeout="$2"
                shift 2
                ;;
            --check)
                check_only=true
                shift
                ;;
            *)
                echo "Unknown option: $1" >&2
                echo "Run '$0 --help' for usage" >&2
                exit 2
                ;;
        esac
    done

    # Validate timeout is a number
    if ! [[ "$timeout" =~ ^[0-9]+$ ]]; then
        echo "ERROR: timeout must be a number (seconds)" >&2
        exit 2
    fi

    if [ "$check_only" = true ]; then
        just_check
    else
        wait_for_completion "$timeout"
    fi
}

main "$@"
