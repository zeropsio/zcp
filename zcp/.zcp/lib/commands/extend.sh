#!/bin/bash
# Extension commands for Zerops Workflow

create_import_evidence() {
    local import_file="$1"
    local session_id
    session_id=$(get_session)
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local pid
    pid=$(cat /tmp/projectId 2>/dev/null || echo "$projectId")

    # Get service list
    local services_json="[]"
    if command -v zcli &>/dev/null && [ -n "$pid" ]; then
        services_json=$(zcli service list -P "$pid" --format json 2>/dev/null | \
            sed 's/\x1b\[[0-9;]*m//g' | \
            jq '[.services[] | {name: .name, id: .id, type: .type, status: .status}]' 2>/dev/null || echo "[]")
    fi

    local evidence
    evidence=$(jq -n \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --arg if "$import_file" \
        --argjson svcs "$services_json" \
        '{
            session_id: $sid,
            timestamp: $ts,
            import_file: $if,
            import_successful: true,
            services_created: $svcs,
            ready_for_deployment: true
        }')

    safe_write_json "$SERVICES_IMPORTED_FILE" "$evidence"
    echo "✓ Import evidence created: $SERVICES_IMPORTED_FILE"
}

cmd_extend() {
    local import_file="$1"
    local skip_validation="${2:-}"

    if [ -z "$import_file" ] || [ "$import_file" = "--help" ]; then
        show_topic_help "extend"
        return 0
    fi

    # Check for active session
    local session_id
    session_id=$(get_session)
    if [ -z "$session_id" ]; then
        echo "❌ No active session"
        echo ""
        echo "   Run first: .zcp/workflow.sh init"
        echo ""
        echo "   The init command starts a session and Gate 0 will guide you"
        echo "   to run recipe-search.sh before creating import files."
        return 1
    fi

    # Check Gate 0 (recipe review) - required before extending
    if [ ! -f "$RECIPE_REVIEW_FILE" ]; then
        echo "❌ Gate 0: Recipe review required before importing services"
        echo ""
        echo "   Run: .zcp/recipe-search.sh quick {runtime} [managed-service]"
        echo "   Example: .zcp/recipe-search.sh quick go postgresql"
        echo ""
        echo "   This provides valid patterns for your import.yml:"
        echo "   • Correct version strings (go@1 not go@latest)"
        echo "   • Required fields (buildFromGit, zeropsSetup, etc.)"
        echo "   • Production patterns from official recipes"
        return 1
    fi

    if [ ! -f "$import_file" ]; then
        echo "❌ File not found: $import_file"
        return 1
    fi

    # =========================================================================
    # GATE 0.5: IMPORT VALIDATION (prevents READY_TO_DEPLOY failures)
    # =========================================================================
    # This gate was added after a documented failure where an agent:
    # - Read the recipe showing buildFromGit and zeropsSetup
    # - Created import.yml WITHOUT these critical fields
    # - Caused services to be stuck in READY_TO_DEPLOY (empty containers)
    # =========================================================================

    if [ "$skip_validation" != "--skip-validation" ]; then
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "Gate 0.5: Import Validation"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""

        # Run the validator
        if ! "$SCRIPT_DIR/../validate-import.sh" "$import_file"; then
            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "❌ IMPORT BLOCKED - Fix validation errors first"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "Why this gate exists:"
            echo "  A past failure occurred where import.yml was created without"
            echo "  buildFromGit and zeropsSetup, causing empty READY_TO_DEPLOY"
            echo "  services that couldn't run any code."
            echo ""
            echo "To bypass (NOT recommended):"
            echo "  .zcp/workflow.sh extend $import_file --skip-validation"
            echo ""
            return 1
        fi

        echo ""
    else
        echo "⚠️  Skipping import validation (--skip-validation flag)"
        echo "   You're responsible for ensuring import.yml is correct."
        echo ""
    fi

    local pid
    pid=$(cat /tmp/projectId 2>/dev/null || echo "$projectId")
    if [ -z "$pid" ]; then
        echo "❌ No projectId found"
        return 1
    fi

    echo "📦 Importing services from: $import_file"
    echo ""

    if ! zcli project service-import "$import_file" -P "$pid"; then
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

    # Create evidence for Gate 2
    create_import_evidence "$import_file"

    # =========================================================================
    # POST-IMPORT STATUS CHECK: Detect READY_TO_DEPLOY (missing buildFromGit)
    # =========================================================================
    # If services are stuck in READY_TO_DEPLOY, it usually means:
    # 1. import.yml was missing buildFromGit (they have no code)
    # 2. OR startWithoutCode:true was intentional (dev mode)
    # This check warns the agent so they can fix before wasting time debugging
    # =========================================================================

    local ready_to_deploy_services
    ready_to_deploy_services=$(zcli service list -P "$pid" --format json 2>/dev/null | \
        sed 's/\x1b\[[0-9;]*m//g' | \
        jq -r '.services[] | select(.status == "READY_TO_DEPLOY") | "\(.name) (\(.type))"' 2>/dev/null)

    if [ -n "$ready_to_deploy_services" ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "⚠️  WARNING: Services in READY_TO_DEPLOY status"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        echo "$ready_to_deploy_services" | while read -r line; do
            echo "  • $line"
        done
        echo ""
        echo "This means these services have NO CODE deployed."
        echo ""
        echo "If this is EXPECTED (dev mode with SSHFS):"
        echo "  1. Mount the service: .zcp/mount.sh {service}"
        echo "  2. Add your code to /var/www/{service}/"
        echo "  3. The service will start when code is present"
        echo ""
        echo "If this is UNEXPECTED:"
        echo "  Your import.yml is likely missing 'buildFromGit'."
        echo "  The recipe showed this field but it wasn't included."
        echo ""
        echo "  To fix:"
        echo "  1. Delete these services and re-import with buildFromGit"
        echo "  2. Or deploy code manually: ssh {service} 'zcli push'"
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    fi

    # Detect DEV runtime services that need SSHFS mounts (not stage - stage gets code via zcli push)
    local dev_services
    dev_services=$(zcli service list -P "$pid" --format json 2>/dev/null | \
        sed 's/\x1b\[[0-9;]*m//g' | \
        jq -r '.services[] | select(.type | test("^(go|nodejs|php|python|rust|dotnet|java|static|nginx|alpine|bun)@")) | select(.name | test("stage") | not) | .name' 2>/dev/null || true)

    if [ -n "$dev_services" ]; then
        echo ""
        echo "📂 SSHFS MOUNTS FOR DEV SERVICES"
        echo ""
        echo "   ⚠️  Dev services need startWithoutCode: true in import.yml"
        echo "   Stage services don't need mounts - they get code via zcli push"
        echo ""
        echo "   Once dev service is RUNNING, create mount:"
        echo ""
        for svc in $dev_services; do
            # Check if mount already exists
            if [ ! -d "/var/www/$svc" ] || [ -z "$(ls -A /var/www/$svc 2>/dev/null)" ]; then
                echo "   .zcp/mount.sh $svc"
            fi
        done
    fi

    echo ""
    echo "⚠️  IMPORTANT: Environment Variable Timing"
    echo "   New services' vars are NOT visible in ZCP until restart."
    echo ""
    echo "   To access new credentials:"
    echo "   Option A: Restart ZCP (reconnect your IDE) - recommended"
    echo "   Option B: Use connection string: ssh db 'echo \$connectionString'"
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

    local phase
    phase=$(get_phase)

    # Check dev work is complete
    if [ "$phase" != "DONE" ]; then
        echo "❌ Complete dev-only workflow first (reach DONE phase)"
        echo "   Current phase: $phase"
        echo ""
        echo "   Complete dev iteration first, then upgrade."
        return 1
    fi

    # Check dev verification exists
    if [ ! -f "$DEV_VERIFY_FILE" ]; then
        echo "❌ Dev verification required before upgrading"
        echo "   Run: .zcp/verify.sh {dev} {port} /"
        return 1
    fi

    echo "Upgrading to full deployment mode..."

    # Change mode using set_mode (which calls sync_state)
    set_mode "full"

    # Reset to DEVELOP (not DONE)
    set_phase "DEVELOP"

    echo "✅ Upgraded to full mode"
    echo ""
    echo "Next steps:"
    echo "  1. .zcp/workflow.sh transition_to DEPLOY"
    echo "  2. Deploy to stage"
    echo "  3. .zcp/workflow.sh transition_to VERIFY"
    echo "  4. Verify stage"
    echo "  5. .zcp/workflow.sh transition_to DONE"
    echo ""

    output_phase_guidance "DEVELOP"
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
