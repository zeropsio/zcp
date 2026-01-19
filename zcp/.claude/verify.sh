#!/bin/bash

# Zerops Endpoint Verification with Evidence Generation
# Tests HTTP endpoints and creates JSON evidence files

set -o pipefail

DEBUG=false

# ============================================================================
# HELP
# ============================================================================

show_help() {
    cat <<'EOF'
verify.sh - Endpoint verification with evidence generation

⚠️  WARNING: This script only checks HTTP status codes (2xx = pass).
    HTTP 200 does NOT mean the feature works correctly!

    You MUST also verify:
    - Backend: Response body contains correct data
    - Frontend: No JavaScript errors (agent-browser errors)
    - Database: Data actually persisted

USAGE:
  verify.sh {service} {port} {endpoint} [endpoints...]
  verify.sh --debug {service} {port} {endpoint} [endpoints...]
  verify.sh --help

EXAMPLES:
  verify.sh appdev 8080 / /status /api/items
  verify.sh --debug appstage 8080 /

OUTPUT:
  Creates /tmp/{service}_verify.json
  Auto-copies to /tmp/dev_verify.json or /tmp/stage_verify.json

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  ADDITIONAL VERIFICATION REQUIRED (verify.sh is not enough!)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Backend APIs - Check response content:
  ssh {service} "curl -s http://localhost:{port}/api/endpoint" | jq .
  # Look for: correct data, no error messages

Frontend - Check for JavaScript errors:
  agent-browser open "$URL"
  agent-browser errors       # MUST be empty
  agent-browser console      # Check for runtime errors
  agent-browser screenshot   # Visual verification

Logs - Check for exceptions:
  ssh {service} "grep -iE 'error|exception|panic' /tmp/app.log"

Database - Verify persistence:
  psql/mysql/redis-cli to query and verify data

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

EVIDENCE FORMAT:
  {
    "session_id": "20260118160000-1234-5678",
    "service": "appdev",
    "port": 8080,
    "timestamp": "2026-01-18T16:05:00Z",
    "results": [
      {"endpoint": "/", "status": 200, "pass": true},
      {"endpoint": "/status", "status": 200, "pass": true}
    ],
    "passed": 2,
    "failed": 0
  }

DEBUG MODE:
  Use --debug flag to see detailed command execution
EOF
}

# ============================================================================
# UTILITY
# ============================================================================

debug_log() {
    if [ "$DEBUG" = true ]; then
        echo "[DEBUG] $*" >&2
    fi
}

get_session() {
    if [ -f "/tmp/claude_session" ]; then
        cat "/tmp/claude_session"
    else
        echo "$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
    fi
}

# Load discovery for accurate service matching
# Sets globals: DEV_SERVICE_NAME, STAGE_SERVICE_NAME
get_discovery_names() {
    if [ -f "/tmp/discovery.json" ]; then
        DEV_SERVICE_NAME=$(jq -r '.dev.name // empty' "/tmp/discovery.json" 2>/dev/null)
        STAGE_SERVICE_NAME=$(jq -r '.stage.name // empty' "/tmp/discovery.json" 2>/dev/null)
    else
        DEV_SERVICE_NAME=""
        STAGE_SERVICE_NAME=""
    fi
}

# ============================================================================
# ENDPOINT TESTING
# ============================================================================

test_endpoint() {
    local service="$1"
    local port="$2"
    local endpoint="$3"

    debug_log "Testing endpoint: $endpoint"
    debug_log "Command: ssh $service \"curl -sf -o /dev/null -w '%{http_code}' http://localhost:$port$endpoint\""

    local status_code
    status_code=$(ssh "$service" "curl -sf -o /dev/null -w '%{http_code}' http://localhost:$port$endpoint" 2>/dev/null)
    local curl_exit=$?

    debug_log "Result: $status_code (curl exit: $curl_exit)"

    # If curl succeeded and we got a 2xx status, it's a pass
    if [ $curl_exit -eq 0 ] && [ -n "$status_code" ] && [ "$status_code" -ge 200 ] && [ "$status_code" -lt 300 ]; then
        debug_log "Pass: true"
        echo "$status_code:true"
    else
        debug_log "Pass: false"
        # Return whatever status we got, or 000 if curl failed completely
        if [ -z "$status_code" ]; then
            status_code="000"
        fi
        echo "$status_code:false"
    fi
}

# ============================================================================
# FRONTEND DETECTION
# ============================================================================

check_frontend() {
    local service="$1"

    debug_log "Checking for frontend indicators in $service"

    # Check for common frontend directories/files
    if ssh "$service" "ls /var/www/templates /var/www/static /var/www/index.html /var/www/public 2>/dev/null" 2>/dev/null | grep -q .; then
        return 0
    fi
    return 1
}

show_frontend_reminder() {
    local service="$1"

    cat <<EOF

⚠️  FRONTEND DETECTED - Browser testing recommended

   URL=\$(ssh $service "echo \\\$zeropsSubdomain")
   agent-browser open "\$URL"
   agent-browser errors
   agent-browser console
   agent-browser screenshot

   💡 HTTP 200 ≠ working UI
      Screenshots can reveal broken layouts that curl cannot detect
EOF
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    # Parse flags
    if [ "$1" = "--help" ]; then
        show_help
        exit 0
    fi

    if [ "$1" = "--debug" ]; then
        DEBUG=true
        shift
    fi

    local service="$1"
    local port="$2"
    shift 2

    if [ -z "$service" ] || [ -z "$port" ] || [ $# -eq 0 ]; then
        echo "❌ Usage: verify.sh [--debug] {service} {port} {endpoint} [endpoints...]"
        echo ""
        echo "Example: verify.sh appdev 8080 / /status /api/items"
        echo "Help:    verify.sh --help"
        exit 2
    fi

    # Check jq availability
    if ! command -v jq &>/dev/null; then
        echo "❌ jq required but not found"
        exit 1
    fi

    # Get session
    local session_id
    session_id=$(get_session)
    debug_log "Session: $session_id"

    # Prepare results
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local passed=0
    local failed=0
    local results=()

    echo "=== Verifying $service:$port ==="

    # Test each endpoint
    for endpoint in "$@"; do
        local result
        result=$(test_endpoint "$service" "$port" "$endpoint")

        local status_code="${result%%:*}"
        local pass="${result##*:}"

        if [ "$pass" = "true" ]; then
            echo "  ✅ $endpoint → $status_code"
            passed=$((passed + 1))
        else
            echo "  ❌ $endpoint → $status_code"
            failed=$((failed + 1))
        fi

        # Build JSON result
        results+=("$(jq -n \
            --arg ep "$endpoint" \
            --arg st "$status_code" \
            --argjson p "$([ "$pass" = "true" ] && echo true || echo false)" \
            '{endpoint: $ep, status: ($st | tonumber), pass: $p}')")
    done

    echo ""
    echo "Passed: $passed | Failed: $failed"

    # Create JSON evidence
    local results_json
    results_json=$(printf '%s\n' "${results[@]}" | jq -s '.')

    local evidence_file="/tmp/${service}_verify.json"

    jq -n \
        --arg sid "$session_id" \
        --arg svc "$service" \
        --argjson prt "$port" \
        --arg ts "$timestamp" \
        --argjson res "$results_json" \
        --argjson passed "$passed" \
        --argjson failed "$failed" \
        '{
            session_id: $sid,
            service: $svc,
            port: $prt,
            timestamp: $ts,
            results: $res,
            passed: $passed,
            failed: $failed
        }' > "$evidence_file"

    echo "Evidence: $evidence_file"

    # Load discovery names for accurate matching
    get_discovery_names

    # Copy evidence to role-specific file based on discovery.json (exact match)
    if [ -n "$DEV_SERVICE_NAME" ] && [ "$service" = "$DEV_SERVICE_NAME" ]; then
        cp "$evidence_file" /tmp/dev_verify.json
        echo "→ Copied to /tmp/dev_verify.json (matches discovery dev: $DEV_SERVICE_NAME)"
    elif [ -n "$STAGE_SERVICE_NAME" ] && [ "$service" = "$STAGE_SERVICE_NAME" ]; then
        cp "$evidence_file" /tmp/stage_verify.json
        echo "→ Copied to /tmp/stage_verify.json (matches discovery stage: $STAGE_SERVICE_NAME)"
    elif [ -n "$DEV_SERVICE_NAME" ] || [ -n "$STAGE_SERVICE_NAME" ]; then
        # Discovery exists but service doesn't match
        echo "⚠️  Service '$service' not in discovery.json"
        echo "   Expected: dev='$DEV_SERVICE_NAME' or stage='$STAGE_SERVICE_NAME'"
        echo "   Evidence saved to: $evidence_file (not auto-linked to workflow)"
        echo ""
        echo "💡 If this is your dev/stage service, update discovery:"
        echo "   workflow.sh create_discovery {dev_id} $service {stage_id} {stage_name}"
    else
        # No discovery - fall back to pattern matching with warning
        echo "⚠️  No discovery.json found, using pattern matching fallback"
        if echo "$service" | grep -qi "dev" && ! echo "$service" | grep -qi "stage"; then
            cp "$evidence_file" /tmp/dev_verify.json
            echo "→ Copied to /tmp/dev_verify.json (pattern match: contains 'dev')"
        elif echo "$service" | grep -qi "stage"; then
            cp "$evidence_file" /tmp/stage_verify.json
            echo "→ Copied to /tmp/stage_verify.json (pattern match: contains 'stage')"
        fi
    fi

    # Check for frontend and show reminder
    if check_frontend "$service"; then
        show_frontend_reminder "$service"
    fi

    # Exit code based on failures
    if [ "$failed" -eq 0 ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "⚠️  HTTP status passed, but you must also verify:"
        echo ""
        if check_frontend "$service" 2>/dev/null; then
            echo "   Frontend: agent-browser errors (must be empty)"
            echo "             agent-browser console (check for errors)"
        else
            echo "   Backend:  curl response body (check data is correct)"
            echo "             grep -iE 'error|exception' /tmp/app.log"
        fi
        echo ""
        echo "   HTTP 200 ≠ feature works correctly"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        exit 0
    else
        exit 1
    fi
}

main "$@"
