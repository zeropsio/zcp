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
        DISCOVER)
            if [ "$current_phase" != "INIT" ]; then
                echo "❌ Cannot transition to DISCOVER from $current_phase"
                echo "📋 Run: .zcp/workflow.sh init"
                return 2
            fi
            ;;
        DEVELOP)
            if [ "$current_phase" != "DISCOVER" ]; then
                echo "❌ Cannot transition to DEVELOP from $current_phase"
                echo "📋 Required flow: INIT → DISCOVER → DEVELOP"
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
    output_phase_guidance "$target_phase"
}

output_phase_guidance() {
    local phase="$1"

    case "$phase" in
        DISCOVER)
            cat <<'EOF'
✅ Phase: DISCOVER

📋 Commands:
   zcli login --region=gomibako \
       --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
       "$ZEROPS_ZAGENT_API_KEY"

   zcli service list -P $projectId

📋 Then record discovery:
   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

⚠️  Never use 'zcli scope' - it's buggy
⚠️  Use service IDs (from list), not hostnames

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Gate: /tmp/discovery.json must exist
📋 Next: .zcp/workflow.sh transition_to DEVELOP
EOF
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
      \"\$ZEROPS_ZAGENT_API_KEY\""

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
