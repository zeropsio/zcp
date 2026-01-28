#!/bin/bash

# Zerops Endpoint Verification with Evidence Generation
# Tests HTTP endpoints and creates JSON evidence files

set -o pipefail
umask 077

# Trap for cleanup on exit
trap 'rm -f "/tmp/verify_tmp.$$" 2>/dev/null' EXIT INT TERM

DEBUG=false

# ============================================================================
# HELP
# ============================================================================

show_help() {
    cat <<'EOF'
.zcp/verify.sh - Endpoint verification with evidence generation

⚠️  WARNING: This script only checks HTTP status codes (2xx = pass).
    HTTP 200 does NOT mean the feature works correctly!

    You MUST also verify:
    - Backend: Response body contains correct data
    - Frontend: No JavaScript errors (agent-browser errors)
    - Database: Data actually persisted

USAGE:
  .zcp/verify.sh {service} {port} {endpoint} [endpoints...]
  .zcp/verify.sh --debug {service} {port} {endpoint} [endpoints...]
  .zcp/verify.sh --help

EXAMPLES:
  .zcp/verify.sh appdev 8080 / /status /api/items
  .zcp/verify.sh --debug appstage 8080 /

OUTPUT:
  Creates /tmp/{service}_verify.json
  Auto-copies to /tmp/dev_verify.json or /tmp/stage_verify.json

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  ADDITIONAL VERIFICATION REQUIRED (.zcp/verify.sh is not enough!)
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

# Source utils.sh for shared functions (get_session, etc.)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "$SCRIPT_DIR/lib/utils.sh" ]; then
    source "$SCRIPT_DIR/lib/utils.sh"
fi

# Source validation functions (CRITICAL-3: hostname validation)
if [ -f "$SCRIPT_DIR/lib/validate.sh" ]; then
    source "$SCRIPT_DIR/lib/validate.sh"
fi

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
    debug_log "Command: ssh $service \"curl -s -w '\\n%{http_code}' http://localhost:$port$endpoint\""

    # Get both body and status code (body, newline, status_code)
    local output
    output=$(ssh "$service" "curl -s -w '\n%{http_code}' http://localhost:$port$endpoint" 2>/dev/null)
    local curl_exit=$?

    # Parse status code (last line) and body (everything before)
    local status_code="${output##*$'\n'}"
    local body="${output%$'\n'*}"

    debug_log "Result: $status_code (curl exit: $curl_exit)"

    # If curl succeeded and we got a 2xx status, it's a pass
    if [ $curl_exit -eq 0 ] && [ -n "$status_code" ] && [ "$status_code" -ge 200 ] && [ "$status_code" -lt 300 ]; then
        debug_log "Pass: true"
        echo "$status_code:true:"
    else
        debug_log "Pass: false"
        # Return whatever status we got, or 000 if curl failed completely
        if [ -z "$status_code" ]; then
            status_code="000"
        fi
        # Include truncated body for context capture (first 200 chars)
        local truncated="${body:0:200}"
        echo "$status_code:false:$truncated"
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
# PRE-FLIGHT CHECK: Is port actually listening?
# ============================================================================

check_port_listening() {
    local service="$1"
    local port="$2"

    debug_log "Pre-flight: checking if port $port is listening on $service"

    # Check if port is listening (try netstat first, fall back to ss)
    local listening
    listening=$(ssh "$service" "netstat -tlnp 2>/dev/null | grep -E ':$port\s' || ss -tlnp 2>/dev/null | grep -E ':$port\s'" 2>/dev/null)

    if [ -z "$listening" ]; then
        return 1  # Nothing listening
    fi
    return 0  # Port is listening
}

show_no_server_error() {
    local service="$1"
    local port="$2"

    # Detect runtime from files in /var/www
    local runtime="unknown"
    if ssh "$service" "test -f /var/www/go.mod" 2>/dev/null; then
        runtime="go"
    elif ssh "$service" "test -f /var/www/package.json" 2>/dev/null; then
        runtime="nodejs"
    elif ssh "$service" "test -f /var/www/requirements.txt || test -f /var/www/app.py" 2>/dev/null; then
        runtime="python"
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "⚠️  NO SERVER LISTENING ON PORT $port"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "Dev services use 'start: zsc noop --silent' — no auto-start."
    echo ""
    echo "START THE SERVER:"

    case "$runtime" in
        go)
            echo "  ssh $service \"cd /var/www && nohup go run . > /tmp/app.log 2>&1 &\""
            ;;
        nodejs)
            echo "  ssh $service \"cd /var/www && nohup node index.js > /tmp/app.log 2>&1 &\""
            ;;
        python)
            echo "  ssh $service \"cd /var/www && nohup python app.py > /tmp/app.log 2>&1 &\""
            ;;
        *)
            echo "  ssh $service \"cd /var/www && nohup <your-command> > /tmp/app.log 2>&1 &\""
            ;;
    esac

    echo ""
    echo "VERIFY PORT:"
    echo "  ssh $service \"netstat -tlnp 2>/dev/null | grep $port || ss -tlnp | grep $port\""
    echo ""
    echo "THEN RE-RUN:"
    echo "  .zcp/verify.sh $service $port ..."
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
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
        echo "❌ Usage: .zcp/verify.sh [--debug] {service} {port} {endpoint} [endpoints...]"
        echo ""
        echo "Example: .zcp/verify.sh appdev 8080 / /status /api/items"
        echo "Help:    .zcp/verify.sh --help"
        exit 2
    fi

    # CRITICAL-3: Validate service hostname before SSH (prevents injection)
    if type validate_ssh_hostname &>/dev/null; then
        if ! validate_ssh_hostname "$service"; then
            exit 1
        fi
    else
        # Fallback validation
        if [[ ! "$service" =~ ^[a-zA-Z0-9_-]+$ ]] || [[ "$service" == *"@"* ]]; then
            echo "❌ Invalid service name: '$service'" >&2
            echo "   Must contain only alphanumeric characters, underscores, and hyphens" >&2
            exit 1
        fi
    fi

    # Validate port number (M-3)
    if type validate_port &>/dev/null; then
        if ! validate_port "$port"; then
            exit 1
        fi
    else
        if [[ ! "$port" =~ ^[0-9]+$ ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
            echo "❌ Invalid port: '$port' (must be 1-65535)" >&2
            exit 1
        fi
    fi

    # Check jq availability
    if ! command -v jq &>/dev/null; then
        echo "❌ jq required but not found"
        exit 1
    fi

    # PRE-FLIGHT: Check if port is listening before attempting verification
    echo "Pre-flight: checking port $port..."
    if ! check_port_listening "$service" "$port"; then
        show_no_server_error "$service" "$port"

        # Get session for evidence file
        local session_id
        session_id=$(get_session 2>/dev/null || echo "unknown")

        # Create evidence file showing pre-flight failure
        local preflight_file="/tmp/${service}_verify.json"
        jq -n \
            --arg sid "$session_id" \
            --arg svc "$service" \
            --argjson prt "$port" \
            --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            '{
                session_id: $sid,
                service: $svc,
                port: $prt,
                timestamp: $ts,
                preflight_failed: true,
                preflight_reason: "No process listening on port",
                results: [],
                passed: 0,
                failed: 0
            }' > "$preflight_file"

        echo "Evidence: $preflight_file (preflight_failed: true)"
        exit 1
    fi
    echo "  ✓ Port $port is listening"

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

    echo ""
    echo "=== Verifying $service:$port ==="

    # Track first failure for context capture
    local first_failure_endpoint=""
    local first_failure_status=""
    local first_failure_body=""

    # Test each endpoint
    for endpoint in "$@"; do
        local result
        result=$(test_endpoint "$service" "$port" "$endpoint")

        # Parse result: status_code:pass:body_snippet
        local status_code pass body_snippet
        status_code=$(echo "$result" | cut -d: -f1)
        pass=$(echo "$result" | cut -d: -f2)
        body_snippet=$(echo "$result" | cut -d: -f3-)

        if [ "$pass" = "true" ]; then
            echo "  ✅ $endpoint → $status_code"
            passed=$((passed + 1))
        else
            echo "  ❌ $endpoint → $status_code"
            failed=$((failed + 1))

            # Capture first failure for context
            if [ -z "$first_failure_endpoint" ]; then
                first_failure_endpoint="$endpoint"
                first_failure_status="$status_code"
                first_failure_body="$body_snippet"
            fi
        fi

        # Build JSON result
        results+=("$(jq -n \
            --arg ep "$endpoint" \
            --arg st "$status_code" \
            --argjson p "$([ "$pass" = "true" ] && echo true || echo false)" \
            '{endpoint: $ep, status: ($st | tonumber), pass: $p}')")
    done

    # Auto-capture context on failure (for workflow continuity)
    if [ -n "$first_failure_endpoint" ]; then
        # Source utils.sh for auto_capture_context if not already sourced
        SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
        if [ -f "$SCRIPT_DIR/lib/utils.sh" ]; then
            source "$SCRIPT_DIR/lib/utils.sh"
            auto_capture_context "verify_failure" "$first_failure_endpoint" "$first_failure_status" "$first_failure_body"
        fi
    fi

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

    # Symlink evidence to role-specific file based on discovery.json (exact match)
    # Using symlinks instead of copies to avoid duplication
    if [ -n "$DEV_SERVICE_NAME" ] && [ "$service" = "$DEV_SERVICE_NAME" ]; then
        ln -sf "$evidence_file" /tmp/dev_verify.json
        echo "→ Linked to /tmp/dev_verify.json (matches discovery dev: $DEV_SERVICE_NAME)"
    elif [ -n "$STAGE_SERVICE_NAME" ] && [ "$service" = "$STAGE_SERVICE_NAME" ]; then
        ln -sf "$evidence_file" /tmp/stage_verify.json
        echo "→ Linked to /tmp/stage_verify.json (matches discovery stage: $STAGE_SERVICE_NAME)"
    elif [ -n "$DEV_SERVICE_NAME" ] || [ -n "$STAGE_SERVICE_NAME" ]; then
        # Discovery exists but service doesn't match
        echo "⚠️  Service '$service' not in discovery.json"
        echo "   Expected: dev='$DEV_SERVICE_NAME' or stage='$STAGE_SERVICE_NAME'"
        echo "   Evidence saved to: $evidence_file (not auto-linked to workflow)"
        echo ""
        echo "💡 If this is your dev/stage service, update discovery:"
        echo "   .zcp/workflow.sh create_discovery {dev_id} $service {stage_id} {stage_name}"
    else
        # No discovery - fall back to pattern matching with warning
        echo "⚠️  No discovery.json found, using pattern matching fallback"
        if echo "$service" | grep -qi "dev" && ! echo "$service" | grep -qi "stage"; then
            ln -sf "$evidence_file" /tmp/dev_verify.json
            echo "→ Linked to /tmp/dev_verify.json (pattern match: contains 'dev')"
        elif echo "$service" | grep -qi "stage"; then
            ln -sf "$evidence_file" /tmp/stage_verify.json
            echo "→ Linked to /tmp/stage_verify.json (pattern match: contains 'stage')"
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
