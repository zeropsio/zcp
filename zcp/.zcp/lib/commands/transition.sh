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
        echo "Phases: COMPOSE, EXTEND, SYNTHESIZE, DISCOVER, DEVELOP, DEPLOY, VERIFY, DONE"
        echo ""
        echo "Synthesis mode: INIT → COMPOSE → EXTEND → SYNTHESIZE → DEVELOP → DEPLOY → VERIFY → DONE"
        echo "Standard mode:  INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE"
        echo ""
        echo "Options:"
        echo "  --back    Go backward (invalidates evidence)"
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
        COMPOSE)
            if [ "$current_phase" != "INIT" ]; then
                echo "❌ Cannot transition to COMPOSE from $current_phase"
                echo "📋 Run: .zcp/workflow.sh init"
                return 2
            fi
            # Gate 0: Recipe Review required before compose
            if ! check_gate_init_to_discover; then
                return 2
            fi
            ;;
        EXTEND)
            if [ "$current_phase" != "COMPOSE" ]; then
                echo "❌ Cannot transition to EXTEND from $current_phase"
                echo "📋 Required flow: INIT → COMPOSE → EXTEND"
                return 2
            fi
            # Gate: Synthesis plan must exist
            if ! check_gate_compose_to_extend; then
                return 2
            fi
            ;;
        SYNTHESIZE)
            if [ "$current_phase" != "EXTEND" ]; then
                echo "❌ Cannot transition to SYNTHESIZE from $current_phase"
                echo "📋 Required flow: COMPOSE → EXTEND → SYNTHESIZE"
                return 2
            fi
            # Gate: Services must be imported
            if ! check_gate_extend_to_synthesize; then
                return 2
            fi
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
            # Can come from DISCOVER (standard) or SYNTHESIZE (synthesis mode)
            if [ "$current_phase" != "DISCOVER" ] && [ "$current_phase" != "SYNTHESIZE" ]; then
                echo "❌ Cannot transition to DEVELOP from $current_phase"
                echo "📋 Required flow: DISCOVER → DEVELOP (standard) or SYNTHESIZE → DEVELOP (synthesis)"
                return 2
            fi
            # Gate depends on source phase
            if [ "$current_phase" = "DISCOVER" ]; then
                if ! check_gate_discover_to_develop; then
                    return 2
                fi
            elif [ "$current_phase" = "SYNTHESIZE" ]; then
                if ! check_gate_synthesis; then
                    return 2
                fi
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
# SYNTHESIS PHASE GUIDANCE - COMPOSE
# ============================================================================

output_compose_guidance() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  PHASE: COMPOSE

  Generate synthesis plan and infrastructure definition.

  COMMAND:
    .zcp/workflow.sh compose --runtime {runtime} --services {services}

  EXAMPLES:
    .zcp/workflow.sh compose --runtime go --services postgresql
    .zcp/workflow.sh compose --runtime nodejs --services postgresql,valkey
    .zcp/workflow.sh compose --runtime python

  OUTPUT:
    /tmp/synthesis_plan.json      ← Service topology, env mappings
    /tmp/synthesized_import.yml   ← Import file for extend

  NEXT: .zcp/workflow.sh transition_to EXTEND

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF

    # Emit WIGGUM state
    emit_wiggum_state_block
}

# ============================================================================
# SYNTHESIS PHASE GUIDANCE - EXTEND
# ============================================================================

output_extend_guidance() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  PHASE: EXTEND

  Import services to Zerops and wait for RUNNING.

  COMMAND:
    .zcp/workflow.sh extend /tmp/synthesized_import.yml

  AFTER SERVICES RUNNING:
    Create code files in /var/www/{dev}/:
    - zerops.yml (with zerops: wrapper)
    - main.{ext} (connectivity proof code)
    - {deps_file} (go.mod, package.json, etc.)

    Reference /tmp/synthesis_plan.json for:
    - Environment variable mappings
    - Required files list
    - Service connections

  NEXT: After creating code → .zcp/workflow.sh verify_synthesis

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF

    # Emit WIGGUM state
    emit_wiggum_state_block
}

# ============================================================================
# SYNTHESIS PHASE GUIDANCE - SYNTHESIZE
# ============================================================================

output_synthesize_guidance() {
    cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  PHASE: SYNTHESIZE

  Validate agent-created code structure.

  COMMAND:
    .zcp/workflow.sh verify_synthesis

  CHECKS:
    ✓ zerops.yml exists with zerops: wrapper
    ✓ Main code file exists
    ✓ Dependency file exists
    ✓ Session ID matches

  NEXT: .zcp/workflow.sh transition_to DEVELOP

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
EOF

    # Emit WIGGUM state
    emit_wiggum_state_block
}

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
    zcli_test_result=$(zcli service list -P "$pid" --json 2>&1)
    local zcli_exit=$?

    # Check for auth errors specifically
    if [ $zcli_exit -ne 0 ] && echo "$zcli_test_result" | grep -qiE "unauthorized|auth|login|token|403"; then
        cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔐 LOGIN REQUIRED FIRST
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

zcli is not authenticated. Run:

   zcli login $ZEROPS_ZCP_API_KEY

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
   • Valid version strings (go@1 not go@latest)
   • Correct YAML structure
   • Production patterns (alpine, cache, etc.)
   • Environment variable patterns

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 2: Plan service topology (RECOMMENDED)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   .zcp/workflow.sh plan_services {runtime} [managed-service]

   Example: .zcp/workflow.sh plan_services go postgresql

   This creates /tmp/service_plan.json with:
   • Service hostnames (appdev, appstage, db)
   • Runtime versions based on recipes
   • Setup configurations (dev vs prod)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 3: Get/Create import.yml (REQUIRED)
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

   1. Read /tmp/fetched_recipe.md
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

   Reference: /tmp/fetched_docs.md for version strings
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

output_phase_guidance() {
    local phase="$1"

    case "$phase" in
        COMPOSE)
            output_compose_guidance
            ;;
        EXTEND)
            output_extend_guidance
            ;;
        SYNTHESIZE)
            output_synthesize_guidance
            ;;
        DISCOVER)
            output_discover_guidance
            ;;
        DEVELOP)
            cat <<'EOF'
✅ Phase: DEVELOP

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📁 Files: /var/www/{dev}/     (edit directly via SSHFS)
💻 Run:   ssh {dev} "cmd"     (execute inside container)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  CRITICAL: Dev is where you iterate and fix errors.
    Stage is for final validation AFTER dev confirms success.

    You MUST verify the feature works correctly on dev before deploying.
    If you find errors on stage, you did not test properly on dev.

┌─────────────────────────────────────────────────────────────────┐
│  DEVELOP LOOP (repeat until feature works):                     │
│                                                                 │
│  1. Build & Run                                                 │
│  2. Test functionality (not just HTTP status!)                  │
│  3. Check for errors (logs, responses, browser console)         │
│  4. If errors → Fix → Go to step 1                              │
│  5. Only when working → run verify.sh → transition to DEPLOY    │
└─────────────────────────────────────────────────────────────────┘

Kill existing process:
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'

Build & run:
  ssh {dev} "{build_command}"
  ssh {dev} './{binary} >> /tmp/app.log 2>&1'
  ↑ Set run_in_background=true in Bash tool parameters

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔍 FUNCTIONAL TESTING (required before deploy):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

HTTP 200 is NOT enough. You must verify the feature WORKS.

Backend APIs:
  # GET the actual response and check content
  ssh {dev} "curl -s http://localhost:{port}/api/endpoint" | jq .

  # POST and verify the operation succeeded
  ssh {dev} "curl -s -X POST http://localhost:{port}/api/items -d '{...}'"

  # Check the data persisted
  ssh {dev} "curl -s http://localhost:{port}/api/items"

Frontend/Full-stack:
  URL=$(ssh {dev} "echo \$zeropsSubdomain")
  agent-browser open "$URL"
  agent-browser errors          # ← MUST be empty
  agent-browser console         # ← Check for runtime errors
  agent-browser screenshot      # ← Visual verification

Logs (check for errors/exceptions):
  ssh {dev} "tail -50 /tmp/app.log"
  ssh {dev} "grep -i error /tmp/app.log"
  ssh {dev} "grep -i exception /tmp/app.log"

Database verification (if applicable):
  psql "$db_connectionString" -c "SELECT * FROM {table} ORDER BY id DESC LIMIT 5;"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
❌ DO NOT deploy to stage if:
   • Response contains error messages
   • Logs show exceptions or stack traces
   • Browser console has JavaScript errors
   • Data isn't persisting correctly
   • UI is broken or not rendering

✅ Deploy to stage ONLY when:
   • Feature works as expected on dev
   • No errors in logs or console
   • You've tested the actual functionality, not just "server responds"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 When ready (feature works, no errors):
   .zcp/verify.sh {dev} {port} / /status /api/...
   .zcp/workflow.sh transition_to DEPLOY
EOF
            ;;
        DEPLOY)
            cat <<'EOF'
✅ Phase: DEPLOY

⚠️  PRE-DEPLOYMENT CHECKLIST (do this BEFORE deploying):
   1. cat /var/www/{dev}/zerops.yaml | grep -A10 deployFiles
   2. Verify ALL artifacts exist:
      ls -la /var/www/{dev}/app
      ls -la /var/www/{dev}/templates/  # if using templates
      ls -la /var/www/{dev}/static/     # if using static files
   3. If you created templates/ or static/, add them to deployFiles!

⚠️  Common failure: Agent builds files but doesn't update deployFiles

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Stop dev process:
  ssh {dev} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'

Authenticate from dev container:
  ssh {dev} "zcli login --region=gomibako \\
      --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \\
      \"\$ZEROPS_ZCP_API_KEY\""

Deploy to stage:
  ssh {dev} "zcli push {stage_service_id} --setup={setup} --versionName=v1.0.0"

  --setup={setup} → references zerops.yaml build config name
  --versionName   → optional but recommended

**zerops.yaml structure reference:**
zerops:
  - setup: api                    # ← --setup value
    build:
      base: go@1.22
      buildCommands:
        - go build -o app main.go
      deployFiles:
        - ./app
        - ./templates             # Don't forget if you created these!
        - ./static
    run:
      base: go@1.22
      ports:
        - port: 8080
      start: ./app

Wait for completion:
  .zcp/status.sh --wait {stage}

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: .zcp/status.sh shows SUCCESS notification
📋 Next: .zcp/workflow.sh transition_to VERIFY
EOF
            ;;
        VERIFY)
            cat <<'EOF'
✅ Phase: VERIFY

Check deployed artifacts:
  ssh {stage} "ls -la /var/www/"

Verify endpoints:
  .zcp/verify.sh {stage} {port} / /status /api/...

Service logs:
  zcli service log -S {stage_service_id} -P $projectId --follow

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠️  BROWSER TESTING (required if frontend exists):
   If your app has HTML/CSS/JS/templates:

   URL=$(ssh {stage} "echo \$zeropsSubdomain")
   agent-browser open "$URL"          # Don't prepend https://!
   agent-browser errors               # Must show no errors
   agent-browser console              # Check runtime errors
   agent-browser network requests     # Verify assets load
   agent-browser screenshot           # Visual evidence

⚠️  HTTP 200 ≠ working UI. CSS/JS errors return 200 but break the app.

💡 Tool awareness: You CAN see screenshots and reason about them.
   You CAN use curl to test functionality, not just status codes.
   You CAN query the database to verify data persistence.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: verify.sh must pass (creates /tmp/stage_verify.json)
📋 Next: .zcp/workflow.sh transition_to DONE
EOF
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
