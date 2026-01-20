#!/bin/bash
# Status and show commands for Zerops Workflow

cmd_show() {
    local show_guidance=false
    local show_full=false

    # Parse flags
    while [ $# -gt 0 ]; do
        case "$1" in
            --guidance) show_guidance=true; shift ;;
            --full) show_full=true; shift ;;
            *) shift ;;
        esac
    done

    local session_id
    local mode
    local phase
    local iteration

    session_id=$(get_session)
    mode=$(get_mode)
    phase=$(get_phase)
    iteration=$(get_iteration 2>/dev/null || echo "1")

    cat <<EOF
╔══════════════════════════════════════════════════════════════════╗
║  WORKFLOW STATUS                                                 ║
╚══════════════════════════════════════════════════════════════════╝

Session:    ${session_id:-none}
Mode:       ${mode:-none}
Phase:      ${phase:-none}
Iteration:  ${iteration}

Evidence:
EOF

    # Check discovery
    if check_evidence_session "$DISCOVERY_FILE"; then
        echo "  ✓ /tmp/discovery.json (current session)"
    elif [ -f "$DISCOVERY_FILE" ]; then
        echo "  ✗ /tmp/discovery.json (stale session)"
    else
        echo "  ✗ /tmp/discovery.json (missing)"
    fi

    # Check dev verify
    if check_evidence_session "$DEV_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        echo "  ✓ /tmp/dev_verify.json (current session, $failures failures)"
    elif [ -f "$DEV_VERIFY_FILE" ]; then
        echo "  ✗ /tmp/dev_verify.json (stale session)"
    else
        echo "  ✗ /tmp/dev_verify.json (missing)"
    fi

    # Check stage verify
    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        echo "  ✓ /tmp/stage_verify.json (current session, $failures failures)"
    elif [ -f "$STAGE_VERIFY_FILE" ]; then
        echo "  ✗ /tmp/stage_verify.json (stale session)"
    else
        echo "  ✗ /tmp/stage_verify.json (missing)"
        # Indicate if evidence was invalidated by backward transition
        if [ "$(get_phase)" = "DEVELOP" ] && [ -f "$DEV_VERIFY_FILE" ]; then
            echo "    ⚠️  Stage evidence may have been invalidated (backward transition)"
        fi
    fi

    # Check deploy evidence
    if [ -f "$DEPLOY_EVIDENCE_FILE" ] 2>/dev/null; then
        if check_evidence_session "$DEPLOY_EVIDENCE_FILE"; then
            echo "  ✓ /tmp/deploy_evidence.json (current session)"
        else
            echo "  ✗ /tmp/deploy_evidence.json (stale session)"
        fi
    fi

    # Show discovered services if discovery exists
    if [ -f "$DISCOVERY_FILE" ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "📦 DISCOVERED SERVICES"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        local dev_name dev_id stage_name stage_id
        dev_name=$(jq -r '.dev.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
        dev_id=$(jq -r '.dev.id // "?"' "$DISCOVERY_FILE" 2>/dev/null)
        stage_name=$(jq -r '.stage.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
        stage_id=$(jq -r '.stage.id // "?"' "$DISCOVERY_FILE" 2>/dev/null)
        echo ""
        echo "  Runtime (SSH ✓):  $dev_name (dev), $stage_name (stage)"
        echo "  Managed (NO SSH): db, cache, etc. → use psql, redis-cli from ZCP"
        echo ""
        echo "  DB access:  psql \"\$db_connectionString\""
        echo "  Variables:  \$db_hostname, \$db_user, \$db_database, \$db_connectionString"
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "💡 NEXT STEPS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Give specific guidance based on phase and what's missing
    case "$phase" in
        INIT)
            if ! check_evidence_session "$DISCOVERY_FILE"; then
                cat <<'GUIDANCE'
1. Discover services:
   zcli login --region=gomibako \
       --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
       "$ZEROPS_ZCP_API_KEY"
   zcli service list -P $projectId   ← -P flag required!

2. Record discovery (use IDs from step 1):
   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}

3. Transition:
   .zcp/workflow.sh transition_to DISCOVER
GUIDANCE
            else
                echo "Discovery exists. Run: .zcp/workflow.sh transition_to DISCOVER"
            fi
            ;;
        DISCOVER)
            if check_evidence_session "$DISCOVERY_FILE"; then
                echo "Discovery complete. Run: .zcp/workflow.sh transition_to DEVELOP"
            else
                cat <<'GUIDANCE'
Discovery missing or stale. Re-run:
   zcli service list -P $projectId   ← -P flag required!
   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
GUIDANCE
            fi
            ;;
        DEVELOP)
            if ! check_evidence_session "$DEV_VERIFY_FILE"; then
                local dev_name
                dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
                cat <<GUIDANCE
⚠️  DEVELOP PHASE REMINDERS:
   • Kill orphans: ssh $dev_name 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'
   • Long commands: run_in_background=true
   • HTTP 200 ≠ working — check content, logs, console

1. Build and test on dev ($dev_name):
   ssh $dev_name "{build_cmd} && ./{binary}"
2. Verify endpoints:
   .zcp/verify.sh $dev_name {port} / /api/...
3. Then: .zcp/workflow.sh transition_to DEPLOY
GUIDANCE
            else
                echo "Dev verified. Run: .zcp/workflow.sh transition_to DEPLOY"
            fi
            ;;
        DEPLOY)
            if ! check_evidence_session "$DEPLOY_EVIDENCE_FILE" 2>/dev/null; then
                local dev_name stage_id stage_name
                dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
                stage_id=$(jq -r '.stage.id // "STAGE_ID"' "$DISCOVERY_FILE" 2>/dev/null)
                stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)
                cat <<GUIDANCE
⚠️  DEPLOY PHASE REMINDERS:
   • Deploy from dev container, NOT ZCP
   • deployFiles must include ALL artifacts
   • Check: cat /var/www/$dev_name/zerops.yaml | grep -A10 deployFiles

1. Check deployFiles in zerops.yaml includes all artifacts
2. Deploy:
   ssh $dev_name "zcli login ... && zcli push $stage_id --setup={setup}"
3. Wait:
   .zcp/status.sh --wait $stage_name
4. Then: .zcp/workflow.sh transition_to VERIFY
GUIDANCE
            else
                echo "Deploy complete. Run: .zcp/workflow.sh transition_to VERIFY"
            fi
            ;;
        VERIFY)
            if ! check_evidence_session "$STAGE_VERIFY_FILE"; then
                local stage_name
                stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)
                cat <<GUIDANCE
⚠️  VERIFY PHASE REMINDERS:
   • HTTP 200 ≠ working — check content, logs, console
   • zeropsSubdomain is full URL — don't prepend https://
   • Stage ($stage_name) is runtime — SSH works

1. Verify stage endpoints:
   .zcp/verify.sh $stage_name {port} / /api/...
2. Browser check (if frontend):
   URL=\$(ssh $stage_name "echo \\\$zeropsSubdomain")
   agent-browser open "\$URL"
   agent-browser errors   # Must be empty
3. Then: .zcp/workflow.sh transition_to DONE
GUIDANCE
            else
                echo "Stage verified. Run: .zcp/workflow.sh transition_to DONE"
            fi
            ;;
        DONE)
            cat <<'GUIDANCE'
✅ Workflow complete.

To continue working (bug fix, new feature, iteration):
   .zcp/workflow.sh iterate "description"     Start new iteration
   .zcp/workflow.sh iterate --to VERIFY       Skip to verify (no code changes)

To finish:
   .zcp/workflow.sh complete                  Mark session complete
GUIDANCE
            ;;
        QUICK)
            cat <<'GUIDANCE'
Quick mode - no workflow enforcement

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  CRITICAL RULES (you are on ZCP, not inside containers)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Runtime services: ssh {service} "command"
• Managed services (db, cache, etc.): NO SSH!
  Use client tools directly from ZCP:
  psql "$db_connectionString"
  redis-cli -u "$cache_connectionString"
• Variables: ${hostname}_VAR from ZCP, $VAR inside ssh
• zcli from ZCP: login first, then -P $projectId
  zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"
• Files: /var/www/{service}/ via SSHFS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
GUIDANCE
            ;;
        *)
            echo "Unknown phase. Run: .zcp/workflow.sh init"
            ;;
    esac

    # Show last error if any (automatically captured from verify/deploy failures)
    local last_error
    last_error=$(get_last_error 2>/dev/null)
    if [ -n "$last_error" ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "⚠️  LAST ERROR"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        echo "  $last_error"
    fi

    # If --full flag, show extended context
    if [ "$show_full" = true ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "📜 EXTENDED CONTEXT"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""

        # Show intent if set
        local intent
        intent=$(get_intent 2>/dev/null)
        if [ -n "$intent" ]; then
            echo "Intent: \"$intent\""
        else
            echo "Intent: (not set)"
        fi

        # Show iteration history summary
        local history_file="$WORKFLOW_STATE_DIR/iteration_history.json"
        if [ -f "$history_file" ]; then
            local iter_count
            iter_count=$(jq -r '.iterations | length' "$history_file" 2>/dev/null || echo "0")
            echo "Iteration history: $iter_count iteration(s) recorded"

            # Show last iteration summary
            local last_summary
            last_summary=$(jq -r '.iterations[-1].summary // ""' "$history_file" 2>/dev/null)
            if [ -n "$last_summary" ]; then
                echo "Current iteration: \"$last_summary\""
            fi
        fi

        # Show recent notes if any
        local notes_count
        notes_count=$(jq -r '.notes | length' "$CONTEXT_FILE" 2>/dev/null || echo "0")
        if [ "$notes_count" -gt 0 ]; then
            echo ""
            echo "Recent notes:"
            jq -r '.notes[-3:][] | "  [\(.at | split("T")[1] | split(".")[0])] \(.text)"' "$CONTEXT_FILE" 2>/dev/null
        fi

        # Show context file timestamp
        if [ -f "$CONTEXT_FILE" ]; then
            local ctx_ts
            ctx_ts=$(jq -r '.last_error.timestamp // empty' "$CONTEXT_FILE" 2>/dev/null)
            if [ -n "$ctx_ts" ]; then
                echo ""
                echo "Last context update: $ctx_ts"
            fi
        fi
    fi

    # If --guidance flag, output full phase guidance
    if [ "$show_guidance" = true ] && [ -n "$phase" ] && [ "$phase" != "NONE" ] && [ "$phase" != "QUICK" ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "📖 PHASE GUIDANCE"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        output_phase_guidance "$phase"
    fi
}

cmd_recover() {
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║  FULL CONTEXT RECOVERY                                           ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""

    # Run show with guidance
    cmd_show --guidance

    # Output critical rules reminder
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "⚠️  CRITICAL RULES (always remember)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    cat <<'RULES'
• Kill orphans:     ssh {svc} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'
• Long commands:    run_in_background=true in Bash tool
• HTTP 200:         Does NOT mean working — check content, logs, console
• Deploy from:      Dev container (ssh {dev} "zcli push..."), NOT from ZCP
• deployFiles:      Must include ALL artifacts — check before every deploy
• zeropsSubdomain:  Already full URL — don't prepend https://
RULES
}

cmd_state() {
    local session_id mode phase
    session_id=$(get_session)
    mode=$(get_mode)
    phase=$(get_phase)

    local dev_name stage_name dev_verify stage_verify
    dev_name="?"
    stage_name="?"
    dev_verify="missing"
    stage_verify="missing"

    if [ -f "$DISCOVERY_FILE" ]; then
        dev_name=$(jq -r '.dev.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
        stage_name=$(jq -r '.stage.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
    fi

    if check_evidence_session "$DEV_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        dev_verify="${failures}_failures"
    fi

    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        stage_verify="${failures}_failures"
    fi

    echo "${phase:-NONE} | ${mode:-none} | dev=${dev_name} stage=${stage_name} | dev_verify=${dev_verify} stage_verify=${stage_verify}"
}

cmd_complete() {
    local session_id
    session_id=$(get_session)

    if [ -z "$session_id" ]; then
        echo "❌ No active session"
        return 1
    fi

    local all_valid=true
    local messages=()

    # Check all evidence
    if check_evidence_session "$DISCOVERY_FILE"; then
        messages+=("   • Discovery: /tmp/discovery.json ✓")
    else
        messages+=("   ✗ Discovery: /tmp/discovery.json MISSING or stale")
        all_valid=false
    fi

    if check_evidence_session "$DEV_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        if [ "$failures" -eq 0 ]; then
            messages+=("   • Dev verify: /tmp/dev_verify.json (0 failures) ✓")
        else
            messages+=("   ✗ Dev verify: /tmp/dev_verify.json ($failures failures)")
            all_valid=false
        fi
    else
        messages+=("   ✗ Dev verify: /tmp/dev_verify.json MISSING or stale")
        all_valid=false
    fi

    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        if [ "$failures" -eq 0 ]; then
            messages+=("   • Stage verify: /tmp/stage_verify.json (0 failures) ✓")
        else
            messages+=("   ✗ Stage verify: /tmp/stage_verify.json ($failures failures)")
            all_valid=false
        fi
    else
        messages+=("   ✗ Stage verify: /tmp/stage_verify.json MISSING or stale")
        all_valid=false
    fi

    if [ "$all_valid" = true ]; then
        echo "✅ Evidence validated:"
        echo "   • Session: $session_id"
        printf '%s\n' "${messages[@]}"
        echo ""
        echo "<completed>WORKFLOW_DONE</completed>"
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "📋 Next task? Run workflow.sh again to decide:"
        echo "   .zcp/workflow.sh init    → deploying"
        echo "   .zcp/workflow.sh --quick → exploring"
        return 0
    else
        echo "❌ Evidence validation failed:"
        echo ""
        echo "   • Session: $session_id"
        printf '%s\n' "${messages[@]}"
        echo ""
        echo "💡 Fix the issues above and run: .zcp/workflow.sh complete"
        return 3
    fi
}

cmd_reset() {
    local keep_discovery=false
    if [ "$1" = "--keep-discovery" ]; then
        keep_discovery=true
    fi

    # Always clear session state and verification evidence
    rm -f "$SESSION_FILE" "$MODE_FILE" "$PHASE_FILE"
    rm -f "$DEV_VERIFY_FILE" "$STAGE_VERIFY_FILE" "$DEPLOY_EVIDENCE_FILE"

    if [ "$keep_discovery" = true ]; then
        if [ -f "$DISCOVERY_FILE" ]; then
            echo "✓ Discovery preserved"
            echo "  Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")"
            echo "  Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")"
            echo ""
            echo "💡 Next: .zcp/workflow.sh init"
            echo "   Discovery will be reused with new session"
        else
            echo "⚠️  No discovery to preserve"
            rm -f "$DISCOVERY_FILE"
            echo ""
            echo "💡 Start fresh: .zcp/workflow.sh init"
        fi
    else
        rm -f "$DISCOVERY_FILE"
        echo "✅ All workflow state cleared"
        echo ""
        echo "💡 Start fresh: .zcp/workflow.sh init"
    fi
}
