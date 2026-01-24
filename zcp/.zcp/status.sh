#!/bin/bash

# Zerops Deployment Status and Monitoring
# Shows service status and can wait for deployment completion

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

SHOWS:
  - Service list with app version timestamps
  - Running/pending processes (builds)
  - Recent notifications (completions)

WAIT MODE:
  Polls until deployment completes or timeout.
  Returns 0 on success, 1 on failure/timeout.

DEPLOYMENT STATUS LOGIC:
  ┌─────────────────────┬──────────────────┬────────────┐
  │ Processes           │ Notifications    │ Status     │
  ├─────────────────────┼──────────────────┼────────────┤
  │ RUNNING or PENDING  │ -                │ Building   │
  │ (empty)             │ SUCCESS          │ Complete ✅│
  │ (empty)             │ ERROR            │ Failed ❌  │
  │ (empty)             │ (not found)      │ In progress│
  └─────────────────────┴──────────────────┴────────────┘

EXAMPLES:
  .zcp/status.sh
  .zcp/status.sh --wait appstage
  .zcp/status.sh --wait appstage --timeout 600
EOF
}

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

    # Processes
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
# WAIT MODE
# ============================================================================

check_deployment_status() {
    local service="$1"
    local pid
    pid=$(get_project_id)

    if [ -z "$pid" ]; then
        echo "ERROR:No projectId"
        return 1
    fi

    # Check processes
    local processes
    processes=$(zcli project processes -P "$pid" 2>/dev/null | grep -v "^Using config")

    # If we have active processes, we're building
    if echo "$processes" | grep -qE "(RUNNING|PENDING)"; then
        echo "BUILDING"
        return 0
    fi

    # No active processes - check notifications for completion
    local notifications
    notifications=$(zcli project notifications -P "$pid" 2>/dev/null | grep -v "^Using config")

    # Look for recent notification mentioning our service
    if echo "$notifications" | grep -i "$service" | grep -qi "SUCCESS"; then
        echo "SUCCESS"
        return 0
    elif echo "$notifications" | grep -i "$service" | grep -qi "ERROR"; then
        echo "ERROR"
        return 0
    fi

    # No clear status yet
    echo "IN_PROGRESS"
    return 0
}

wait_for_deployment() {
    local service="$1"
    local timeout="${2:-300}"  # Default 5 minutes

    echo "⏳ Waiting for $service deployment to complete (timeout: ${timeout}s)..."
    echo ""

    local elapsed=0
    local check_interval=5

    while [ $elapsed -lt $timeout ]; do
        local status
        status=$(check_deployment_status "$service")

        case "$status" in
            BUILDING)
                echo "  [${elapsed}/${timeout}s] Building... (status: RUNNING)"
                ;;
            SUCCESS)
                echo "  [${elapsed}/${timeout}s] ✅ Deployment complete!"
                echo ""
                # Record deployment evidence
                local deploy_session
                deploy_session=$(cat /tmp/claude_session 2>/dev/null || echo "")
                if [ -n "$deploy_session" ]; then
                    if ! jq -n \
                        --arg sid "$deploy_session" \
                        --arg svc "$service" \
                        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
                        '{
                            session_id: $sid,
                            service: $svc,
                            timestamp: $ts,
                            status: "SUCCESS"
                        }' > /tmp/deploy_evidence.json.tmp; then
                        echo "Warning: Failed to record deployment evidence" >&2
                        rm -f /tmp/deploy_evidence.json.tmp
                    else
                        mv /tmp/deploy_evidence.json.tmp /tmp/deploy_evidence.json
                        echo "→ Deployment evidence recorded: /tmp/deploy_evidence.json"
                        echo ""
                    fi
                fi
                show_status
                return 0
                ;;
            ERROR)
                echo "  [${elapsed}/${timeout}s] ❌ Deployment failed!"
                echo ""
                # Auto-capture context for workflow continuity
                SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
                if [ -f "$SCRIPT_DIR/lib/utils.sh" ]; then
                    source "$SCRIPT_DIR/lib/utils.sh"
                    auto_capture_context "deploy_failure" "$service" "ERROR" "Deployment failed"
                fi
                show_status
                return 1
                ;;
            IN_PROGRESS)
                echo "  [${elapsed}/${timeout}s] Waiting for completion notification..."
                ;;
            ERROR:*)
                echo "  [${elapsed}/${timeout}s] ❌ ${status#ERROR:}"
                return 1
                ;;
        esac

        sleep $check_interval
        elapsed=$((elapsed + check_interval))
    done

    echo ""
    echo "❌ Timeout waiting for deployment (${timeout}s)"
    echo ""
    # Auto-capture context for workflow continuity
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/lib/utils.sh" ]; then
        source "$SCRIPT_DIR/lib/utils.sh"
        auto_capture_context "deploy_timeout" "$service" "TIMEOUT" "Deployment timed out after ${timeout}s"
    fi
    show_status
    return 1
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    if [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
        show_help
        exit 0
    fi

    if [ "$1" = "--wait" ]; then
        shift
        local service="$1"
        local timeout=300

        if [ -z "$service" ]; then
            echo "❌ Usage: .zcp/status.sh --wait {service} [--timeout N]"
            exit 2
        fi

        shift

        # Parse timeout if provided
        if [ "$1" = "--timeout" ]; then
            shift
            timeout="$1"
            if [ -z "$timeout" ]; then
                echo "❌ --timeout requires a value"
                exit 2
            fi
        fi

        wait_for_deployment "$service" "$timeout"
        exit $?
    fi

    # No arguments - just show status
    show_status
}

main "$@"
