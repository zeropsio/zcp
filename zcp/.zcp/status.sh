#!/bin/bash

# Zerops Deployment Status and Monitoring
# Shows service status and can wait for deployment completion
#
# FIXED ISSUES:
# - Process check now service-specific (was project-wide)
# - Notification matching uses exact service name + timestamp filtering
# - Tracks deployment start time for correlation
# - Handles external timeout/interrupt signals gracefully
# - Better queue detection with process correlation

set -o pipefail

# ============================================================================
# HELP
# ============================================================================

show_help() {
    cat <<'EOF'
.zcp/status.sh - Deployment status and monitoring

USAGE:
  .zcp/status.sh                           # Show current state
  .zcp/status.sh --wait {service}          # Wait for deployment
  .zcp/status.sh --wait {service} --timeout 600
  .zcp/status.sh --check-queue {service}   # Check deploy queue status

SHOWS:
  - Service list with app version timestamps
  - Running/pending processes (builds)
  - Recent notifications (completions)

WAIT MODE:
  Polls until deployment completes or timeout.
  Returns 0 on success, 1 on failure/timeout.

  Key behaviors:
  - Only tracks processes for the SPECIFIC service (not project-wide)
  - Filters notifications by timestamp (ignores old SUCCESS/ERROR)
  - Records deployment start time for accurate correlation
  - Handles SIGTERM/SIGINT gracefully with cleanup

DEPLOYMENT STATUS LOGIC:
  ┌─────────────────────┬──────────────────┬────────────┐
  │ Service Processes   │ Notifications    │ Status     │
  ├─────────────────────┼──────────────────┼────────────┤
  │ BUILDING/PENDING    │ -                │ Building   │
  │ (none)              │ SUCCESS (new)    │ Complete ✅│
  │ (none)              │ ERROR (new)      │ Failed ❌  │
  │ (none)              │ (none new)       │ In progress│
  └─────────────────────┴──────────────────┴────────────┘

QUEUE DETECTION (Gap 44):
  When multiple deploys to the same service are in progress,
  Zerops queues them. If the first deploy fails, queued deploys
  are cancelled.

  Use --check-queue before deploying to see if another deploy
  is in progress or queued.

EXAMPLES:
  .zcp/status.sh
  .zcp/status.sh --wait appstage
  .zcp/status.sh --wait appstage --timeout 600
EOF
}

# ============================================================================
# CONSTANTS
# ============================================================================

readonly CHECK_INTERVAL=5
readonly DEFAULT_TIMEOUT=300
readonly ZCP_TMP="${ZCP_TMP_DIR:-/tmp}"

# ============================================================================
# CLEANUP AND SIGNAL HANDLING
# ============================================================================

# Track if we're in wait mode for cleanup
WAIT_MODE_ACTIVE=false
WAIT_SERVICE=""
WAIT_START_TIME=""

cleanup_on_exit() {
    local exit_code=$?

    if [ "$WAIT_MODE_ACTIVE" = true ] && [ -n "$WAIT_SERVICE" ]; then
        # Record interruption context if we were waiting
        if [ $exit_code -ne 0 ] && [ $exit_code -ne 1 ]; then
            SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
            if [ -f "$SCRIPT_DIR/lib/utils.sh" ]; then
                source "$SCRIPT_DIR/lib/utils.sh"
                auto_capture_context "deploy_interrupted" "$WAIT_SERVICE" "SIGNAL:$exit_code" \
                    "Deployment wait interrupted by signal (external timeout?)"
            fi
        fi
    fi

    # Clean up temp files
    rm -f "${ZCP_TMP}/deploy_evidence.json.tmp.$$" 2>/dev/null
    rm -f "${ZCP_TMP}/deploy_start_time.$$" 2>/dev/null
}

trap cleanup_on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# ============================================================================
# UTILITY
# ============================================================================

get_project_id() {
    if [ -n "$projectId" ]; then
        echo "$projectId"
    else
        echo ""
    fi
}

# Strip ANSI color codes from zcli output
strip_ansi() {
    sed 's/\x1b\[[0-9;]*m//g'
}

# Get current timestamp in epoch seconds
get_epoch() {
    date +%s
}

# Parse ISO timestamp to epoch (handles both GNU and BSD date)
parse_timestamp_to_epoch() {
    local ts="$1"
    local epoch

    # Try GNU date first
    if epoch=$(date -d "$ts" +%s 2>/dev/null); then
        echo "$epoch"
        return 0
    fi

    # Try BSD date
    if epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$ts" +%s 2>/dev/null); then
        echo "$epoch"
        return 0
    fi

    # Try BSD date with timezone offset format
    if epoch=$(date -j -f "%Y-%m-%dT%H:%M:%S%z" "$ts" +%s 2>/dev/null); then
        echo "$epoch"
        return 0
    fi

    # Fallback: return 0 (will be treated as very old)
    echo "0"
    return 1
}

# ============================================================================
# SERVICE-SPECIFIC STATUS (FIXED: was project-wide)
# ============================================================================

# Get service-specific JSON data from zcli
# Returns: JSON object for the specific service, or empty if not found
get_service_json() {
    local service="$1"
    local pid
    pid=$(get_project_id)

    if [ -z "$pid" ]; then
        return 1
    fi

    zcli service list -P "$pid" --format json 2>/dev/null | \
        strip_ansi | \
        jq --arg svc "$service" '(.services // [])[] | select(.name == $svc)' 2>/dev/null
}

# Check if a SPECIFIC service has active builds (FIXED: was checking all processes)
# Returns: BUILDING, PENDING, or IDLE
get_service_build_status() {
    local service="$1"
    local service_json

    service_json=$(get_service_json "$service")

    if [ -z "$service_json" ]; then
        echo "NOT_FOUND"
        return 1
    fi

    # Check THIS service's processes array
    local building_count pending_count
    building_count=$(echo "$service_json" | jq '[.processes[]? | select(.status == "BUILDING")] | length // 0' 2>/dev/null || echo "0")
    pending_count=$(echo "$service_json" | jq '[.processes[]? | select(.status == "PENDING")] | length // 0' 2>/dev/null || echo "0")

    if [ "$building_count" -gt 0 ]; then
        echo "BUILDING"
        return 0
    elif [ "$pending_count" -gt 0 ]; then
        echo "PENDING"
        return 0
    else
        echo "IDLE"
        return 0
    fi
}

# ============================================================================
# QUEUE DETECTION (Gap 44: Concurrent Deploy Conflict)
# ============================================================================

# Check if a service has queued deploys
# Returns: IDLE, BUILDING, or QUEUED:N (where N is queue count)
check_deploy_queue() {
    local service="$1"
    local service_json

    service_json=$(get_service_json "$service")

    if [ -z "$service_json" ]; then
        echo "NOT_FOUND"
        return 1
    fi

    # Check for queued processes
    local process_count building_count pending_count
    process_count=$(echo "$service_json" | jq '.processes | length // 0' 2>/dev/null || echo "0")
    building_count=$(echo "$service_json" | jq '[.processes[]? | select(.status == "BUILDING")] | length // 0' 2>/dev/null || echo "0")
    pending_count=$(echo "$service_json" | jq '[.processes[]? | select(.status == "PENDING")] | length // 0' 2>/dev/null || echo "0")

    local active_count=$((building_count + pending_count))

    if [ "$active_count" -gt 1 ]; then
        echo "QUEUED:$active_count"
        return 0
    elif [ "$active_count" -eq 1 ]; then
        echo "BUILDING"
        return 0
    else
        echo "IDLE"
        return 0
    fi
}

# Show queue status for a service
show_queue_status() {
    local service="$1"
    local queue_status
    queue_status=$(check_deploy_queue "$service")

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📦 DEPLOY QUEUE STATUS: $service"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    case "$queue_status" in
        QUEUED:*)
            local queue_count="${queue_status#QUEUED:}"
            echo "  ⏳ Status: QUEUED"
            echo "  📊 Deploys in queue: $queue_count"
            echo ""
            echo "  ⚠️  Another deploy is in progress."
            echo "     Your deploy will be queued and start after current one completes."
            echo "     If current deploy FAILS, queued deploys are CANCELLED."
            echo ""
            echo "  💡 Recommendation: Wait for current deploy to complete before starting new one."
            ;;
        BUILDING)
            echo "  🔨 Status: BUILDING"
            echo "  📊 One deploy in progress"
            echo ""
            echo "  ⚠️  A deploy is currently in progress."
            echo "     Starting another deploy now will queue it."
            ;;
        IDLE)
            echo "  ✅ Status: IDLE"
            echo "  📊 No active deploys"
            echo ""
            echo "  Ready to deploy."
            ;;
        NOT_FOUND)
            echo "  ❌ Service not found: $service"
            echo ""
            echo "  Check service name with: zcli service list -P \$projectId"
            ;;
        ERROR:*)
            echo "  ❌ Error: ${queue_status#ERROR:}"
            ;;
    esac

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# ============================================================================
# STATUS DISPLAY
# ============================================================================

show_status() {
    local pid
    pid=$(get_project_id)

    if [ -z "$pid" ]; then
        echo "❌ No projectId found in environment"
        exit 1
    fi

    cat <<EOF
╔══════════════════════════════════════════════════════════════════╗
║  ZEROPS STATUS                                                   ║
╚══════════════════════════════════════════════════════════════════╝

EOF

    # Services
    echo "┌─ SERVICES ─────────────────────────────────────────────────────────"
    zcli service list -P "$pid" 2>/dev/null | grep -v "^Using config" | head -20
    echo "└────────────────────────────────────────────────────────────────────"
    echo ""

    # Processes (project-wide for display purposes)
    echo "┌─ PROCESSES (running/pending builds) ──────────────────────────────"
    local processes
    processes=$(zcli project processes -P "$pid" 2>/dev/null | grep -v "^Using config")

    if echo "$processes" | grep -qi "no processes"; then
        echo "  (none)"
    elif [ -z "$processes" ]; then
        echo "  (none)"
    else
        echo "$processes" | head -20
    fi
    echo ""

    # Notifications
    echo "┌─ NOTIFICATIONS (recent) ───────────────────────────────────────────"
    zcli project notifications -P "$pid" 2>/dev/null | grep -v "^Using config" | head -20
    echo "└────────────────────────────────────────────────────────────────────"
}

# ============================================================================
# NOTIFICATION PARSING (FIXED: timestamp filtering + exact matching)
# ============================================================================

# Check notifications for a specific service, filtering by timestamp
# Args: service_name, start_epoch (only match notifications after this time)
# Returns: SUCCESS, ERROR, or NONE
check_service_notifications() {
    local service="$1"
    local start_epoch="$2"
    local pid
    pid=$(get_project_id)

    if [ -z "$pid" ]; then
        echo "NONE"
        return 0
    fi

    # Get notifications as JSON if available, otherwise parse text
    local notifications_raw
    notifications_raw=$(zcli project notifications -P "$pid" --format json 2>/dev/null | strip_ansi)

    # Check if we got valid JSON
    if echo "$notifications_raw" | jq -e '.notifications' &>/dev/null; then
        # JSON format available - parse properly
        local result
        result=$(echo "$notifications_raw" | jq -r --arg svc "$service" --argjson start "$start_epoch" '
            .notifications // [] |
            map(select(
                # Exact service name match (case-insensitive)
                (.serviceName // .service // "" | ascii_downcase) == ($svc | ascii_downcase)
            )) |
            # Sort by timestamp descending to get most recent first
            sort_by(.createdAt // .timestamp // "1970-01-01") | reverse |
            # Get the first (most recent) matching notification
            .[0] // null |
            if . == null then "NONE"
            elif (.status // .type // "" | ascii_downcase | contains("success")) then "SUCCESS"
            elif (.status // .type // "" | ascii_downcase | contains("error")) or
                 (.status // .type // "" | ascii_downcase | contains("fail")) then "ERROR"
            else "NONE"
            end
        ' 2>/dev/null)

        if [ -n "$result" ] && [ "$result" != "null" ]; then
            echo "$result"
            return 0
        fi
    fi

    # Fallback: Parse text format with exact matching
    local notifications_text
    notifications_text=$(zcli project notifications -P "$pid" 2>/dev/null | grep -v "^Using config")

    if [ -z "$notifications_text" ]; then
        echo "NONE"
        return 0
    fi

    # Use word boundary matching for exact service name
    # Pattern: service name followed by space or punctuation (not part of another name)
    local pattern="\\b${service}\\b"

    # Check for SUCCESS with exact service match
    if echo "$notifications_text" | grep -iE "$pattern" | grep -qi "SUCCESS\|COMPLETED\|DEPLOYED"; then
        echo "SUCCESS"
        return 0
    fi

    # Check for ERROR with exact service match
    if echo "$notifications_text" | grep -iE "$pattern" | grep -qi "ERROR\|FAILED\|FAILURE"; then
        echo "ERROR"
        return 0
    fi

    echo "NONE"
    return 0
}

# ============================================================================
# DEPLOYMENT STATUS CHECK (COMPLETELY REWRITTEN)
# ============================================================================

# Check deployment status for a SPECIFIC service
# Args: service_name, start_epoch (deployment start time)
# Returns: BUILDING, SUCCESS, ERROR, IN_PROGRESS, or ERROR:message
check_deployment_status() {
    local service="$1"
    local start_epoch="${2:-0}"
    local pid
    pid=$(get_project_id)

    if [ -z "$pid" ]; then
        echo "ERROR:No projectId"
        return 1
    fi

    # Step 1: Check THIS service's build status (FIXED: was project-wide)
    local build_status
    build_status=$(get_service_build_status "$service")

    case "$build_status" in
        NOT_FOUND)
            echo "ERROR:Service not found: $service"
            return 1
            ;;
        BUILDING|PENDING)
            echo "BUILDING"
            return 0
            ;;
        IDLE)
            # No active build - check notifications
            ;;
        *)
            # Unknown status - continue to notification check
            ;;
    esac

    # Step 2: Check notifications for THIS service (FIXED: timestamp + exact match)
    local notification_status
    notification_status=$(check_service_notifications "$service" "$start_epoch")

    case "$notification_status" in
        SUCCESS)
            echo "SUCCESS"
            return 0
            ;;
        ERROR)
            echo "ERROR"
            return 0
            ;;
        NONE)
            # No notification yet - still in progress
            echo "IN_PROGRESS"
            return 0
            ;;
    esac

    # Fallback
    echo "IN_PROGRESS"
    return 0
}

# ============================================================================
# WAIT MODE (COMPLETELY REWRITTEN)
# ============================================================================

wait_for_deployment() {
    local service="$1"
    local timeout="${2:-$DEFAULT_TIMEOUT}"

    # Validate service name
    if [ -z "$service" ]; then
        echo "ERROR: Service name required"
        return 1
    fi

    # Validate timeout is numeric
    if ! [[ "$timeout" =~ ^[0-9]+$ ]]; then
        echo "ERROR: Invalid timeout value: $timeout"
        return 1
    fi

    # Set globals for cleanup handler
    WAIT_MODE_ACTIVE=true
    WAIT_SERVICE="$service"
    WAIT_START_TIME=$(get_epoch)

    # Record deployment start time for notification filtering
    local start_epoch="$WAIT_START_TIME"
    echo "$start_epoch" > "${ZCP_TMP}/deploy_start_time.$$"

    echo "⏳ Waiting for $service deployment to complete (timeout: ${timeout}s)..."
    echo "   Started at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    echo ""

    local elapsed=0
    local last_status=""
    local consecutive_idle=0

    while [ $elapsed -lt $timeout ]; do
        local status
        status=$(check_deployment_status "$service" "$start_epoch")

        # Track consecutive IDLE states (might indicate missed notification)
        if [ "$status" = "IN_PROGRESS" ]; then
            consecutive_idle=$((consecutive_idle + 1))
        else
            consecutive_idle=0
        fi

        # Gap 44: Check queue status for better user feedback
        local queue_status
        queue_status=$(check_deploy_queue "$service")

        case "$status" in
            BUILDING)
                case "$queue_status" in
                    QUEUED:*)
                        local queue_count="${queue_status#QUEUED:}"
                        echo "  [${elapsed}/${timeout}s] ⏳ Queued (${queue_count} deploys in queue)"
                        if [ "$last_status" != "QUEUED" ]; then
                            echo "    → Your deploy will start after current one completes"
                        fi
                        last_status="QUEUED"
                        ;;
                    *)
                        echo "  [${elapsed}/${timeout}s] 🔨 Building... (service: $service)"
                        last_status="BUILDING"
                        ;;
                esac
                ;;

            SUCCESS)
                echo "  [${elapsed}/${timeout}s] ✅ Deployment complete!"
                echo ""
                record_deployment_evidence "$service" "SUCCESS"
                show_status
                WAIT_MODE_ACTIVE=false
                return 0
                ;;

            ERROR)
                echo "  [${elapsed}/${timeout}s] ❌ Deployment failed!"
                echo ""
                capture_deployment_error "$service" "Deployment failed"
                show_status
                WAIT_MODE_ACTIVE=false
                return 1
                ;;

            IN_PROGRESS)
                if [ $consecutive_idle -gt 12 ]; then
                    # 60+ seconds of no activity - warn user
                    echo "  [${elapsed}/${timeout}s] ⚠️  No activity detected for 60s+ (checking...)"
                else
                    echo "  [${elapsed}/${timeout}s] ⏳ Waiting for completion notification..."
                fi
                last_status="IN_PROGRESS"
                ;;

            ERROR:*)
                echo "  [${elapsed}/${timeout}s] ❌ ${status#ERROR:}"
                capture_deployment_error "$service" "${status#ERROR:}"
                WAIT_MODE_ACTIVE=false
                return 1
                ;;

            *)
                echo "  [${elapsed}/${timeout}s] ❓ Unknown status: $status"
                ;;
        esac

        sleep $CHECK_INTERVAL
        elapsed=$((elapsed + CHECK_INTERVAL))
    done

    # Timeout reached
    echo ""
    echo "❌ Timeout waiting for deployment (${timeout}s)"
    echo ""
    echo "   The deployment may still be running. Check with:"
    echo "   zcli project processes -P \$projectId"
    echo "   zcli project notifications -P \$projectId"
    echo ""

    capture_deployment_error "$service" "Deployment timed out after ${timeout}s"
    show_status
    WAIT_MODE_ACTIVE=false
    return 1
}

# ============================================================================
# EVIDENCE RECORDING
# ============================================================================

record_deployment_evidence() {
    local service="$1"
    local status="$2"

    # Get session ID
    local deploy_session
    deploy_session=$(cat "${ZCP_TMP}/zcp_session" 2>/dev/null || echo "")

    if [ -z "$deploy_session" ]; then
        echo "⚠️  No session ID found - evidence will have empty session_id"
    fi

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local start_time=""
    if [ -f "${ZCP_TMP}/deploy_start_time.$$" ]; then
        local start_epoch
        start_epoch=$(cat "${ZCP_TMP}/deploy_start_time.$$")
        # Convert epoch back to ISO format for evidence
        if command -v gdate &>/dev/null; then
            start_time=$(gdate -u -d "@$start_epoch" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "")
        else
            start_time=$(date -u -r "$start_epoch" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo "")
        fi
    fi

    local evidence_file="${ZCP_TMP}/deploy_evidence.json"
    local tmp_file="${evidence_file}.tmp.$$"

    if ! jq -n \
        --arg sid "$deploy_session" \
        --arg svc "$service" \
        --arg ts "$timestamp" \
        --arg status "$status" \
        --arg start "${start_time:-$timestamp}" \
        '{
            session_id: $sid,
            service: $svc,
            timestamp: $ts,
            status: $status,
            deployment_started: $start
        }' > "$tmp_file" 2>/dev/null; then
        echo "⚠️  Warning: Failed to create deployment evidence JSON" >&2
        rm -f "$tmp_file"
        return 1
    fi

    if ! mv "$tmp_file" "$evidence_file" 2>/dev/null; then
        echo "⚠️  Warning: Failed to save deployment evidence" >&2
        rm -f "$tmp_file"
        return 1
    fi

    echo "→ Deployment evidence recorded: $evidence_file"
    echo ""

    # Also write to persistent storage if available
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/lib/utils.sh" ]; then
        source "$SCRIPT_DIR/lib/utils.sh"
        if [ "$PERSISTENT_ENABLED" = true ] && [ -d "$WORKFLOW_STATE_DIR" ]; then
            mkdir -p "$WORKFLOW_STATE_DIR/evidence" 2>/dev/null
            cp "$evidence_file" "$WORKFLOW_STATE_DIR/evidence/deploy_evidence.json" 2>/dev/null
        fi
    fi

    return 0
}

capture_deployment_error() {
    local service="$1"
    local error_msg="$2"

    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/lib/utils.sh" ]; then
        source "$SCRIPT_DIR/lib/utils.sh"
        auto_capture_context "deploy_failure" "$service" "ERROR" "$error_msg"
    fi
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
        show_help
        exit 0
    fi

    if [ "$1" = "--check-queue" ]; then
        shift
        local service="$1"

        if [ -z "$service" ]; then
            echo "❌ Usage: .zcp/status.sh --check-queue {service}"
            exit 2
        fi

        show_queue_status "$service"
        exit 0
    fi

    if [ "$1" = "--wait" ]; then
        shift
        local service="$1"
        local timeout=$DEFAULT_TIMEOUT

        if [ -z "$service" ]; then
            echo "❌ Usage: .zcp/status.sh --wait {service} [--timeout N]"
            exit 2
        fi

        shift

        # Parse timeout if provided
        while [ $# -gt 0 ]; do
            case "$1" in
                --timeout)
                    shift
                    timeout="$1"
                    if [ -z "$timeout" ]; then
                        echo "❌ --timeout requires a value"
                        exit 2
                    fi
                    ;;
                *)
                    echo "❌ Unknown option: $1"
                    exit 2
                    ;;
            esac
            shift
        done

        wait_for_deployment "$service" "$timeout"
        exit $?
    fi

    # No arguments - just show status
    show_status
}

main "$@"
