#!/bin/bash

# Zerops Verification - Attestation Based
# Agent verifies using tools, then records what was verified

set -o pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/state.sh" 2>/dev/null || true

# ============================================================================
# RECORD ATTESTATION
# ============================================================================

record_attestation() {
    local service="$1"
    local attestation="$2"

    if [ -z "$attestation" ]; then
        echo "❌ Attestation required"
        echo "Usage: .zcp/verify.sh {service} \"what you verified\""
        return 1
    fi

    local session_id
    session_id=$(get_session 2>/dev/null || echo "unknown")
    local evidence_file="/tmp/${service}_verify.json"

    jq -n \
        --arg sid "$session_id" \
        --arg svc "$service" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg att "$attestation" \
        '{
            session_id: $sid,
            service: $svc,
            timestamp: $ts,
            verification_type: "attestation",
            attestation: $att,
            passed: 1,
            failed: 0
        }' > "$evidence_file"

    echo "✅ Verified: $evidence_file"

    # Auto-link based on discovery
    if [ -f "/tmp/discovery.json" ]; then
        local dev_name stage_name
        dev_name=$(jq -r '.dev.name // empty' /tmp/discovery.json 2>/dev/null)
        stage_name=$(jq -r '.stage.name // empty' /tmp/discovery.json 2>/dev/null)

        if [ "$service" = "$dev_name" ]; then
            ln -sf "$evidence_file" /tmp/dev_verify.json
            echo "→ Linked to /tmp/dev_verify.json"
        elif [ "$service" = "$stage_name" ]; then
            ln -sf "$evidence_file" /tmp/stage_verify.json
            echo "→ Linked to /tmp/stage_verify.json"
        fi
    fi
}

# ============================================================================
# DIAGNOSTIC HELPERS (optional, non-blocking)
# ============================================================================

check_port() {
    local service="$1"
    local port="$2"

    if [ -z "$port" ]; then
        echo "Usage: .zcp/verify.sh --check {service} {port}"
        return 1
    fi

    echo "Checking port $port on $service..."
    if ssh "$service" "netstat -tlnp 2>/dev/null | grep -q ':$port ' || ss -tlnp 2>/dev/null | grep -q ':$port '" 2>/dev/null; then
        echo "  ✓ Port $port is listening"
        return 0
    else
        echo "  ✗ Nothing on port $port"
        echo "  → Start your server first"
        return 1
    fi
}

show_tools() {
    local service="$1"

    # Get runtime from discovery if available
    local runtime="unknown"
    if [ -f "/tmp/discovery.json" ]; then
        runtime=$(jq -r '.runtime // "unknown"' /tmp/discovery.json 2>/dev/null)
    fi

    cat <<EOF

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔧 VERIFICATION TOOLS FOR: $service
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

EOF

    # Type checking based on runtime
    echo "TYPE/BUILD CHECK:"
    case "$runtime" in
        go|go@*)
            echo "  ssh $service \"cd /var/www && go build -n .\"      # Dry-run build"
            echo "  ssh $service \"cd /var/www && go vet ./...\"       # Static analysis"
            ;;
        bun|bun@*)
            echo "  ssh $service \"cd /var/www && bun x tsc --noEmit\" # Type check"
            ;;
        nodejs|nodejs@*|node|node@*)
            echo "  ssh $service \"cd /var/www && npx tsc --noEmit\"   # Type check"
            ;;
        python|python@*)
            echo "  ssh $service \"cd /var/www && python -m py_compile *.py\""
            ;;
        *)
            echo "  ssh $service \"cd /var/www && <build-command>\""
            ;;
    esac

    cat <<EOF

HTTP ENDPOINTS:
  ssh $service "curl -s http://localhost:{port}/"
  ssh $service "curl -s http://localhost:{port}/health | jq ."
  ssh $service "curl -sI http://localhost:{port}/events | grep content-type"

LOGS:
  ssh $service "tail -30 /tmp/app.log"
  ssh $service "grep -iE 'error|panic|exception' /tmp/app.log"
  zcli service log -S {service_id} -P \$projectId --limit 50

PROCESS (workers/cron):
  ssh $service "ps aux | grep {process_name}"

BROWSER (frontend):
  URL=\$(ssh $service "echo \$zeropsSubdomain")
  agent-browser open "\$URL"
  agent-browser errors      # MUST be empty
  agent-browser screenshot

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ WHEN VERIFIED - Record what you checked:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  .zcp/verify.sh $service "brief description of verification"

Examples:
  .zcp/verify.sh $service "tsc clean, /health returns ok, no errors in logs"
  .zcp/verify.sh $service "worker process running, processed test job, logs clean"
  .zcp/verify.sh $service "browser errors empty, UI renders correctly"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    case "$1" in
        --help|-h)
            cat <<'EOF'
.zcp/verify.sh - Record verification attestation

USAGE:
  .zcp/verify.sh {service} "what you verified"    # Record attestation (required)
  .zcp/verify.sh {service}                        # Show available tools
  .zcp/verify.sh --tools {service}                # Show available tools
  .zcp/verify.sh --check {service} {port}         # Check if port listening

EXAMPLES:
  .zcp/verify.sh bundev "tsc passed, /events streams correctly, logs clean"
  .zcp/verify.sh goworker "process running, processed 3 jobs, no errors"
  .zcp/verify.sh nginx-fe "browser errors empty, assets load, no 404s"

This tool does NOT automatically verify anything.
YOU verify using the tools shown, then record what you verified.
EOF
            ;;
        --tools)
            show_tools "$2"
            ;;
        --check)
            check_port "$2" "$3"
            ;;
        *)
            local service="$1"
            shift
            local attestation="$*"

            if [ -z "$service" ]; then
                echo "Usage: .zcp/verify.sh {service} \"what you verified\""
                echo "       .zcp/verify.sh --help"
                exit 1
            fi

            if [ -z "$attestation" ]; then
                # No attestation given - show tools
                show_tools "$service"
            else
                record_attestation "$service" "$attestation"
            fi
            ;;
    esac
}

main "$@"
