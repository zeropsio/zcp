#!/bin/bash
# Gate checks for Zerops Workflow phase transitions
#
# Each gate validates prerequisites before allowing phase transitions.
# Gates output structured recovery hints (JSON to stderr) for WIGGUM
# (Workflow Infrastructure for Guided Gates and Unified Management)
# to enable automated error recovery.

# ============================================================================
# GATE CHECK HELPERS (reduce duplication across gate functions)
# ============================================================================

# Global variables for gate checking (reset by gate_start)
_GATE_CHECKS_PASSED=0
_GATE_CHECKS_TOTAL=0
_GATE_ALL_PASSED=true

# Initialize a new gate check sequence
# Usage: gate_start "Gate: FROM → TO"
gate_start() {
    local header="$1"
    _GATE_CHECKS_PASSED=0
    _GATE_CHECKS_TOTAL=0
    _GATE_ALL_PASSED=true
    echo "$header"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# Record a passed check
# Usage: gate_pass "check description"
gate_pass() {
    local msg="$1"
    echo "  ✓ $msg"
    ((_GATE_CHECKS_PASSED++))
    ((_GATE_CHECKS_TOTAL++))
}

# Record a failed check
# Usage: gate_fail "check description" "fix instruction"
gate_fail() {
    local msg="$1"
    local fix="$2"
    echo "  ✗ $msg"
    [ -n "$fix" ] && echo "    → $fix"
    ((_GATE_CHECKS_TOTAL++))
    _GATE_ALL_PASSED=false
}

# Record a warning (counts as passed but shows warning)
# Usage: gate_warn "warning message"
gate_warn() {
    local msg="$1"
    echo "  ⚠ $msg"
    ((_GATE_CHECKS_PASSED++))
    ((_GATE_CHECKS_TOTAL++))
}

# Check if a file exists
# Usage: gate_check_file "$FILE" "filename" "fix command"
gate_check_file() {
    local file="$1"
    local name="$2"
    local fix="$3"
    if [ -f "$file" ]; then
        gate_pass "$name exists"
        return 0
    else
        gate_fail "$name missing" "$fix"
        return 1
    fi
}

# Check session ID matches
# Usage: gate_check_session "$FILE"
gate_check_session() {
    local file="$1"
    if check_evidence_session "$file"; then
        gate_pass "session_id matches current session"
        return 0
    else
        gate_fail "session_id mismatch" "Re-run command for current session"
        return 1
    fi
}

# Check verification has zero failures
# Usage: gate_check_no_failures "$FILE" "context"
gate_check_no_failures() {
    local file="$1"
    local context="${2:-verification}"
    if command -v jq &>/dev/null && [ -f "$file" ]; then
        local failures
        failures=$(jq -r '.failed // 0' "$file" 2>/dev/null)
        if ! [[ "$failures" =~ ^[0-9]+$ ]]; then
            gate_fail "Cannot read failure count from evidence file"
            return 1
        elif [ "$failures" -eq 0 ]; then
            local passed
            passed=$(jq -r '.passed // 0' "$file" 2>/dev/null)
            gate_pass "$context passed ($passed endpoints, 0 failures)"
            return 0
        else
            gate_fail "$context has $failures failure(s)" "Fix failing endpoints before proceeding"
            return 1
        fi
    fi
    return 1
}

# Finish gate and return result
# Usage: gate_finish [evidence_file_for_freshness] [hours]
# Returns: 0 if all passed, 1 if any failed
gate_finish() {
    local evidence_file="$1"
    local hours="${2:-24}"

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $_GATE_CHECKS_PASSED/$_GATE_CHECKS_TOTAL checks passed"

    if [ "$_GATE_ALL_PASSED" = true ]; then
        [ -n "$evidence_file" ] && [ -f "$evidence_file" ] && check_evidence_freshness "$evidence_file" "$hours"
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - fix issues above before proceeding"
        return 1
    fi
}

# ============================================================================
# Gate 0: INIT → DISCOVER/COMPOSE (Recipe Review)
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
# Gate 0.5: Import Validation (called by extend command)
# ============================================================================
# This gate was added after a documented failure where an agent:
# - Read the recipe showing buildFromGit and zeropsSetup
# - Created import.yml WITHOUT these critical fields
# - Caused services to be stuck in READY_TO_DEPLOY (empty containers)
#
# The gate enforces: "USE THE RECIPE'S IMPORT.YML - don't invent your own!"
# ============================================================================

check_gate_import_validation() {
    local import_file="$1"
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate 0.5: Import Validation"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: import file exists
    ((checks_total++))
    if [ -f "$import_file" ]; then
        echo "  ✓ Import file exists: $import_file"
        ((checks_passed++))
    else
        echo "  ✗ Import file not found: $import_file"
        all_passed=false
    fi

    # Check 2: YAML syntax valid
    ((checks_total++))
    if command -v yq &>/dev/null; then
        if yq '.' "$import_file" > /dev/null 2>&1; then
            echo "  ✓ YAML syntax valid"
            ((checks_passed++))
        else
            echo "  ✗ Invalid YAML syntax"
            all_passed=false
        fi
    else
        echo "  ⚠ yq not available - skipping YAML check"
        ((checks_passed++))
    fi

    # Check 3: Runtime services have code source
    ((checks_total++))
    if command -v yq &>/dev/null && [ -f "$import_file" ]; then
        local runtime_without_code=0
        local service_count
        service_count=$(yq '.services | length // 0' "$import_file" 2>/dev/null || echo "0")

        local i=0
        while [ $i -lt "$service_count" ]; do
            local type hostname build_from_git start_without_code
            type=$(yq ".services[$i].type // empty" "$import_file" 2>/dev/null)
            hostname=$(yq ".services[$i].hostname // empty" "$import_file" 2>/dev/null)
            build_from_git=$(yq ".services[$i].buildFromGit // empty" "$import_file" 2>/dev/null)
            start_without_code=$(yq ".services[$i].startWithoutCode // empty" "$import_file" 2>/dev/null)

            # Check if it's a runtime type
            if echo "$type" | grep -qE "^(go|nodejs|php|python|rust|bun|dotnet|java|nginx|static|alpine)@"; then
                # Runtime must have buildFromGit OR startWithoutCode
                if [ -z "$build_from_git" ] || [ "$build_from_git" = "null" ]; then
                    if [ "$start_without_code" != "true" ]; then
                        ((runtime_without_code++))
                        echo "    ⚠ $hostname: no buildFromGit or startWithoutCode"
                    fi
                fi
            fi
            ((i++))
        done

        if [ $runtime_without_code -eq 0 ]; then
            echo "  ✓ All runtime services have code source"
            ((checks_passed++))
        else
            echo "  ✗ $runtime_without_code runtime service(s) missing code source"
            echo "    → Add buildFromGit: <repo-url> or startWithoutCode: true"
            all_passed=false
        fi
    else
        echo "  ⚠ Cannot validate code source (yq unavailable)"
        ((checks_passed++))
    fi

    # Check 4: Runtime services have zeropsSetup
    ((checks_total++))
    if command -v yq &>/dev/null && [ -f "$import_file" ]; then
        local runtime_without_setup=0
        local service_count
        service_count=$(yq '.services | length // 0' "$import_file" 2>/dev/null || echo "0")

        local i=0
        while [ $i -lt "$service_count" ]; do
            local type hostname zerops_setup
            type=$(yq ".services[$i].type // empty" "$import_file" 2>/dev/null)
            hostname=$(yq ".services[$i].hostname // empty" "$import_file" 2>/dev/null)
            zerops_setup=$(yq ".services[$i].zeropsSetup // empty" "$import_file" 2>/dev/null)

            # Check if it's a runtime type
            if echo "$type" | grep -qE "^(go|nodejs|php|python|rust|bun|dotnet|java|nginx|static|alpine)@"; then
                if [ -z "$zerops_setup" ] || [ "$zerops_setup" = "null" ]; then
                    ((runtime_without_setup++))
                    echo "    ⚠ $hostname: no zeropsSetup"
                fi
            fi
            ((i++))
        done

        if [ $runtime_without_setup -eq 0 ]; then
            echo "  ✓ All runtime services have zeropsSetup"
            ((checks_passed++))
        else
            echo "  ✗ $runtime_without_setup runtime service(s) missing zeropsSetup"
            echo "    → Add zeropsSetup: dev or zeropsSetup: prod"
            all_passed=false
        fi
    else
        echo "  ⚠ Cannot validate zeropsSetup (yq unavailable)"
        ((checks_passed++))
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - import.yml validation errors"
        echo ""
        echo "This gate prevents a documented failure where:"
        echo "  • Agent read recipe showing buildFromGit/zeropsSetup"
        echo "  • Agent created import.yml WITHOUT these fields"
        echo "  • Services ended up in READY_TO_DEPLOY (empty)"
        echo ""
        echo "Fix: Use the recipe's import.yml directly, or add missing fields"
        echo "Run: .zcp/validate-import.sh $import_file"
        return 1
    fi
}

# ============================================================================
# Gate 1: DISCOVER → DEVELOP
# ============================================================================

check_gate_discover_to_develop() {
    gate_start "Gate: DISCOVER → DEVELOP"

    # Check 1: discovery.json exists
    gate_check_file "$DISCOVERY_FILE" "discovery.json" \
        "Run: .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}"

    # Check 2: session_id matches
    gate_check_session "$DISCOVERY_FILE"

    # Check 3: dev != stage (unless single_mode)
    if command -v jq &>/dev/null && [ -f "$DISCOVERY_FILE" ]; then
        local dev_name stage_name single_mode
        dev_name=$(jq -r '.dev.name' "$DISCOVERY_FILE" 2>/dev/null)
        stage_name=$(jq -r '.stage.name' "$DISCOVERY_FILE" 2>/dev/null)
        single_mode=$(jq -r '.single_mode // false' "$DISCOVERY_FILE" 2>/dev/null)

        if [ "$dev_name" != "$stage_name" ]; then
            gate_pass "dev ≠ stage ($dev_name vs $stage_name)"
        elif [ "$single_mode" = "true" ]; then
            gate_warn "single-service mode (dev = stage = $dev_name)"
            echo "    → Intentional: source corruption risk acknowledged"
        else
            gate_fail "dev.name == stage.name ('$dev_name')" \
                "Cannot use same service for dev and stage (zcli push overwrites /var/www/)"
        fi
    else
        gate_warn "Cannot verify dev≠stage (jq unavailable or no discovery)"
    fi

    gate_finish "$DISCOVERY_FILE" 24
}

check_gate_develop_to_deploy() {
    gate_start "Gate: DEVELOP → DEPLOY"

    # Check 1: dev_verify.json exists
    gate_check_file "$DEV_VERIFY_FILE" "dev_verify.json" \
        "Run: .zcp/verify.sh {dev} {port} / /status /api/..."

    # Check 2: session_id matches
    gate_check_session "$DEV_VERIFY_FILE"

    # Check 3: failures == 0
    gate_check_no_failures "$DEV_VERIFY_FILE" "verification"

    gate_finish "$DEV_VERIFY_FILE" 24
}

check_gate_deploy_to_verify() {
    gate_start "Gate: DEPLOY → VERIFY"

    # Check 1: deploy_evidence.json exists
    gate_check_file "$DEPLOY_EVIDENCE_FILE" "deploy_evidence.json" \
        "Run: .zcp/status.sh --wait {stage}"

    # Check 2: session_id matches
    gate_check_session "$DEPLOY_EVIDENCE_FILE"

    gate_finish "$DEPLOY_EVIDENCE_FILE" 24
}

check_gate_verify_to_done() {
    gate_start "Gate: VERIFY → DONE"

    # Check 1: stage_verify.json exists
    gate_check_file "$STAGE_VERIFY_FILE" "stage_verify.json" \
        "Run: .zcp/verify.sh {stage} {port} / /status /api/..."

    # Check 2: session_id matches
    gate_check_session "$STAGE_VERIFY_FILE"

    # Check 3: failures == 0
    gate_check_no_failures "$STAGE_VERIFY_FILE" "verification"

    gate_finish "$STAGE_VERIFY_FILE" 24
}

# ============================================================================
# Gate S: Synthesis Complete (SYNTHESIZE → DEVELOP)
# ============================================================================
# Validates that agent-created code exists and is properly structured

check_gate_synthesis() {
    local synthesis_file="$SYNTHESIS_COMPLETE_FILE"
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate S: SYNTHESIZE → DEVELOP"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: synthesis_complete.json exists
    ((checks_total++))
    if [ -f "$synthesis_file" ]; then
        echo "  ✓ synthesis_complete.json exists"
        ((checks_passed++))
    else
        echo "  ✗ synthesis_complete.json missing"
        echo "    → Run: .zcp/workflow.sh verify_synthesis"
        echo "    → Prerequisites:"
        echo "      - Services must be RUNNING"
        echo "      - Code files must exist in /var/www/{dev}/"
        echo "      - zerops.yml must have zerops: wrapper"
        all_passed=false

        # Output JSON fix instructions
        cat <<EOF >&2

{
  "gate": "GATE_S",
  "status": "BLOCKED",
  "reason": "Synthesis not verified",
  "evidence_required": "$synthesis_file",
  "fix": {
    "command": ".zcp/workflow.sh verify_synthesis",
    "prerequisites": [
      "Services must be RUNNING",
      "Code files must exist in /var/www/{dev}/",
      "zerops.yml must have zerops: wrapper"
    ]
  }
}
EOF
        return 2
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if check_evidence_session "$synthesis_file"; then
        echo "  ✓ session_id matches current session"
        ((checks_passed++))
    else
        local current_session=$(get_session)
        local evidence_session=$(jq -r '.session_id // "none"' "$synthesis_file" 2>/dev/null)
        echo "  ✗ session_id mismatch"
        echo "    → Current session: $current_session"
        echo "    → Evidence session: $evidence_session"
        echo "    → Re-run: .zcp/workflow.sh verify_synthesis"
        all_passed=false

        cat <<EOF >&2

{
  "gate": "GATE_S",
  "status": "BLOCKED",
  "reason": "Synthesis evidence from different session",
  "current_session": "$current_session",
  "evidence_session": "$evidence_session",
  "fix": {
    "command": ".zcp/workflow.sh verify_synthesis",
    "note": "Re-run to update evidence with current session"
  }
}
EOF
        return 2
    fi

    # Check 3: status is complete
    ((checks_total++))
    if command -v jq &>/dev/null && [ -f "$synthesis_file" ]; then
        local status
        status=$(jq -r '.status // "unknown"' "$synthesis_file" 2>/dev/null)
        if [ "$status" = "complete" ]; then
            local files_count
            files_count=$(jq -r '.files_verified | length' "$synthesis_file" 2>/dev/null || echo "0")
            echo "  ✓ synthesis complete ($files_count files verified)"
            ((checks_passed++))
        else
            echo "  ✗ synthesis status: $status (expected: complete)"
            echo "    → Check errors in synthesis_complete.json"
            all_passed=false
        fi
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - verify synthesis before proceeding"
        return 1
    fi
}

# ============================================================================
# Gate: COMPOSE → EXTEND (Synthesis Plan)
# ============================================================================

check_gate_compose_to_extend() {
    local synthesis_plan="$SYNTHESIS_PLAN_FILE"
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: COMPOSE → EXTEND"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: synthesis_plan.json exists
    ((checks_total++))
    if [ -f "$synthesis_plan" ]; then
        echo "  ✓ synthesis_plan.json exists"
        ((checks_passed++))
    else
        echo "  ✗ synthesis_plan.json missing"
        echo "    → Run: .zcp/workflow.sh compose --runtime {runtime} --services {services}"
        all_passed=false
    fi

    # Check 2: synthesized_import.yml exists
    ((checks_total++))
    if [ -f "$SYNTHESIZED_IMPORT_FILE" ]; then
        echo "  ✓ synthesized_import.yml exists"
        ((checks_passed++))
    else
        echo "  ✗ synthesized_import.yml missing"
        echo "    → Run: .zcp/workflow.sh compose --runtime {runtime} --services {services}"
        all_passed=false
    fi

    # Check 3: session_id matches (if file exists)
    ((checks_total++))
    if [ -f "$synthesis_plan" ]; then
        if check_evidence_session "$synthesis_plan"; then
            echo "  ✓ session_id matches current session"
            ((checks_passed++))
        else
            echo "  ✗ session_id mismatch - re-run compose"
            all_passed=false
        fi
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - run compose command first"
        return 1
    fi
}

# ============================================================================
# Gate: EXTEND → SYNTHESIZE (Services Imported)
# ============================================================================

check_gate_extend_to_synthesize() {
    local services_imported="$SERVICES_IMPORTED_FILE"
    local checks_passed=0
    local checks_total=0
    local all_passed=true

    echo "Gate: EXTEND → SYNTHESIZE"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    # Check 1: services_imported.json exists
    ((checks_total++))
    if [ -f "$services_imported" ]; then
        echo "  ✓ services_imported.json exists"
        ((checks_passed++))
    else
        echo "  ✗ services_imported.json missing"
        echo "    → Run: .zcp/workflow.sh extend $SYNTHESIZED_IMPORT_FILE"
        all_passed=false
    fi

    # Check 2: session_id matches
    ((checks_total++))
    if [ -f "$services_imported" ]; then
        if check_evidence_session "$services_imported"; then
            echo "  ✓ session_id matches current session"
            ((checks_passed++))
        else
            echo "  ✗ session_id mismatch - re-run extend"
            all_passed=false
        fi
    fi

    # Check 3: services are RUNNING
    ((checks_total++))
    if [ -f "$services_imported" ] && command -v jq &>/dev/null; then
        local all_running
        all_running=$(jq -r '.services | all(.status == "RUNNING")' "$services_imported" 2>/dev/null || echo "false")
        if [ "$all_running" = "true" ]; then
            echo "  ✓ all services are RUNNING"
            ((checks_passed++))
        else
            echo "  ✗ not all services are RUNNING"
            echo "    → Wait for services: .zcp/status.sh --wait {service}"
            all_passed=false
        fi
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Result: $checks_passed/$checks_total checks passed"

    if [ "$all_passed" = true ]; then
        return 0
    else
        echo ""
        echo "❌ Gate FAILED - ensure services are imported and running"
        return 1
    fi
}
