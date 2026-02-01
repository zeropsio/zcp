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
        session_id=$(generate_secure_session_id)
        # Initialize unified state
        zcp_init "$session_id"
        set_mode "dev-only"
        set_phase "INIT"

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
                        session_id=$(generate_secure_session_id)
                        # Initialize unified state
                        zcp_init "$session_id"
                        set_mode "hotfix"
                        set_phase "DEVELOP"

                        # Update session in discovery
                        if jq --arg sid "$session_id" '.session_id = $sid' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp.$$"; then
                            mv "${DISCOVERY_FILE}.tmp.$$" "$DISCOVERY_FILE"
                        else
                            rm -f "${DISCOVERY_FILE}.tmp.$$"
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
    session_id=$(generate_secure_session_id)
    # Initialize unified state
    zcp_init "$session_id"
    set_mode "full"
    set_phase "INIT"

    # Check for preserved discovery and update session_id
    if [ -f "$DISCOVERY_FILE" ]; then
        local old_session
        old_session=$(jq -r '.session_id // empty' "$DISCOVERY_FILE" 2>/dev/null)
        if [ -n "$old_session" ]; then
            # Update session_id in existing discovery
            if jq --arg sid "$session_id" '.session_id = $sid' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp.$$"; then
                mv "${DISCOVERY_FILE}.tmp.$$" "$DISCOVERY_FILE"
            else
                rm -f "${DISCOVERY_FILE}.tmp.$$"
                echo "Failed to update discovery.json" >&2
                return 1
            fi

            echo "✅ Session: $session_id"
            echo ""
            echo "📋 Preserved discovery detected:"
            echo "   Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")"
            echo "   Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")"
            echo ""
            echo "💡 EXISTING PROJECT - Gate 0 will be skipped"
            echo ""
            echo "   Run these commands:"
            echo "   .zcp/workflow.sh transition_to DISCOVER   ← Gate 0 skipped (discovery exists)"
            echo "   .zcp/workflow.sh transition_to DEVELOP    ← Continue to development"
            echo ""
            echo "   Or use iterate if you just completed a workflow:"
            echo "   .zcp/workflow.sh iterate \"description\""
            return 0
        fi
    fi

    # Normal init (no preserved discovery)
    # Don't try to detect bootstrap mode here - zcli isn't logged in yet
    # Let DISCOVER handle it (after login, it can check for services)

    echo "✅ Session: $session_id"
    echo ""
    echo "📋 Workflow: INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE"
    echo ""
    cat <<'EOF'
💡 NEXT: Run transition_to DISCOVER

   ┌─────────────────────────────────────────────────────────────────┐
   │ .zcp/workflow.sh transition_to DISCOVER                         │
   │                                                                  │
   │ This will:                                                       │
   │   • Guide you through zcli login                                 │
   │   • Check if services exist (bootstrap vs standard flow)         │
   │   • Show exact next steps based on your project state            │
   └─────────────────────────────────────────────────────────────────┘

⚠️  FOLLOW THE OUTPUT - each transition tells you what to do next.
    Don't make your own plan. Let the workflow guide you.

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
    session_id=$(generate_secure_session_id)
    # Initialize unified state
    zcp_init "$session_id"
    set_mode "quick"
    set_phase "QUICK"

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
