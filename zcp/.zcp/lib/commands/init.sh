#!/bin/bash
# Init commands for Zerops Workflow

cmd_init() {
    local mode_flag="$1"
    local existing_session
    existing_session=$(get_session)

    # Idempotent init - don't create duplicate sessions
    if [ -n "$existing_session" ]; then
        local current_phase
        current_phase=$(get_phase)

        # Special handling for DONE phase - suggest iterate instead
        if [ "$current_phase" = "DONE" ]; then
            echo "╔══════════════════════════════════════════════════════════════════╗"
            echo "║  SESSION ACTIVE - WORKFLOW COMPLETE                              ║"
            echo "╚══════════════════════════════════════════════════════════════════╝"
            echo ""
            echo "Session: $existing_session"
            echo "Phase:   DONE"
            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "💡 OPTIONS"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "  iterate [summary]        Start new iteration (recommended)"
            echo "                           Preserves discovery, archives evidence"
            echo ""
            echo "  iterate --to VERIFY      Skip to verify (no code changes needed)"
            echo ""
            echo "  reset --keep-discovery   Full reset, preserve service mapping"
            echo ""
            echo "  show                     View current status"
            echo ""
            echo "Example: .zcp/workflow.sh iterate \"Add delete confirmation\""
            return 0
        fi

        echo "✅ Session already active: $existing_session"
        echo ""
        echo "╔══════════════════════════════════════════════════════════════════╗"
        echo "║  ⛔ STOP - READ THE RULES BELOW BEFORE DOING ANYTHING            ║"
        echo "╚══════════════════════════════════════════════════════════════════╝"
        echo ""
        cmd_show
        return 0
    fi

    # Handle --dev-only mode
    if [ "$mode_flag" = "--dev-only" ]; then
        local session_id
        session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
        echo "$session_id" > "$SESSION_FILE"
        echo "dev-only" > "$MODE_FILE"
        echo "INIT" > "$PHASE_FILE"

        cat <<'EOF'
✅ DEV-ONLY MODE

📋 Flow: INIT → DISCOVER → DEVELOP → DONE
   (No deployment, no stage verification)

💡 Use this for:
   - Rapid prototyping
   - Dev iteration without deployment
   - Testing before committing to deploy

⚠️  To upgrade to full deployment later:
   .zcp/workflow.sh upgrade-to-full
EOF
        return 0
    fi

    # Handle --hotfix mode
    if [ "$mode_flag" = "--hotfix" ]; then
        # Check for recent discovery
        if [ -f "$DISCOVERY_FILE" ]; then
            local timestamp age_hours
            timestamp=$(jq -r '.timestamp // empty' "$DISCOVERY_FILE" 2>/dev/null)
            if [ -n "$timestamp" ]; then
                local disco_epoch now_epoch
                # Try GNU date first, then BSD date
                if disco_epoch=$(date -d "$timestamp" +%s 2>/dev/null); then
                    : # GNU date worked
                elif disco_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$timestamp" +%s 2>/dev/null); then
                    : # BSD date worked
                fi

                if [ -n "$disco_epoch" ]; then
                    now_epoch=$(date +%s)
                    age_hours=$(( (now_epoch - disco_epoch) / 3600 ))

                    local max_age="${HOTFIX_MAX_AGE_HOURS:-24}"
                    if [ "$age_hours" -lt "$max_age" ]; then
                        local session_id
                        session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
                        echo "$session_id" > "$SESSION_FILE"
                        echo "hotfix" > "$MODE_FILE"
                        echo "DEVELOP" > "$PHASE_FILE"

                        # Update session in discovery
                        if jq --arg sid "$session_id" '.session_id = $sid' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp"; then
                            mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"
                        else
                            rm -f "${DISCOVERY_FILE}.tmp"
                            echo "Failed to update discovery.json" >&2
                            return 1
                        fi

                        cat <<EOF
🚨 HOTFIX MODE

✓ Reusing discovery from ${age_hours}h ago
  Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")
  Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")

📋 Flow: DEVELOP → DEPLOY → VERIFY → DONE
   (Skipping discovery and dev verification)

⚠️  REDUCED SAFETY:
   - No dev verification (you may deploy untested code)
   - Stage verification still REQUIRED

Ready. Start implementing your hotfix.
EOF
                        return 0
                    fi
                fi
            fi
        fi

        echo "❌ Cannot use hotfix mode"
        echo "   No recent discovery (< ${HOTFIX_MAX_AGE_HOURS:-24}h) found"
        echo "   Run: .zcp/workflow.sh init (normal mode)"
        return 1
    fi

    # Create new session
    local session_id
    session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
    echo "$session_id" > "$SESSION_FILE"
    echo "full" > "$MODE_FILE"
    echo "INIT" > "$PHASE_FILE"

    # Check for preserved discovery and update session_id
    if [ -f "$DISCOVERY_FILE" ]; then
        local old_session
        old_session=$(jq -r '.session_id // empty' "$DISCOVERY_FILE" 2>/dev/null)
        if [ -n "$old_session" ]; then
            # Update session_id in existing discovery
            if jq --arg sid "$session_id" '.session_id = $sid' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp"; then
                mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"
            else
                rm -f "${DISCOVERY_FILE}.tmp"
                echo "Failed to update discovery.json" >&2
                return 1
            fi

            echo "✅ Session: $session_id"
            echo ""
            echo "📋 Preserved discovery detected:"
            echo "   Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")"
            echo "   Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")"
            echo ""
            echo "💡 NEXT: Skip DISCOVER, go directly to DEVELOP"
            echo "   .zcp/workflow.sh transition_to DISCOVER"
            echo "   .zcp/workflow.sh transition_to DEVELOP"
            return 0
        fi
    fi

    # Normal init (no preserved discovery)
    cat <<EOF
✅ Session: $session_id

📋 Workflow: INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE

💡 NEXT: DISCOVER phase
   1. zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "\$ZEROPS_ZCP_API_KEY"
   2. zcli service list -P \$projectId
   3. .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
   4. .zcp/workflow.sh transition_to DISCOVER

⚠️  Cannot skip DISCOVER - creates required evidence

📖 Full reference: .zcp/workflow.sh --help
EOF
}

cmd_quick() {
    local existing_session
    existing_session=$(get_session)

    # Don't override existing enforced sessions
    if [ -n "$existing_session" ]; then
        local current_mode
        current_mode=$(get_mode)
        if [ "$current_mode" != "quick" ]; then
            echo "⚠️  Active session exists: $existing_session (mode: $current_mode)"
            echo ""
            echo "To switch to quick mode, first reset the current session:"
            echo "   .zcp/workflow.sh reset"
            echo "   .zcp/workflow.sh --quick"
            echo ""
            echo "Or view current status:"
            echo "   .zcp/workflow.sh show"
            return 1
        fi
        # Already in quick mode, just show status
        echo "✅ Already in quick mode (session: $existing_session)"
        echo ""
        cmd_show
        return 0
    fi

    local session_id
    session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
    echo "$session_id" > "$SESSION_FILE"
    echo "quick" > "$MODE_FILE"
    echo "QUICK" > "$PHASE_FILE"

    cat <<'EOF'
✅ Quick mode - no enforcement

💡 Available tools:
   status.sh                    # Check deployment state
   .zcp/status.sh --wait {svc}       # Wait for deploy
   .zcp/verify.sh {svc} {port} /...  # Test endpoints
   .zcp/workflow.sh --help           # Full reference

⚠️  Remember:
   Files: /var/www/{service}/   (SSHFS direct edit)
   Commands: ssh {service} "cmd"
EOF
}
