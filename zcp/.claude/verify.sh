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

TOOL AWARENESS:
  You can test more than HTTP status codes:
  - curl response bodies for data verification
  - Database queries to verify persistence
  - agent-browser for visual verification

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

    # Auto-copy to role-specific file
    if echo "$service" | grep -qi "dev" && ! echo "$service" | grep -qi "stage"; then
        cp "$evidence_file" /tmp/dev_verify.json
        echo "→ Copied to /tmp/dev_verify.json"
    elif echo "$service" | grep -qi "stage"; then
        cp "$evidence_file" /tmp/stage_verify.json
        echo "→ Copied to /tmp/stage_verify.json"
    fi

    # Check for frontend and show reminder
    if check_frontend "$service"; then
        show_frontend_reminder "$service"
    fi

    # Exit code based on failures
    if [ "$failed" -eq 0 ]; then
        exit 0
    else
        exit 1
    fi
}

main "$@"
