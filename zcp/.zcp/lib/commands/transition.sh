#!/bin/bash
# Phase transition commands for Zerops Workflow

cmd_transition_to() {
    local target_phase="$1"
    local back_flag=""

    # Handle --back flag
    if [ "$1" = "--back" ]; then
        back_flag="--back"
        shift
        target_phase="$1"
    fi

    if [ -z "$target_phase" ]; then
        echo "❌ Usage: .zcp/workflow.sh transition_to [--back] {phase}"
        echo "Phases: DISCOVER, DEVELOP, DEPLOY, VERIFY, DONE"
        echo ""
        echo "Full mode:     INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE"
        echo "Dev-only mode: INIT → DISCOVER → DEVELOP → DONE"
        echo ""
        echo "Options:"
        echo "  --back    Go backward (invalidates evidence)"
        echo ""
        echo "For new projects, use bootstrap first:"
        echo "  .zcp/workflow.sh bootstrap --runtime <type> --services <list>"
        return 1
    fi

    if ! validate_phase "$target_phase"; then
        echo "❌ Invalid phase: $target_phase"
        echo "Valid phases: ${PHASES[*]}"
        return 1
    fi

    local current_phase
    local mode
    current_phase=$(get_phase)
    mode=$(get_mode)

    # GATE: Bootstrap mode - must complete ALL bootstrap tasks before transitions
    if [ "$mode" = "bootstrap" ]; then
        local bootstrap_complete_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"
        local bootstrap_status=""

        # Check file exists AND has status "completed" (not just "agent_handoff")
        if [ -f "$bootstrap_complete_file" ]; then
            bootstrap_status=$(jq -r '.status // ""' "$bootstrap_complete_file" 2>/dev/null)
        fi

        if [ "$bootstrap_status" != "completed" ]; then
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "❌ BOOTSTRAP IN PROGRESS - NOT COMPLETE"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "Workflow transitions are BLOCKED until bootstrap completes."
            echo ""

            # Check if handoff exists (infrastructure done, code generation pending)
            local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"
            local state_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
            if [ -f "$handoff_file" ]; then
                echo "Infrastructure is ready, but code generation not complete."
                echo ""
                echo "Services to set up:"
                jq -r '.service_handoffs[] | "   • \(.dev_hostname): \(.mount_path)/"' "$handoff_file" 2>/dev/null
                echo ""
                echo "Tasks:"
                echo "   1. Create zerops.yml with build/deploy/run config"
                echo "   2. Write application code"
                echo "   3. Push and verify dev/stage"
                echo "   4. Run: .zcp/workflow.sh bootstrap-done"
            elif [ -f "$state_file" ]; then
                local checkpoint
                checkpoint=$(jq -r '.checkpoint // "unknown"' "$state_file" 2>/dev/null)
                echo "Bootstrap in progress at checkpoint: $checkpoint"
                echo ""
                echo "To check next step:"
                echo "   .zcp/bootstrap.sh resume"
            else
                echo "Bootstrap hasn't started."
                echo ""
                echo "To start:"
                echo "   .zcp/workflow.sh bootstrap --runtime <type> --services <list>"
            fi

            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            return 1
        fi

        # Bootstrap truly complete - switch to full mode for normal workflow
        echo "Bootstrap complete. Switching to full workflow mode."
        set_mode "full"
        mode="full"
    fi

    # Check if already in target phase (show guidance anyway)
    if [ "$current_phase" = "$target_phase" ] && [ "$back_flag" != "--back" ]; then
        echo "⚠️  Already in $target_phase phase. Showing guidance:"
        echo ""
        output_phase_guidance "$target_phase"
        return 0
    fi

    # In quick mode, allow any transition
    if [ "$mode" = "quick" ]; then
        set_phase "$target_phase"
        output_phase_guidance "$target_phase"
        return 0
    fi

    # In dev-only mode, truncated flow: DISCOVER → DEVELOP → DONE
    if [ "$mode" = "dev-only" ]; then
        case "$target_phase" in
            DISCOVER|DEVELOP)
                set_phase "$target_phase"
                output_phase_guidance "$target_phase"
                return 0
                ;;
            DONE)
                if [ "$current_phase" = "DEVELOP" ]; then
                    echo "✅ Dev-only workflow complete"
                    echo ""
                    echo "💡 To deploy this work later:"
                    echo "   .zcp/workflow.sh upgrade-to-full"
                    set_phase "$target_phase"
                    return 0
                fi
                ;;
            DEPLOY|VERIFY)
                echo "❌ DEPLOY/VERIFY not available in dev-only mode"
                echo ""
                echo "💡 To enable deployment:"
                echo "   .zcp/workflow.sh upgrade-to-full"
                return 1
                ;;
        esac
    fi

    # In hotfix mode, skip discovery and dev verification
    if [ "$mode" = "hotfix" ]; then
        case "$target_phase" in
            DEPLOY)
                # Skip dev verification gate in hotfix mode
                set_phase "$target_phase"
                echo "🚨 HOTFIX: Skipping dev verification"
                output_phase_guidance "$target_phase"
                return 0
                ;;
            VERIFY|DONE)
                # Still enforce verification in hotfix mode
                ;;
        esac
    fi

    # Handle backward transitions with --back flag
    if [ "$back_flag" = "--back" ]; then
        case "$(get_phase)→$target_phase" in
            VERIFY→DEVELOP|DEPLOY→DEVELOP)
                rm -f "$STAGE_VERIFY_FILE"
                rm -f "$DEPLOY_EVIDENCE_FILE" 2>/dev/null
                echo "⚠️  Backward transition: Stage verification evidence invalidated"
                echo "   You will need to re-verify stage after redeploying"
                set_phase "$target_phase"
                output_phase_guidance "$target_phase"
                return 0
                ;;
            DONE→VERIFY)
                echo "⚠️  Backward transition: Re-verification mode"
                set_phase "$target_phase"
                output_phase_guidance "$target_phase"
                return 0
                ;;
            *)
                echo "❌ Cannot go back to $target_phase from $(get_phase)"
                echo ""
                echo "Allowed backward transitions:"
                echo "  VERIFY → DEVELOP (invalidates stage evidence)"
                echo "  DEPLOY → DEVELOP (invalidates stage evidence)"
                echo "  DONE → VERIFY"
                return 1
                ;;
        esac
    fi

    # In full mode, enforce gates
    case "$target_phase" in
        COMPOSE|EXTEND|SYNTHESIZE)
            # DEPRECATED: These phases are removed. Use bootstrap instead.
            echo "❌ $target_phase phase is deprecated."
            echo ""
            echo "Use the bootstrap command instead:"
            echo "  .zcp/workflow.sh bootstrap --runtime <type> --services <list>"
            echo ""
            echo "For help: .zcp/workflow.sh bootstrap --help"
            return 2
            ;;
        DISCOVER)
            if [ "$current_phase" != "INIT" ]; then
                echo "❌ Cannot transition to DISCOVER from $current_phase"
                echo "📋 Run: .zcp/workflow.sh init"
                return 2
            fi
            # Gate 0: Recipe Discovery
            if ! check_gate_init_to_discover; then
                return 2
            fi
            ;;
        DEVELOP)
            if [ "$current_phase" != "DISCOVER" ]; then
                echo "❌ Cannot transition to DEVELOP from $current_phase"
                echo "📋 Required flow: DISCOVER → DEVELOP"
                return 2
            fi
            if ! check_gate_discover_to_develop; then
                return 2
            fi
            ;;
        DEPLOY)
            if [ "$current_phase" != "DEVELOP" ]; then
                echo "❌ Cannot transition to DEPLOY from $current_phase"
                echo "📋 Required flow: DEVELOP → DEPLOY"
                return 2
            fi
            if ! check_gate_develop_to_deploy; then
                return 2
            fi
            ;;
        VERIFY)
            if [ "$current_phase" != "DEPLOY" ]; then
                echo "❌ Cannot transition to VERIFY from $current_phase"
                echo "📋 Required flow: DEPLOY → VERIFY"
                return 2
            fi
            if ! check_gate_deploy_to_verify; then
                return 2
            fi
            ;;
        DONE)
            if [ "$current_phase" != "VERIFY" ]; then
                echo "❌ Cannot transition to DONE from $current_phase"
                echo "📋 Required flow: VERIFY → DONE"
                return 2
            fi
            if ! check_gate_verify_to_done; then
                return 2
            fi
            ;;
    esac

    set_phase "$target_phase"
    sync_to_persistent
    output_phase_guidance "$target_phase"
}

# ============================================================================
# REMOVED: Synthesis phase guidance functions
# ============================================================================
# Use bootstrap instead: .zcp/workflow.sh bootstrap --runtime <type> --services <list>

# ============================================================================
# WIGGUM STATE BLOCK OUTPUT
# ============================================================================

emit_wiggum_state_block() {
    echo ""
    echo "┌─────────────────────────────────────────────────────────────────────────────┐"
    echo "│  WORKFLOW STATE (JSON)                                                       │"
    echo "└─────────────────────────────────────────────────────────────────────────────┘"
    echo ""

    # Update and emit workflow state
    if type update_workflow_state &>/dev/null; then
        update_workflow_state 2>/dev/null
        if [ -f "$WORKFLOW_STATE_FILE" ]; then
            cat "$WORKFLOW_STATE_FILE" 2>/dev/null | jq '.' 2>/dev/null || cat "$WORKFLOW_STATE_FILE" 2>/dev/null
        fi
    fi
}

# ============================================================================
# DISCOVER PHASE GUIDANCE (Detects Bootstrap vs Standard Flow)
# ============================================================================

output_discover_guidance() {
    echo "✅ Phase: DISCOVER"
    echo ""

    # STEP 1: Check if we need to login first
    # Try a simple zcli command to test authentication
    local pid
    pid=$(cat /tmp/projectId 2>/dev/null || echo "${projectId:-}")

    if [ -z "$pid" ]; then
        cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  NO PROJECT ID FOUND
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Cannot detect services without project ID.
Check that $projectId is set or /tmp/projectId exists.

This usually means you're not running on ZCP.
EOF
        return
    fi

    # Check if zcli is available and authenticated
    local zcli_test_result
    zcli_test_result=$(zcli service list -P "$pid" --format json 2>&1)
    local zcli_exit=$?

    # Check for auth errors specifically
    if [ $zcli_exit -ne 0 ] && echo "$zcli_test_result" | grep -qiE "unauthorized|auth|login|token|403"; then
        cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔐 LOGIN REQUIRED FIRST
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

zcli is not authenticated. Run:

   zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "\$ZCP_API_KEY"

Then re-run:

   .zcp/workflow.sh transition_to DISCOVER

The workflow will then detect your services and show
the appropriate next steps (bootstrap or standard flow).
EOF
        return
    fi

    # Now try to detect services (zcli should be working)
    local has_services=false
    local detection_error=""

    if check_runtime_services_exist 2>/dev/null; then
        has_services=true
    else
        # Capture why detection failed for user guidance
        if [ -z "$DETECTED_SERVICES_JSON" ] || [ "$DETECTED_SERVICES_JSON" = "[]" ]; then
            detection_error="No runtime services found"
        fi
    fi

    if [ "$has_services" = true ]; then
        # STANDARD FLOW: Services exist, just discover and record them
        cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 STANDARD FLOW: Runtime services detected
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

EOF
        echo "Existing services:"
        get_services_summary
        echo ""

        cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Record discovery:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   zcli service list -P $projectId

   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

⚠️  Use service IDs (from list), not hostnames
⚠️  Never use 'zcli scope' - it's buggy

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: /tmp/discovery.json must exist
📋 Next: .zcp/workflow.sh transition_to DEVELOP
EOF

    else
        # BOOTSTRAP FLOW: No services, need to create them
        cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🚀 BOOTSTRAP FLOW: No runtime services found
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF
        # Show detection context if available
        if [ -n "$detection_error" ]; then
            echo ""
            echo "ℹ️  Detection: $detection_error"
        fi
        cat <<'EOF'

You need to CREATE services before you can discover them.
Follow these steps IN ORDER:

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 1: Review recipes (REQUIRED - Gate 0)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   .zcp/recipe-search.sh quick {runtime} [managed-service]

   Example: .zcp/recipe-search.sh quick go postgresql

   This creates /tmp/recipe_review.json with:
   • Correct YAML structure
   • Production patterns (alpine, cache, etc.)
   • Environment variable patterns
   Note: Version strings now come from docs.zerops.io/data.json via plan.json

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 2: Use Bootstrap for New Projects
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   For NEW projects without services, use bootstrap:

   .zcp/workflow.sh bootstrap --runtime <types> --services <types>

   Use user's exact words. This creates services and guides the agent to:
   • Complete zerops.yml with build commands
   • Write minimal status page code
   • Push and test

   See: .zcp/workflow.sh bootstrap --help

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 3 (manual): Get/Create import.yml
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF

        # Check if we have a ready-made import.yml from recipe
        local has_ready_import="false"
        local pattern_source=""
        if [ -f "$RECIPE_REVIEW_FILE" ]; then
            has_ready_import=$(jq -r '.has_ready_import_yml // false' "$RECIPE_REVIEW_FILE" 2>/dev/null)
            pattern_source=$(jq -r '.pattern_source // ""' "$RECIPE_REVIEW_FILE" 2>/dev/null)
        fi

        if [ "$has_ready_import" = "true" ]; then
            cat <<'EOF'

   ✅ Recipe found with ready-made import.yml!

   1. Read the recipe file (check /tmp/recipe_*.md for your runtime)
   2. Find the import.yml section (look for "services:")
   3. Copy it EXACTLY to import.yml - don't cherry-pick fields!

   ⚠️  USE THE RECIPE'S IMPORT.YML - DON'T INVENT YOUR OWN!

   The recipe's import.yml includes CRITICAL fields:
     • buildFromGit: <repo-url>  → Initial code deployment
     • zeropsSetup: dev/prod     → Links to zerops.yml build config

   ❌ A past failure occurred when these were omitted:
      Services ended up in READY_TO_DEPLOY (empty containers)
EOF
        else
            cat <<'EOF'

   Documentation fallback - construct import.yml manually:

   services:
     - hostname: appstage
       type: go@1
       zeropsSetup: prod                    # ← CRITICAL: links to zerops.yml
       buildFromGit: https://github.com/...  # ← CRITICAL: initial code
       enableSubdomainAccess: true

     - hostname: appdev
       type: go@1
       zeropsSetup: dev                     # ← CRITICAL: links to zerops.yml
       buildFromGit: https://github.com/...  # ← OR use startWithoutCode: true
       enableSubdomainAccess: true

     - hostname: db
       type: postgresql@17
       mode: NON_HA

   Reference: Check /tmp/recipe_*.md for your runtime's documentation
EOF
        fi

        cat <<'EOF'

   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   ⚠️  CRITICAL FIELDS FOR RUNTIME SERVICES:
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   buildFromGit: <url>     Zerops clones & deploys from this repo
       OR
   startWithoutCode: true  Dev mode - use SSHFS mount for code

   zeropsSetup: dev/prod   Links import.yml to zerops.yml setup block
                           Without this, Zerops doesn't know HOW to build

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 4: Import services (REQUIRED)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   .zcp/workflow.sh extend import.yml

   This will:
   • Create the services in Zerops
   • Wait for them to be ready
   • Create /tmp/services_imported.json evidence

   ⚠️  After import, restart ZCP to get new env vars!

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 5: Record discovery (REQUIRED)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   zcli service list -P $projectId

   .zcp/workflow.sh create_discovery {dev_id} appdev {stage_id} appstage

⚠️  Use service IDs (from list), not hostnames

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: /tmp/discovery.json must exist
📋 Next: .zcp/workflow.sh transition_to DEVELOP
EOF
    fi
}

# ============================================================================
# DYNAMIC GUIDANCE HELPERS (FIX-03: Resolve placeholders from discovery.json)
# ============================================================================

# Get runtime-specific defaults (duplicated from status.sh for independence)
_get_runtime_defaults() {
    local runtime="$1"
    case "$runtime" in
        go|go@*) echo "proc=app binary=app build='go build -o app' port=8080" ;;
        nodejs|nodejs@*|node|node@*) echo "proc=node binary='node index.js' build='npm install && npm run build' port=8080" ;;
        bun|bun@*) echo "proc=bun binary='bun run index.ts' build='bun install' port=8080" ;;
        python|python@*) echo "proc=python binary='python app.py' build='pip install -r requirements.txt' port=8080" ;;
        rust|rust@*) echo "proc=app binary='./target/release/app' build='cargo build --release' port=8080" ;;
        php|php@*) echo "proc=php binary='php -S 0.0.0.0:8080' build='composer install' port=8080" ;;
        dotnet|dotnet@*) echo "proc=dotnet binary='dotnet run' build='dotnet build' port=8080" ;;
        java|java@*) echo "proc=java binary='java -jar app.jar' build='mvn package' port=8080" ;;
        *) echo "proc=app binary='./app' build='<see zerops.yml>' port=8080" ;;
    esac
}

# Output DEVELOP phase with resolved values
_output_develop_guidance_resolved() {
    local shared_db="$1"

    if [ ! -f "$DISCOVERY_FILE" ]; then
        cat <<'EOF'
✅ Phase: DEVELOP

⚠️  No discovery.json found. Run discovery first:
   .zcp/workflow.sh transition_to DISCOVER

Once discovered, this will show exact commands for your services.
EOF
        return
    fi

    local service_count
    service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

    echo "✅ Phase: DEVELOP"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📁 Files: /var/www/{service}/     (edit directly via SSHFS)"
    echo "💻 Run:   ssh {service} \"cmd\"    (execute inside container)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "⚠️  CRITICAL: Dev is where you iterate and fix errors."
    echo "    Stage is for final validation AFTER dev confirms success."
    echo ""

    # Output commands for EACH service with RESOLVED values
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📦 SERVICES ($service_count)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    local i=0
    while [ "$i" -lt "$service_count" ]; do
        local dev_name runtime
        if [ "$service_count" -eq 1 ]; then
            dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
            runtime=$(jq -r '.runtime // "unknown"' "$DISCOVERY_FILE" 2>/dev/null)
        else
            dev_name=$(jq -r ".services[$i].dev.name // \"service$i\"" "$DISCOVERY_FILE" 2>/dev/null)
            runtime=$(jq -r ".services[$i].runtime // \"unknown\"" "$DISCOVERY_FILE" 2>/dev/null)
        fi

        local proc binary build port
        eval "$(_get_runtime_defaults "$runtime")"

        echo "[$((i+1))] $dev_name ($runtime)"
        echo "────────────────────────────────────────────────────────────────"
        echo ""
        echo "Kill existing process:"
        echo "  ssh $dev_name 'pkill -9 $proc; killall -9 $proc 2>/dev/null; fuser -k $port/tcp 2>/dev/null; true'"
        echo ""
        echo "Build & run:"
        echo "  ssh $dev_name \"cd /var/www && $build\""
        echo "  ssh $dev_name \"cd /var/www && $binary >> /tmp/app.log 2>&1\"  # run_in_background=true"
        echo ""
        echo "Verify:"
        echo "  .zcp/verify.sh $dev_name $port / /health"
        echo ""

        i=$((i + 1))
    done

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "🔍 FUNCTIONAL TESTING (required before deploy):"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "HTTP 200 is NOT enough. You must verify the feature WORKS."
    echo ""

    # Use resolved service name for log check guidance
    local first_dev_name
    if [ "$service_count" -eq 1 ]; then
        first_dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
    else
        first_dev_name=$(jq -r '.services[0].dev.name // "service0"' "$DISCOVERY_FILE" 2>/dev/null)
    fi

    echo "Check logs for errors:"
    echo "  ssh $first_dev_name \"tail -50 /tmp/app.log\""
    echo "  ssh $first_dev_name \"grep -i error /tmp/app.log\""
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📋 When ready (feature works, no errors):"
    echo "   Run verify.sh for each service, then:"
    echo "   .zcp/workflow.sh transition_to DEPLOY"

    # Gap 46: Show migration guidance if shared database detected
    if [ "$shared_db" = "true" ]; then
        cat <<'MIGRATION_GUIDANCE'

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🗄️  DATABASE MIGRATIONS (shared database detected)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Choose ONE service to run migrations. In its zerops.yml:

  run:
    initCommands:
      - zsc execOnce "${appVersionId}-migrate" "./migrate.sh"

Pattern: zsc execOnce "unique-id" "command"
  • Use $appVersionId for automatic refresh on each deploy
MIGRATION_GUIDANCE
    fi
}

# Output DEPLOY phase with resolved values
_output_deploy_guidance_resolved() {
    if [ ! -f "$DISCOVERY_FILE" ]; then
        cat <<'EOF'
✅ Phase: DEPLOY

⚠️  No discovery.json found. Cannot determine service IDs.
   Run: .zcp/workflow.sh show
EOF
        return
    fi

    local service_count
    service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

    echo "✅ Phase: DEPLOY"
    echo ""
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║  ⛔ STOP - REVIEW zerops.yml BEFORE DEPLOYING                     ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "You modified zerops.yml. Read it NOW and confirm:"
    echo ""

    # Show the zerops.yml files that need review
    local i=0
    while [ "$i" -lt "$service_count" ]; do
        local dev_name
        if [ "$service_count" -eq 1 ]; then
            dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
        else
            dev_name=$(jq -r ".services[$i].dev.name" "$DISCOVERY_FILE" 2>/dev/null)
        fi
        echo "   cat /var/www/$dev_name/zerops.yml"
        i=$((i + 1))
    done

    echo ""
    echo "Check these sections are CORRECT:"
    echo "   □ build.deployFiles   — all artifacts listed (binary, static/, etc.)"
    echo "   □ run.envVariables    — all env vars needed at runtime"
    echo "   □ run.start           — correct startup command"
    echo "   □ run.ports           — matches what your app listens on"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📦 DEPLOYMENT COMMANDS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    local i=0
    while [ "$i" -lt "$service_count" ]; do
        local dev_name stage_id stage_name runtime
        if [ "$service_count" -eq 1 ]; then
            dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
            stage_id=$(jq -r '.stage.id // "STAGE_ID"' "$DISCOVERY_FILE" 2>/dev/null)
            stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)
            runtime=$(jq -r '.runtime // "unknown"' "$DISCOVERY_FILE" 2>/dev/null)
        else
            dev_name=$(jq -r ".services[$i].dev.name" "$DISCOVERY_FILE" 2>/dev/null)
            stage_id=$(jq -r ".services[$i].stage.id" "$DISCOVERY_FILE" 2>/dev/null)
            stage_name=$(jq -r ".services[$i].stage.name" "$DISCOVERY_FILE" 2>/dev/null)
            runtime=$(jq -r ".services[$i].runtime // \"unknown\"" "$DISCOVERY_FILE" 2>/dev/null)
        fi

        local proc port
        eval "$(_get_runtime_defaults "$runtime")"

        echo "[$((i+1))] $dev_name → $stage_name"
        echo "────────────────────────────────────────────────────────────────"
        echo ""
        echo "Stop dev process:"
        echo "  ssh $dev_name 'pkill -9 $proc; killall -9 $proc 2>/dev/null; fuser -k $port/tcp 2>/dev/null; true'"
        echo ""
        echo "Authenticate (if needed):"
        echo "  ssh $dev_name \"zcli login --region=gomibako \\"
        echo "      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \\"
        echo "      \\\"\\\$ZCP_API_KEY\\\"\""
        echo ""
        echo "Deploy:"
        echo "  ssh $dev_name \"cd /var/www && zcli push $stage_id --setup=prod --versionName=v1.0.0\""
        echo ""
        echo "Wait for completion:"
        echo "  .zcp/status.sh --wait $stage_name"
        echo ""

        i=$((i + 1))
    done

    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: .zcp/status.sh shows SUCCESS notification
📋 Next: .zcp/workflow.sh transition_to VERIFY
EOF
}

# Output VERIFY phase with resolved values
_output_verify_guidance_resolved() {
    if [ ! -f "$DISCOVERY_FILE" ]; then
        cat <<'EOF'
✅ Phase: VERIFY

⚠️  No discovery.json found.
   Run: .zcp/workflow.sh show
EOF
        return
    fi

    local service_count
    service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

    echo "✅ Phase: VERIFY"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📦 VERIFICATION COMMANDS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    local i=0
    while [ "$i" -lt "$service_count" ]; do
        local stage_name stage_id runtime
        if [ "$service_count" -eq 1 ]; then
            stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)
            stage_id=$(jq -r '.stage.id // "STAGE_ID"' "$DISCOVERY_FILE" 2>/dev/null)
            runtime=$(jq -r '.runtime // "unknown"' "$DISCOVERY_FILE" 2>/dev/null)
        else
            stage_name=$(jq -r ".services[$i].stage.name" "$DISCOVERY_FILE" 2>/dev/null)
            stage_id=$(jq -r ".services[$i].stage.id" "$DISCOVERY_FILE" 2>/dev/null)
            runtime=$(jq -r ".services[$i].runtime // \"unknown\"" "$DISCOVERY_FILE" 2>/dev/null)
        fi

        local port
        eval "$(_get_runtime_defaults "$runtime")"

        echo "[$((i+1))] $stage_name"
        echo "────────────────────────────────────────────────────────────────"
        echo ""
        echo "Check deployed artifacts:"
        echo "  ssh $stage_name \"ls -la /var/www/\""
        echo ""
        echo "Verify endpoints:"
        echo "  .zcp/verify.sh $stage_name $port / /health"
        echo ""
        echo "Service logs:"
        echo "  zcli service log -S $stage_id -P \$projectId --follow"
        echo ""

        i=$((i + 1))
    done

    # Get first stage name for browser testing example
    local first_stage_name
    if [ "$service_count" -eq 1 ]; then
        first_stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)
    else
        first_stage_name=$(jq -r '.services[0].stage.name // "stage0"' "$DISCOVERY_FILE" 2>/dev/null)
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "⚠️  BROWSER TESTING (required if frontend exists):"
    echo "   URL=\$(ssh $first_stage_name \"echo \\\$zeropsSubdomain\")"
    echo "   agent-browser open \"\$URL\"          # Don't prepend https://!"
    echo "   agent-browser errors               # Must show no errors"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📋 Gate: verify.sh must pass (creates /tmp/stage_verify.json)"
    echo "📋 Next: .zcp/workflow.sh transition_to DONE"
}

output_phase_guidance() {
    local phase="$1"

    case "$phase" in
        DISCOVER)
            output_discover_guidance
            ;;
        DEVELOP)
            # Gap 46: Check for shared database and show migration guidance
            local shared_db=false
            if [ -f "$DISCOVERY_FILE" ]; then
                shared_db=$(jq -r '.shared_database // false' "$DISCOVERY_FILE" 2>/dev/null)
            fi

            _output_develop_guidance_resolved "$shared_db"
            ;;
        DEPLOY)
            _output_deploy_guidance_resolved
            ;;
        VERIFY)
            _output_verify_guidance_resolved
            ;;
        DONE)
            cat <<'EOF'
✅ Phase: DONE

Run completion check:
  .zcp/workflow.sh complete

This will verify all evidence and output the completion promise.
EOF
            ;;
    esac
}
