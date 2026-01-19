#!/bin/bash
# Extension commands for Zerops Workflow

cmd_extend() {
    local import_file="$1"

    if [ -z "$import_file" ] || [ "$import_file" = "--help" ]; then
        show_topic_help "extend"
        return 0
    fi

    if [ ! -f "$import_file" ]; then
        echo "❌ File not found: $import_file"
        return 1
    fi

    # Validate YAML if yq is available
    if command -v yq &>/dev/null; then
        if ! yq '.' "$import_file" > /dev/null 2>&1; then
            echo "❌ Invalid YAML: $import_file"
            return 1
        fi
    fi

    local pid
    pid=$(cat /tmp/projectId 2>/dev/null || echo "$projectId")
    if [ -z "$pid" ]; then
        echo "❌ No projectId found"
        return 1
    fi

    echo "📦 Importing services from: $import_file"
    echo ""

    if ! zcli project import "$import_file" -P "$pid"; then
        echo "❌ Import failed"
        return 1
    fi

    echo ""
    echo "⏳ Waiting for new services to be ready..."

    local attempts=0
    local timeout_seconds=600
    local interval=10
    local max_attempts=$((timeout_seconds / interval))
    while zcli service list -P "$pid" 2>/dev/null | grep -qE "PENDING|BUILDING"; do
        ((attempts++))
        if [ $attempts -ge $max_attempts ]; then
            echo "⚠️  Timeout waiting for services (${timeout_seconds}s)"
            echo "   Check: zcli service list -P $pid"
            break
        fi
        echo "   Still building... (${attempts}/${max_attempts})"
        sleep $interval
    done

    echo ""
    echo "✅ Services ready"
    echo ""
    echo "⚠️  IMPORTANT: Environment Variable Timing"
    echo "   New services' vars are NOT visible in ZCP until restart."
    echo ""
    echo "   To access new credentials:"
    echo "   Option A: Restart ZCP (reconnect your IDE)"
    echo "   Option B: ssh {service} 'echo \$password'"
    echo ""
    echo "💡 See: .zcp/workflow.sh --help extend"
}

cmd_upgrade_to_full() {
    local current_mode
    current_mode=$(get_mode)

    if [ "$current_mode" = "full" ]; then
        echo "✅ Already in full mode"
        return 0
    fi

    if [ "$current_mode" != "dev-only" ]; then
        echo "❌ Can only upgrade from dev-only mode"
        echo "   Current mode: $current_mode"
        return 1
    fi

    echo "full" > "$MODE_FILE"

    local phase
    phase=$(get_phase)

    echo "✅ Upgraded to full mode"
    echo ""
    echo "📋 New workflow: DEVELOP → DEPLOY → VERIFY → DONE"
    echo ""

    if [ "$phase" = "DONE" ]; then
        # Revert to DEVELOP so they can go through full flow
        echo "DEVELOP" > "$PHASE_FILE"
        echo "💡 Reset to DEVELOP phase. Now:"
        echo "   1. .zcp/verify.sh {dev} {port} / /status /api/..."
        echo "   2. .zcp/workflow.sh transition_to DEPLOY"
    else
        echo "💡 Continue from current phase: $phase"
        echo "   Next: .zcp/workflow.sh transition_to DEPLOY"
    fi
}

cmd_record_deployment() {
    local service="$1"

    if [ -z "$service" ]; then
        echo "❌ Usage: .zcp/workflow.sh record_deployment {service_name}"
        return 1
    fi

    local session_id
    session_id=$(get_session)
    if [ -z "$session_id" ]; then
        echo "❌ No active session. Run: .zcp/workflow.sh init"
        return 1
    fi

    jq -n \
        --arg sid "$session_id" \
        --arg svc "$service" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        '{
            session_id: $sid,
            service: $svc,
            timestamp: $ts,
            status: "MANUAL"
        }' > "$DEPLOY_EVIDENCE_FILE"

    echo "✅ Deployment evidence recorded for $service"
    echo ""
    echo "💡 Next: .zcp/workflow.sh transition_to VERIFY"
}
