#!/bin/bash
# Discovery commands for Zerops Workflow

cmd_create_discovery() {
    local dev_id="$1"
    local dev_name="$2"
    local stage_id="$3"
    local stage_name="$4"
    local single_mode=false

    # Handle --single flag
    if [ "$dev_id" = "--single" ]; then
        single_mode=true
        dev_id="$2"
        dev_name="$3"
        stage_id="$2"
        stage_name="$3"

        if [ -z "$dev_id" ] || [ -z "$dev_name" ]; then
            echo "❌ Usage: .zcp/workflow.sh create_discovery --single {service_id} {service_name}"
            return 1
        fi

        echo "⚠️  SINGLE-SERVICE MODE"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        echo "Using same service for dev AND stage: $dev_name"
        echo ""
        echo "RISKS YOU'RE ACCEPTING:"
        echo "  1. zcli push may overwrite source files"
        echo "  2. No isolation between development and deployment"
        echo "  3. A failed deploy affects your development environment"
        echo ""
        echo "WHEN THIS IS SAFE:"
        echo "  - Build creates separate artifact (Go binary, bundled JS)"
        echo "  - Small project where dev/stage separation is overkill"
        echo ""
        echo "Proceeding with single-service mode..."
        echo ""
    fi

    if [ -z "$dev_id" ] || [ -z "$dev_name" ] || [ -z "$stage_id" ] || [ -z "$stage_name" ]; then
        echo "❌ Usage: .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}"
        echo ""
        echo "Example:"
        echo "  .zcp/workflow.sh create_discovery 'abc123' 'appdev' 'def456' 'appstage'"
        echo ""
        echo "Or for single-service mode:"
        echo "  .zcp/workflow.sh create_discovery --single 'abc123' 'myservice'"
        return 1
    fi

    if ! command -v jq &>/dev/null; then
        echo "❌ jq required but not found"
        return 1
    fi

    local session_id
    session_id=$(get_session)
    if [ -z "$session_id" ]; then
        echo "❌ No active session. Run: .zcp/workflow.sh init"
        return 1
    fi

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --arg did "$dev_id" \
        --arg dname "$dev_name" \
        --arg stid "$stage_id" \
        --arg stname "$stage_name" \
        --argjson single "$single_mode" \
        '{
            session_id: $sid,
            timestamp: $ts,
            single_mode: $single,
            dev: {
                id: $did,
                name: $dname
            },
            stage: {
                id: $stid,
                name: $stname
            }
        }' > "$DISCOVERY_FILE"

    echo "✅ Discovery recorded: $DISCOVERY_FILE"
    echo ""
    echo "Dev:   $dev_name ($dev_id)"
    echo "Stage: $stage_name ($stage_id)"
    if [ "$single_mode" = true ]; then
        echo "Mode:  SINGLE-SERVICE (dev = stage)"
    fi
    echo ""
    echo "📋 Next: .zcp/workflow.sh transition_to DEVELOP"
}

cmd_refresh_discovery() {
    if [ ! -f "$DISCOVERY_FILE" ]; then
        echo "❌ No existing discovery to refresh"
        echo "   Run create_discovery first"
        return 1
    fi

    local old_dev old_stage session_id
    old_dev=$(jq -r '.dev.name' "$DISCOVERY_FILE")
    old_stage=$(jq -r '.stage.name' "$DISCOVERY_FILE")
    session_id=$(get_session)

    echo "Current discovery:"
    echo "  Dev:   $old_dev"
    echo "  Stage: $old_stage"
    echo ""

    local pid
    pid=$(cat /tmp/projectId 2>/dev/null || echo "$projectId")
    if [ -z "$pid" ]; then
        echo "❌ No projectId found"
        return 1
    fi

    echo "Available services:"
    zcli service list -P "$pid" 2>/dev/null | grep -v "^Using config" | head -15
    echo ""

    # Check if services still exist
    local services
    services=$(zcli service list -P "$pid" 2>/dev/null)
    local dev_exists=false
    local stage_exists=false

    if echo "$services" | grep -q "$old_dev"; then
        dev_exists=true
    fi
    if echo "$services" | grep -q "$old_stage"; then
        stage_exists=true
    fi

    if $dev_exists && $stage_exists; then
        echo "✓ Existing dev/stage pair still valid"
        echo "  No changes needed"
    else
        echo "⚠️  Discovery may be stale:"
        $dev_exists || echo "  - Dev '$old_dev' not found"
        $stage_exists || echo "  - Stage '$old_stage' not found"
        echo ""
        echo "Run create_discovery with updated service IDs"
    fi
}
