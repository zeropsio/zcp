#!/bin/bash
# Gate checks for Zerops Workflow phase transitions

# ============================================================================
# Gate 0: INIT → DISCOVER (Recipe Review)
# ============================================================================

check_gate_init_to_discover() {
    local checks_passed=0
    local checks_total=0
    local all_passed=true
    local mode
    mode=$(get_mode)

    echo "Gate: INIT → DISCOVER"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # In hotfix mode, warn but don't block
    if [ "$mode" = "hotfix" ]; then
        if [ ! -f "$RECIPE_REVIEW_FILE" ]; then
            echo "  ⚠️  HOTFIX MODE: Recipe review skipped"
            echo "    → Consider running: .zcp/recipe-search.sh quick {runtime}"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            return 0
        fi
    fi

    # In quick mode, skip gate
    if [ "$mode" = "quick" ]; then
        echo "  ⚠️  QUICK MODE: Gate skipped"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        return 0
    fi

    # Check 1: recipe_review.json exists
    ((checks_total++))
    if [ -f "$RECIPE_REVIEW_FILE" ]; then
        echo "  ✓ recipe_review.json exists"
        ((checks_passed++))
    else
        echo "  ✗ recipe_review.json missing"
        echo "    → Run: .zcp/recipe-search.sh quick {runtime} [managed-service]"
        echo "    → Example: .zcp/recipe-search.sh quick go postgresql"
        all_passed=false
    fi

    # Check 2: verified flag is true
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$RECIPE_REVIEW_FILE" ]; then
        local verified
        verified=$(jq -r '.verified // false' "$RECIPE_REVIEW_FILE" 2>/dev/null)
        if [ "$verified" = "true" ]; then
            echo "  ✓ recipe review verified"
            ((checks_passed++))
        else
            echo "  ✗ recipe review not verified"
            echo "    → Re-run recipe-search.sh quick"
            all_passed=false
        fi
    elif [ -f "$RECIPE_REVIEW_FILE" ]; then
        echo "  ⚠ Cannot verify (jq unavailable)"
        ((checks_passed++))
    fi

    # Check 3: patterns_extracted exists
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$RECIPE_REVIEW_FILE" ]; then
        if jq -e '.patterns_extracted' "$RECIPE_REVIEW_FILE" >/dev/null 2>&1; then
            echo "  ✓ patterns extracted"
            ((checks_passed++))
        else
            echo "  ✗ patterns not extracted"
            echo "    → Re-run recipe-search.sh quick"
            all_passed=false
        fi
    elif [ -f "$RECIPE_REVIEW_FILE" ]; then
        echo "  ⚠ Cannot verify patterns (jq unavailable)"
        ((checks_passed++))
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        # Show what was discovered
        echo ""
        echo "📋 Discovered patterns:"
        if command -v jq &>/dev/null && [ -f "$RECIPE_REVIEW_FILE" ]; then
            local runtime managed
            runtime=$(jq -r '.runtimes_identified[0] // "unknown"' "$RECIPE_REVIEW_FILE" 2>/dev/null)
            managed=$(jq -r '.managed_services_identified[0] // "none"' "$RECIPE_REVIEW_FILE" 2>/dev/null)
            echo "   Runtime: $runtime"
            echo "   Managed: $managed"
        fi
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - review recipes before proceeding"
        echo ""
        echo "The Recipe Search Tool prevents 10+ common mistakes by:"
        echo "  • Providing correct version strings (go@1 not go@latest)"
        echo "  • Showing valid YAML fields and structure"
        echo "  • Extracting production patterns (alpine, cache, etc.)"
        echo ""
        echo "This gate exists because every single documented mistake"
        echo "could have been prevented by reviewing recipes first."
        return 1
    fi
}

# ============================================================================
# Gate 1: DISCOVER → DEVELOP
# ============================================================================

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
