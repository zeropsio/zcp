#!/bin/bash
# Discovery commands for Zerops Workflow

cmd_create_discovery() {
    # GATE: Bootstrap mode - must complete ALL bootstrap tasks before discovery
    local mode
    mode=$(get_mode 2>/dev/null)
    if [ "$mode" = "bootstrap" ]; then
        local bootstrap_complete_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"
        local bootstrap_status=""

        # Check file exists AND has status "completed"
        if [ -f "$bootstrap_complete_file" ]; then
            bootstrap_status=$(jq -r '.status // ""' "$bootstrap_complete_file" 2>/dev/null)
        fi

        if [ "$bootstrap_status" != "completed" ]; then
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "❌ BOOTSTRAP IN PROGRESS - NOT COMPLETE"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""

            local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"
            if [ -f "$handoff_file" ]; then
                echo "Complete agent tasks first, then run:"
                echo "   .zcp/workflow.sh bootstrap-done"
            else
                echo "To check next step:"
                echo "   .zcp/bootstrap.sh resume"
            fi

            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            return 1
        fi
    fi

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

# Gap 45: Create multi-service discovery with deploy order
# Usage: .zcp/workflow.sh create_discovery_multi \
#   workerdev:worker123:workerstage:worker456:1 \
#   apidev:api123:apistage:api456:2:workerstage
#
# Format: dev_name:dev_id:stage_name:stage_id:order[:depends_on]
cmd_create_discovery_multi() {
    local mode
    mode=$(get_mode 2>/dev/null)

    # GATE: Bootstrap mode check (same as create_discovery)
    if [ "$mode" = "bootstrap" ]; then
        local bootstrap_complete_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"
        local bootstrap_status=""

        if [ -f "$bootstrap_complete_file" ]; then
            bootstrap_status=$(jq -r '.status // ""' "$bootstrap_complete_file" 2>/dev/null)
        fi

        if [ "$bootstrap_status" != "completed" ]; then
            echo "❌ BOOTSTRAP IN PROGRESS - NOT COMPLETE"
            echo "   Complete bootstrap first: .zcp/workflow.sh bootstrap-done"
            return 1
        fi
    fi

    if [ $# -lt 1 ]; then
        echo "❌ Usage: .zcp/workflow.sh create_discovery_multi {service_def} [{service_def}...]"
        echo ""
        echo "Format: dev_name:dev_id:stage_name:stage_id:order[:depends_on]"
        echo ""
        echo "Example:"
        echo "  .zcp/workflow.sh create_discovery_multi \\"
        echo "    workerdev:worker123:workerstage:worker456:1 \\"
        echo "    apidev:api123:apistage:api456:2:workerstage"
        echo ""
        echo "This creates discovery.json with deploy_order and depends_on fields."
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

    # Build services array
    local services_json="[]"
    local service_count=0

    for service_def in "$@"; do
        # Parse: dev_name:dev_id:stage_name:stage_id:order[:depends_on]
        IFS=':' read -r dev_name dev_id stage_name stage_id order depends_on <<< "$service_def"

        if [ -z "$dev_name" ] || [ -z "$dev_id" ] || [ -z "$stage_name" ] || [ -z "$stage_id" ]; then
            echo "❌ Invalid service definition: $service_def"
            echo "   Format: dev_name:dev_id:stage_name:stage_id:order[:depends_on]"
            return 1
        fi

        # Default order to service count + 1 if not specified
        if [ -z "$order" ]; then
            order=$((service_count + 1))
        fi

        # Build service JSON
        local service_json
        if [ -n "$depends_on" ]; then
            service_json=$(jq -n \
                --arg dn "$dev_name" \
                --arg di "$dev_id" \
                --arg sn "$stage_name" \
                --arg si "$stage_id" \
                --argjson order "$order" \
                --arg deps "$depends_on" \
                '{
                    dev: { name: $dn, id: $di },
                    stage: { name: $sn, id: $si },
                    deploy_order: $order,
                    depends_on: [$deps]
                }')
        else
            service_json=$(jq -n \
                --arg dn "$dev_name" \
                --arg di "$dev_id" \
                --arg sn "$stage_name" \
                --arg si "$stage_id" \
                --argjson order "$order" \
                '{
                    dev: { name: $dn, id: $di },
                    stage: { name: $sn, id: $si },
                    deploy_order: $order
                }')
        fi

        services_json=$(echo "$services_json" | jq --argjson svc "$service_json" '. + [$svc]')
        service_count=$((service_count + 1))
    done

    # Get first service for backward compat
    local first_dev_name first_dev_id first_stage_name first_stage_id
    first_dev_name=$(echo "$services_json" | jq -r '.[0].dev.name')
    first_dev_id=$(echo "$services_json" | jq -r '.[0].dev.id')
    first_stage_name=$(echo "$services_json" | jq -r '.[0].stage.name')
    first_stage_id=$(echo "$services_json" | jq -r '.[0].stage.id')

    # Create discovery.json with services array
    jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --argjson count "$service_count" \
        --argjson services "$services_json" \
        --arg did "$first_dev_id" \
        --arg dname "$first_dev_name" \
        --arg stid "$first_stage_id" \
        --arg stname "$first_stage_name" \
        '{
            session_id: $sid,
            timestamp: $ts,
            service_count: $count,
            services: $services,
            dev: { id: $did, name: $dname },
            stage: { id: $stid, name: $stname }
        }' > "$DISCOVERY_FILE"

    echo "✅ Multi-service discovery recorded: $DISCOVERY_FILE"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📦 SERVICES ($service_count total)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Display services with deploy order
    jq -r '.services | sort_by(.deploy_order // 99) | .[] |
        "[\(.deploy_order // "?")] \(.dev.name) → \(.stage.name)" +
        if .depends_on then " (after: \(.depends_on | join(", ")))" else "" end' \
        "$DISCOVERY_FILE"

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "⚠️  Deploy in this order. Wait for each before starting next."
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "📋 Next: .zcp/workflow.sh transition_to DEVELOP"
}

# Gap 46: Detect if multiple services share a database
# Sets shared_database flag in discovery.json
cmd_detect_shared_database() {
    if [ ! -f "$DISCOVERY_FILE" ]; then
        echo "❌ No discovery.json found"
        return 1
    fi

    local service_count
    service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

    if [ "$service_count" -le 1 ]; then
        echo "ℹ️  Single service - no shared database possible"
        return 0
    fi

    echo "Checking for shared database among $service_count services..."
    echo ""

    local services_with_db=0
    local db_services=""

    # Check each dev service for db_connectionString
    while IFS= read -r svc; do
        [ -z "$svc" ] && continue

        if ssh "$svc" 'echo $db_connectionString' 2>/dev/null | grep -q .; then
            services_with_db=$((services_with_db + 1))
            db_services="$db_services $svc"
            echo "  ✓ $svc: has \$db_connectionString"
        else
            echo "  ✗ $svc: no database connection"
        fi
    done < <(jq -r '.services[].dev.name' "$DISCOVERY_FILE" 2>/dev/null)

    echo ""

    if [ "$services_with_db" -gt 1 ]; then
        # Update discovery with shared_database flag
        jq '.shared_database = true' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp" && \
            mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"

        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "⚠️  SHARED DATABASE DETECTED"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        echo "Services sharing database:$db_services"
        echo ""
        echo "Migration guidance will appear in DEVELOP phase."
        echo "Choose ONE service to run migrations using zsc execOnce."
    else
        # Remove flag if previously set
        jq '.shared_database = false' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp" && \
            mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"

        echo "ℹ️  No shared database detected"
    fi
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
