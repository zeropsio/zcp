#!/bin/bash
# Gate checks for Zerops Workflow phase transitions

check_gate_discover_to_develop() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: DISCOVER → DEVELOP"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: discovery.json exists
    ((checks_total++))
    if [ -f "$DISCOVERY_FILE" ]; then
        echo "  ✓ discovery.json exists"
        ((checks_passed++))
    else
        echo "  ✗ discovery.json missing"
        echo "    → Run: .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}"
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$DISCOVERY_FILE"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        local current_session=$(get_session)
        local disco_session=$(jq -r '.session_id // "none"' "$DISCOVERY_FILE" 2>/dev/null)
        echo "  ✗ session_id mismatch"
        echo "    → Current session: $current_session"
        echo "    → Discovery session: $disco_session"
        echo "    → Run create_discovery again or .zcp/workflow.sh reset"
        all_passed=false
    fi

    # Check 3: dev != stage (unless single_mode)
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$DISCOVERY_FILE" ]; then
        local dev_name stage_name single_mode
        dev_name=$(jq -r '.dev.name' "$DISCOVERY_FILE" 2>/dev/null)
        stage_name=$(jq -r '.stage.name' "$DISCOVERY_FILE" 2>/dev/null)
        single_mode=$(jq -r '.single_mode // false' "$DISCOVERY_FILE" 2>/dev/null)

        if [ "$dev_name" != "$stage_name" ]; then
            echo "  ✓ dev ≠ stage ($dev_name vs $stage_name)"
            ((checks_passed++))
        elif [ "$single_mode" = "true" ]; then
            echo "  ⚠ single-service mode (dev = stage = $dev_name)"
            echo "    → Intentional: source corruption risk acknowledged"
            ((checks_passed++))
        else
            echo "  ✗ dev.name == stage.name ('$dev_name')"
            echo "    → Cannot use same service for dev and stage"
            echo "    → Source corruption risk: zcli push overwrites /var/www/"
            echo "    → Use --single flag if you understand the risk"
            all_passed=false
        fi
    else
        echo "  ⚠ Cannot verify dev≠stage (jq unavailable or no discovery)"
        ((checks_passed++))
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        check_evidence_freshness "$DISCOVERY_FILE" 24
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}

check_gate_develop_to_deploy() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: DEVELOP → DEPLOY"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: dev_verify.json exists
    ((checks_total++))
    if [ -f "$DEV_VERIFY_FILE" ]; then
        echo "  ✓ dev_verify.json exists"
        ((checks_passed++))
    else
        echo "  ✗ dev_verify.json missing"
        echo "    → Run: .zcp/verify.sh {dev} {port} / /status /api/..."
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$DEV_VERIFY_FILE"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        echo "  ✗ session_id mismatch"
        echo "    → Evidence is from a different session"
        echo "    → Re-run verify.sh for current session"
        all_passed=false
    fi

    # Check 3: failures == 0
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$DEV_VERIFY_FILE" ]; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        # Validate numeric before comparison
        if ! [[ "$failures" =~ ^[0-9]+$ ]]; then
            echo "  ✗ Cannot read failure count from evidence file"
            all_passed=false
        elif [ "$failures" -eq 0 ]; then
            local passed
            passed=$(jq -r '.passed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
            echo "  ✓ verification passed ($passed endpoints, 0 failures)"
            ((checks_passed++))
        else
            echo "  ✗ verification has $failures failure(s)"
            echo "    → Fix failing endpoints before deploying"
            echo "    → Check: jq '.results[] | select(.pass==false)' /tmp/dev_verify.json"
            all_passed=false
        fi
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        check_evidence_freshness "$DEV_VERIFY_FILE" 24
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}

check_gate_deploy_to_verify() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: DEPLOY → VERIFY"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: deploy_evidence.json exists
    ((checks_total++))
    if [ -f "$DEPLOY_EVIDENCE_FILE" ]; then
        echo "  ✓ deploy_evidence.json exists"
        ((checks_passed++))
    else
        echo "  ✗ deploy_evidence.json missing"
        echo "    → Run: .zcp/status.sh --wait {stage}"
        echo "    → Or:  .zcp/workflow.sh record_deployment {stage}"
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$DEPLOY_EVIDENCE_FILE"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        echo "  ✗ session_id mismatch"
        echo "    → Deployment evidence is from a different session"
        echo "    → Re-deploy and wait: .zcp/status.sh --wait {stage}"
        all_passed=false
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        check_evidence_freshness "$DEPLOY_EVIDENCE_FILE" 24
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}

check_gate_verify_to_done() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: VERIFY → DONE"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: stage_verify.json exists
    ((checks_total++))
    if [ -f "$STAGE_VERIFY_FILE" ]; then
        echo "  ✓ stage_verify.json exists"
        ((checks_passed++))
    else
        echo "  ✗ stage_verify.json missing"
        echo "    → Run: .zcp/verify.sh {stage} {port} / /status /api/..."
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        echo "  ✗ session_id mismatch"
        echo "    → Evidence is from a different session"
        echo "    → Re-run verify.sh for current session"
        all_passed=false
    fi

    # Check 3: failures == 0
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$STAGE_VERIFY_FILE" ]; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        # Validate numeric before comparison
        if ! [[ "$failures" =~ ^[0-9]+$ ]]; then
            echo "  ✗ Cannot read failure count from evidence file"
            all_passed=false
        elif [ "$failures" -eq 0 ]; then
            local passed
            passed=$(jq -r '.passed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
            echo "  ✓ verification passed ($passed endpoints, 0 failures)"
            ((checks_passed++))
        else
            echo "  ✗ verification has $failures failure(s)"
            echo "    → Fix failing endpoints"
            echo "    → Use: .zcp/workflow.sh transition_to --back DEVELOP"
            all_passed=false
        fi
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        check_evidence_freshness "$STAGE_VERIFY_FILE" 24
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}
