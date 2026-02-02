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

# Check verification evidence (supports both attestation and legacy HTTP/process/logs)
# Usage: gate_check_verification "$FILE" "context"
# Also aliased as gate_check_no_failures for backward compatibility
gate_check_verification() {
    local file="$1"
    local context="${2:-verification}"

    if [ ! -f "$file" ]; then
        gate_fail "No $context evidence" \
            "Run: .zcp/verify.sh {service} \"what you verified\""
        return 1
    fi

    if ! command -v jq &>/dev/null; then
        gate_fail "jq required for evidence validation"
        return 1
    fi

    local verify_type
    verify_type=$(jq -r '.verification_type // "http"' "$file" 2>/dev/null)

    case "$verify_type" in
        attestation)
            local attestation
            attestation=$(jq -r '.attestation // empty' "$file" 2>/dev/null)

            if [ -z "$attestation" ]; then
                gate_fail "Empty attestation" \
                    "Run: .zcp/verify.sh {service} \"what you verified\""
                return 1
            fi

            gate_pass "$context: ${attestation:0:50}..."
            return 0
            ;;
        *)
            # Legacy HTTP/process/logs - check passed > 0, failed == 0

            # Check for preflight failure (HTTP mode only)
            if [ "$verify_type" = "http" ]; then
                local preflight_failed
                preflight_failed=$(jq -r '.preflight_failed // false' "$file" 2>/dev/null)
                if [ "$preflight_failed" = "true" ]; then
                    local reason
                    reason=$(jq -r '.preflight_reason // "Unknown pre-flight failure"' "$file" 2>/dev/null)
                    gate_fail "Pre-flight check failed: $reason" "Ensure process is running on port"
                    return 1
                fi
            fi

            local passed failed
            passed=$(jq -r '.passed // 0' "$file" 2>/dev/null)
            failed=$(jq -r '.failed // 0' "$file" 2>/dev/null)

            if ! [[ "$failed" =~ ^[0-9]+$ ]]; then
                gate_fail "Cannot read failure count from evidence file"
                return 1
            elif [ "$failed" -gt 0 ]; then
                gate_fail "$context has $failed failure(s)"
                return 1
            fi

            if [ "$passed" -eq 0 ]; then
                gate_warn "No checks recorded"
            fi

            gate_pass "$context passed (legacy mode)"
            return 0
            ;;
    esac
}

# Backward compatibility alias
gate_check_no_failures() {
    gate_check_verification "$@"
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
# MULTI-SERVICE VERIFICATION (FIX-02)
# ============================================================================

# Multi-service verification check
# Checks that ALL services in discovery.json have verify files
# Usage: gate_check_all_services_verified "dev" "_verify.json"
gate_check_all_services_verified() {
    local role="$1"  # "dev" or "stage"
    local file_suffix="${2:-_verify.json}"

    if [ ! -f "$DISCOVERY_FILE" ]; then
        return 0  # No discovery, fall back to single-service behavior
    fi

    local service_count
    service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

    if [ "$service_count" -le 1 ]; then
        return 0  # Single service, handled by existing checks
    fi

    # Multi-service: check each service's verify file
    local verified=0
    local missing_services=""

    while IFS= read -r service_name; do
        [ -z "$service_name" ] && continue

        local verify_file="${ZCP_TMP_DIR:-/tmp}/${service_name}${file_suffix}"
        if [ -f "$verify_file" ]; then
            # Check session and failures
            if check_evidence_session "$verify_file" 2>/dev/null; then
                local failures
                failures=$(jq -r '.failed // 0' "$verify_file" 2>/dev/null)
                local preflight_failed
                preflight_failed=$(jq -r '.preflight_failed // false' "$verify_file" 2>/dev/null)

                if [ "$failures" -eq 0 ] && [ "$preflight_failed" != "true" ]; then
                    verified=$((verified + 1))
                else
                    missing_services="$missing_services $service_name(failed)"
                fi
            else
                missing_services="$missing_services $service_name(stale)"
            fi
        else
            missing_services="$missing_services $service_name(missing)"
        fi
    done < <(jq -r ".services[].${role}.name" "$DISCOVERY_FILE" 2>/dev/null)

    if [ "$verified" -lt "$service_count" ]; then
        gate_fail "$verified/$service_count ${role} services verified" \
            "Missing:$missing_services - HTTP: .zcp/verify.sh {hostname} 8080 / | Worker: --process {hostname} {proc}"
        return 1
    else
        gate_pass "All $service_count ${role} services verified"
        return 0
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

    # EXISTING PROJECT CHECK: Skip Gate 0 if valid discovery exists
    # This enables iteration on existing projects without requiring recipe review
    if [ -f "$DISCOVERY_FILE" ]; then
        # Check if discovery has matching session OR is recent (< 24h)
        if check_evidence_session "$DISCOVERY_FILE" 2>/dev/null; then
            echo "  ✓ Existing project: discovery.json matches current session"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "Gate 0 SKIPPED - existing project iteration"
            return 0
        fi

        # Also allow if discovery is fresh (even if session doesn't match)
        # This handles the case where discovery was preserved across sessions
        local disco_timestamp
        disco_timestamp=$(jq -r '.timestamp // empty' "$DISCOVERY_FILE" 2>/dev/null)
        if [ -n "$disco_timestamp" ]; then
            local disco_epoch now_epoch age_hours
            # Try GNU date first, then BSD date
            if disco_epoch=$(date -d "$disco_timestamp" +%s 2>/dev/null) || \
               disco_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$disco_timestamp" +%s 2>/dev/null); then
                now_epoch=$(date +%s)
                age_hours=$(( (now_epoch - disco_epoch) / 3600 ))

                if [ "$age_hours" -lt 24 ]; then
                    echo "  ✓ Existing project: discovery.json is ${age_hours}h old (< 24h)"
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo "Gate 0 SKIPPED - existing project iteration"

                    # Update session_id in discovery to current session
                    local current_session
                    current_session=$(get_session)
                    if [ -n "$current_session" ]; then
                        jq --arg sid "$current_session" '.session_id = $sid' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp" && \
                            mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"
                    fi
                    return 0
                fi
            fi
        fi

        # Discovery exists but is stale - show warning but still require Gate 0
        echo "  ⚠ discovery.json exists but is stale (> 24h)"
        echo "    Consider: Is this the same project? If so, update discovery."
    fi

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
    local mode
    mode=$(get_mode)

    # Quick mode bypasses gate
    if [ "$mode" = "quick" ]; then
        echo "⚠️  QUICK MODE: Gate 0.5 bypassed"
        return 0
    fi

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
    local mode
    mode=$(get_mode)

    # Quick mode bypasses gate
    if [ "$mode" = "quick" ]; then
        echo "⚠️  QUICK MODE: Gate bypassed"
        return 0
    fi

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
    local mode
    mode=$(get_mode)

    # Quick mode bypasses gate
    if [ "$mode" = "quick" ]; then
        echo "⚠️  QUICK MODE: Gate bypassed"
        return 0
    fi

    gate_start "Gate: DEVELOP → DEPLOY"

    # Check 1: dev_verify.json exists
    gate_check_file "$DEV_VERIFY_FILE" "dev_verify.json" \
        "HTTP service: .zcp/verify.sh {dev} {port} / ... | Worker: .zcp/verify.sh --process {dev} {proc}"

    # Check 2: session_id matches
    gate_check_session "$DEV_VERIFY_FILE"

    # Check 3: failures == 0
    gate_check_no_failures "$DEV_VERIFY_FILE" "verification"

    # Check 4: Config validation (integrated Gate 3)
    # Validates zerops.yml before allowing deployment
    if [ -f "$DISCOVERY_FILE" ] && command -v jq &>/dev/null; then
        local dev_service
        dev_service=$(jq -r '.dev.name' "$DISCOVERY_FILE" 2>/dev/null)
        if [ -n "$dev_service" ] && [ "$dev_service" != "null" ]; then
            local config_file="/var/www/$dev_service/zerops.yml"
            if [ -f "$config_file" ]; then
                if command -v yq &>/dev/null; then
                    if yq e '.zerops' "$config_file" > /dev/null 2>&1; then
                        gate_pass "zerops.yml has valid structure"
                    else
                        gate_fail "zerops.yml missing 'zerops:' wrapper" \
                            "Add 'zerops:' as top-level key in $config_file"
                    fi
                else
                    gate_warn "Cannot validate zerops.yml (yq unavailable)"
                fi
            else
                # BREAKING CHANGE: Previously gate_warn, now gate_fail
                # Rationale: Missing zerops.yml means deploy will fail anyway.
                # Failing early with clear guidance is better than proceeding to a broken deploy.
                gate_fail "zerops.yml not found at $config_file" \
                    "Subagent should have created /var/www/$dev_service/zerops.yml"
            fi
        fi
    fi

    # FIX-02: Multi-service verification enforcement
    gate_check_all_services_verified "dev" "_verify.json"

    # Gap 45: Check if multi-service with dependencies
    if [ -f "$DISCOVERY_FILE" ]; then
        local has_deps
        has_deps=$(jq -r '[.services[] | select(.depends_on)] | length' "$DISCOVERY_FILE" 2>/dev/null)

        if [ "$has_deps" -gt 0 ]; then
            gate_warn "Deploy order dependencies detected - deploy services in order shown by 'workflow.sh show'"
        fi
    fi

    gate_finish "$DEV_VERIFY_FILE" 24
}

check_gate_deploy_to_verify() {
    local mode
    mode=$(get_mode)

    # Quick mode bypasses gate
    if [ "$mode" = "quick" ]; then
        echo "⚠️  QUICK MODE: Gate bypassed"
        return 0
    fi

    gate_start "Gate: DEPLOY → VERIFY"

    # Check 1: deploy_evidence.json exists
    gate_check_file "$DEPLOY_EVIDENCE_FILE" "deploy_evidence.json" \
        "Run: .zcp/status.sh --wait {stage}"

    # Check 2: session_id matches
    gate_check_session "$DEPLOY_EVIDENCE_FILE"

    gate_finish "$DEPLOY_EVIDENCE_FILE" 24
}

check_gate_verify_to_done() {
    local mode
    mode=$(get_mode)

    # Quick mode bypasses gate
    if [ "$mode" = "quick" ]; then
        echo "⚠️  QUICK MODE: Gate bypassed"
        return 0
    fi

    gate_start "Gate: VERIFY → DONE"

    # Check 1: stage_verify.json exists
    gate_check_file "$STAGE_VERIFY_FILE" "stage_verify.json" \
        "Run: .zcp/verify.sh {stage} {port} / /status /api/..."

    # Check 2: session_id matches
    gate_check_session "$STAGE_VERIFY_FILE"

    # Check 3: failures == 0
    gate_check_no_failures "$STAGE_VERIFY_FILE" "verification"

    # FIX-02: Multi-service stage verification
    gate_check_all_services_verified "stage" "_verify.json"

    gate_finish "$STAGE_VERIFY_FILE" 24
}

# ============================================================================
# Gate B: BOOTSTRAP -> WORKFLOW
# ============================================================================

check_gate_bootstrap_to_workflow() {
    local mode
    mode=$(get_mode 2>/dev/null || echo "full")
    [[ "$mode" == "quick" ]] && { echo "  ⚠️  QUICK MODE: Gate bypassed"; return 0; }

    gate_start "Gate B: BOOTSTRAP → WORKFLOW"

    local unified="${ZCP_TMP_DIR:-/tmp}/zcp_state.json"
    local legacy="${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"
    local state_src=""

    if [[ -f "$unified" ]]; then
        state_src="unified"
    elif [[ -f "$legacy" ]]; then
        state_src="legacy"
    else
        gate_fail "No bootstrap evidence" "Run: .zcp/workflow.sh bootstrap --runtime <type>"
        gate_finish
        return 1
    fi
    gate_pass "Bootstrap evidence found ($state_src)"

    if [[ "$state_src" == "unified" ]]; then
        local bc
        bc=$(jq -r '.workflow.bootstrap_complete // false' "$unified" 2>/dev/null)
        if [[ "$bc" == "true" ]]; then
            gate_pass "Bootstrap complete in unified state"
        else
            local ba
            ba=$(jq -r '.bootstrap.active // false' "$unified" 2>/dev/null)
            if [[ "$ba" == "true" ]]; then
                gate_fail "Bootstrap in progress" ".zcp/bootstrap.sh resume"
            else
                gate_fail "Bootstrap not done" ".zcp/bootstrap.sh done"
            fi
        fi
        local incomplete
        incomplete=$(jq -r '[.bootstrap.steps[] | select(.status != "complete")] | length' "$unified" 2>/dev/null)
        if [[ "$incomplete" == "0" ]] || [[ -z "$incomplete" ]]; then
            gate_pass "All steps complete"
        else
            local names
            names=$(jq -r '[.bootstrap.steps[] | select(.status != "complete") | .name] | join(", ")' "$unified" 2>/dev/null)
            gate_fail "$incomplete step(s) incomplete: $names" ".zcp/bootstrap.sh step <name>"
        fi
    else
        local st
        st=$(jq -r '.status // "unknown"' "$legacy" 2>/dev/null)
        [[ "$st" == "complete" ]] && gate_pass "Legacy complete" || gate_fail "Legacy status: $st" ".zcp/bootstrap.sh done"
        gate_warn "Step verification skipped (legacy)"
    fi

    gate_finish
}
export -f check_gate_bootstrap_to_workflow
