#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/aggregate-results.sh
# Step: Wait for all subagents to complete, create evidence files, transition to DONE
#
# DISPLAY-ONLY: This step reads cached data from subagent completion files.
# It does NOT re-test endpoints, re-fetch URLs, or make redundant API calls.
# Subagents write /tmp/{hostname}_complete.json with all needed data.
#
# FALLBACK DETECTION: If completion file is missing but state file exists
# (from mark-complete.sh), uses state. If both missing but files exist
# (zerops.yml, source code), auto-marks complete.
#
# Inputs: /tmp/{hostname}_complete.json from subagents, state files
# Outputs: discovery.json, dev_verify.json, stage_verify.json, deploy_evidence.json,
#          bootstrap_complete.json, workflow ready for DONE (use iterate to start work)

# Verify completion by checking actual files exist
# Returns 0 if evidence of completion exists, 1 otherwise
verify_completion_by_files() {
    local dev_hostname="$1"
    local mount_path="/var/www/$dev_hostname"

    # Must have zerops.yml
    if [ ! -f "$mount_path/zerops.yml" ]; then
        return 1
    fi

    # Must have some source code
    local has_code=false
    for pattern in main.go index.js app.py main.py server.go index.ts app.ts; do
        if [ -f "$mount_path/$pattern" ]; then
            has_code=true
            break
        fi
    done

    # Also check for any source files if specific patterns don't match
    if [ "$has_code" = false ]; then
        if find "$mount_path" -maxdepth 2 -type f \( -name "*.go" -o -name "*.js" -o -name "*.py" -o -name "*.ts" -o -name "*.rs" \) 2>/dev/null | grep -q .; then
            has_code=true
        fi
    fi

    if [ "$has_code" = false ]; then
        return 1
    fi

    # All checks passed - evidence of completion exists
    return 0
}

# Auto-mark a service complete using mark-complete.sh
auto_mark_complete() {
    local dev_hostname="$1"
    local script_dir="${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../../..}"
    local mark_script="$script_dir/mark-complete.sh"

    if [ -x "$mark_script" ]; then
        "$mark_script" "$dev_hostname" >/dev/null 2>&1
        return $?
    else
        # Fallback: write directly if script not found
        local state_dir="${BOOTSTRAP_STATE_DIR:-$script_dir/state/bootstrap}/services/$dev_hostname"
        mkdir -p "$state_dir" 2>/dev/null || return 1
        echo "{\"phase\":\"complete\",\"completed_at\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"auto_detected\":true}" > "$state_dir/status.json"
        return $?
    fi
}

# Read completion data from subagent's completion file
# Returns JSON with URLs and verification data, or empty object if not found
read_completion_data() {
    local dev_hostname="$1"
    local completion_file="${ZCP_TMP_DIR:-/tmp}/${dev_hostname}_complete.json"

    if [ -f "$completion_file" ]; then
        cat "$completion_file"
    else
        echo '{}'
    fi
}

step_aggregate_results() {
    local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"

    if [ ! -f "$handoff_file" ]; then
        json_error "aggregate-results" "No handoff file found" '{}' '["Run finalize step first"]'
        return 1
    fi

    local handoffs
    handoffs=$(jq -c '.service_handoffs' "$handoff_file")

    if [ "$handoffs" = "null" ] || [ -z "$handoffs" ]; then
        json_error "aggregate-results" "No service handoffs in handoff file" '{}' '["Re-run finalize step"]'
        return 1
    fi

    local count
    count=$(echo "$handoffs" | jq 'length')

    if [ "$count" -eq 0 ]; then
        json_error "aggregate-results" "Empty service handoffs array" '{}' '[]'
        return 1
    fi

    # Check each service's completion state
    local complete=0
    local failed=0
    local pending=()
    local results='[]'

    local i=0
    while [ "$i" -lt "$count" ]; do
        local dev_hostname
        dev_hostname=$(echo "$handoffs" | jq -r ".[$i].dev_hostname")

        # Get service state
        local service_state
        service_state=$(get_service_state "$dev_hostname" 2>/dev/null || echo '{}')

        local phase
        phase=$(echo "$service_state" | jq -r '.phase // "unknown"')

        # FALLBACK DETECTION: If phase is unknown but files exist, auto-mark complete
        # This handles cases where subagent couldn't write state file (shell context issues)
        local auto_detected=false
        if [ "$phase" = "unknown" ] || [ "$phase" = "null" ] || [ -z "$phase" ]; then
            local mount_path
            mount_path=$(echo "$handoffs" | jq -r ".[$i].mount_path // \"/var/www/$dev_hostname\"")

            if verify_completion_by_files "$dev_hostname"; then
                # Files exist! Auto-mark as complete
                if auto_mark_complete "$dev_hostname"; then
                    phase="complete"
                    auto_detected=true
                    service_state=$(get_service_state "$dev_hostname" 2>/dev/null || echo '{"phase":"complete","auto_detected":true}')
                fi
            fi
        fi

        local result
        result=$(jq -n \
            --arg h "$dev_hostname" \
            --arg p "$phase" \
            --argjson auto "$auto_detected" \
            --argjson s "$service_state" \
            '{hostname: $h, phase: $p, auto_detected: $auto, state: $s}')

        results=$(echo "$results" | jq --argjson r "$result" '. + [$r]')

        case "$phase" in
            complete|completed)
                complete=$((complete + 1))
                ;;
            failed|error)
                failed=$((failed + 1))
                ;;
            *)
                pending+=("$dev_hostname:$phase")
                ;;
        esac

        i=$((i + 1))
    done

    # If there are pending services, return in_progress status
    if [ ${#pending[@]} -gt 0 ]; then
        local pending_json
        pending_json=$(printf '%s\n' "${pending[@]}" | jq -R . | jq -s .)

        # Check if ALL services are in "unknown" state - means subagents never ran
        local all_unknown=true
        for p in "${pending[@]}"; do
            local phase="${p#*:}"
            if [[ "$phase" != "unknown" ]] && [[ "$phase" != "null" ]] && [[ -n "$phase" ]]; then
                all_unknown=false
                break
            fi
        done

        local action_msg recovery_msg
        if [ "$complete" -eq 0 ] && [ "$all_unknown" = true ]; then
            # NO subagents have run at all - they were never spawned
            action_msg="SUBAGENTS WERE NEVER SPAWNED! You must use the Task tool to spawn them."
            recovery_msg="Read /tmp/bootstrap_spawn.json and spawn subagents via Task tool FIRST"

            # Output a clear error instead of just in_progress
            echo ""
            echo "╔═══════════════════════════════════════════════════════════════════╗"
            echo "║  ❌ ERROR: No subagents were spawned!                              ║"
            echo "╠═══════════════════════════════════════════════════════════════════╣"
            echo "║                                                                   ║"
            echo "║  The spawn-subagents step output instructions, but you did not    ║"
            echo "║  use the Task tool to actually spawn the subagents.               ║"
            echo "║                                                                   ║"
            echo "║  GO BACK and spawn subagents:                                     ║"
            echo "║  1. Read: /tmp/bootstrap_spawn.json                               ║"
            echo "║  2. For each .data.instructions[], use Task tool to spawn         ║"
            echo "║  3. Wait for subagents to complete                                ║"
            echo "║  4. THEN run aggregate-results again                              ║"
            echo "║                                                                   ║"
            echo "╚═══════════════════════════════════════════════════════════════════╝"
            echo ""
        else
            action_msg="Wait for subagents to complete, then re-run this step"
            recovery_msg="If subagent reported success but state file missing, use mark-complete.sh"
        fi

        local data
        data=$(jq -n \
            --argjson complete "$complete" \
            --argjson total "$count" \
            --argjson pending "$pending_json" \
            --argjson results "$results" \
            --arg action "$action_msg" \
            '{
                complete: $complete,
                total: $total,
                pending: $pending,
                results: $results,
                action: $action,
                recovery: {
                    description: "If subagent reported success but state file missing:",
                    steps: [
                        "1. Verify files exist: ls /var/www/{hostname}/zerops.yml",
                        "2. Mark complete manually: .zcp/mark-complete.sh {hostname}",
                        "3. Re-run: .zcp/bootstrap.sh step aggregate-results"
                    ],
                    note: "Auto-detection will mark complete if zerops.yml + source code exist"
                }
            }')

        json_progress "aggregate-results" \
            "$complete/$count complete, waiting for ${#pending[@]} service(s)" \
            "$data" \
            "aggregate-results"
        return 0
    fi

    # Check for failures
    if [ "$failed" -gt 0 ]; then
        local data
        data=$(jq -n \
            --argjson complete "$complete" \
            --argjson failed "$failed" \
            --argjson results "$results" \
            '{
                complete: $complete,
                failed: $failed,
                results: $results
            }')

        json_error "aggregate-results" "$failed subagent(s) failed" \
            "$data" \
            '["Check logs in .zcp/state/bootstrap/services/{hostname}/", "Re-run failed subagent tasks", "Then re-run aggregate-results"]'
        return 1
    fi

    # All complete - create discovery.json with all services
    local session_id
    session_id=$(cat "${ZCP_TMP_DIR:-/tmp}/zcp_session" 2>/dev/null || echo "bootstrap-$(date +%s)")

    # Build services array from handoffs + completion data (URLs from subagent)
    local services='[]'
    local i=0
    local any_single_mode="false"

    while [ "$i" -lt "$count" ]; do
        local handoff
        handoff=$(echo "$handoffs" | jq ".[$i]")

        local dev_id dev_name stage_id stage_name runtime
        dev_id=$(echo "$handoff" | jq -r '.dev_id')
        dev_name=$(echo "$handoff" | jq -r '.dev_hostname')
        stage_id=$(echo "$handoff" | jq -r '.stage_id')
        stage_name=$(echo "$handoff" | jq -r '.stage_hostname')
        runtime=$(echo "$handoff" | jq -r '.runtime // "unknown"')

        # Read completion data (URLs, verification) from subagent's file
        # This is the SINGLE SOURCE OF TRUTH - no re-fetching
        local completion_data
        completion_data=$(read_completion_data "$dev_name")

        local dev_url stage_url verification implementation
        dev_url=$(echo "$completion_data" | jq -r '.dev_url // ""')
        stage_url=$(echo "$completion_data" | jq -r '.stage_url // ""')
        verification=$(echo "$completion_data" | jq -c '.verification // {}')
        implementation=$(echo "$completion_data" | jq -r '.implementation // ""')

        # Check single_mode per service pair
        local single_mode="false"
        if [ "$dev_id" = "$stage_id" ]; then
            single_mode="true"
            any_single_mode="true"
        fi

        local service_entry
        service_entry=$(jq -n \
            --arg dev_id "$dev_id" \
            --arg dev_name "$dev_name" \
            --arg stage_id "$stage_id" \
            --arg stage_name "$stage_name" \
            --arg runtime "$runtime" \
            --arg dev_url "$dev_url" \
            --arg stage_url "$stage_url" \
            --arg impl "$implementation" \
            --argjson verification "$verification" \
            --argjson single "$single_mode" \
            '{
                dev: { id: $dev_id, name: $dev_name, url: $dev_url },
                stage: { id: $stage_id, name: $stage_name, url: $stage_url },
                runtime: $runtime,
                implementation: $impl,
                verification: $verification,
                single_mode: $single
            }')

        services=$(echo "$services" | jq --argjson s "$service_entry" '. + [$s]')
        i=$((i + 1))
    done

    # For backwards compatibility, also include primary (first) service at top level
    local first
    first=$(echo "$handoffs" | jq '.[0]')
    local primary_dev_id primary_dev_name primary_stage_id primary_stage_name
    primary_dev_id=$(echo "$first" | jq -r '.dev_id')
    primary_dev_name=$(echo "$first" | jq -r '.dev_hostname')
    primary_stage_id=$(echo "$first" | jq -r '.stage_id')
    primary_stage_name=$(echo "$first" | jq -r '.stage_hostname')

    # Get URLs from completion data (cached by subagent)
    local primary_completion
    primary_completion=$(read_completion_data "$primary_dev_name")
    local primary_dev_url primary_stage_url
    primary_dev_url=$(echo "$primary_completion" | jq -r '.dev_url // ""')
    primary_stage_url=$(echo "$primary_completion" | jq -r '.stage_url // ""')

    local primary_single_mode="false"
    if [ "$primary_dev_id" = "$primary_stage_id" ]; then
        primary_single_mode="true"
    fi

    # Create discovery.json (includes URLs from subagent - no re-fetching)
    local discovery
    discovery=$(jq -n \
        --arg session "$session_id" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg dev_id "$primary_dev_id" \
        --arg dev_name "$primary_dev_name" \
        --arg dev_url "$primary_dev_url" \
        --arg stage_id "$primary_stage_id" \
        --arg stage_name "$primary_stage_name" \
        --arg stage_url "$primary_stage_url" \
        --argjson single "$primary_single_mode" \
        --argjson services "$services" \
        --argjson service_count "$count" \
        '{
            session_id: $session,
            timestamp: $ts,
            single_mode: $single,
            dev: { id: $dev_id, name: $dev_name, url: $dev_url },
            stage: { id: $stage_id, name: $stage_name, url: $stage_url },
            services: $services,
            service_count: $service_count
        }')

    # Include discovered env vars if service_discovery.json exists
    local service_discovery="${ZCP_TMP_DIR:-/tmp}/service_discovery.json"
    if [ -f "$service_discovery" ]; then
        local env_data
        env_data=$(jq '.services // {}' "$service_discovery" 2>/dev/null || echo '{}')

        if [ "$env_data" != "{}" ] && [ "$env_data" != "null" ]; then
            discovery=$(echo "$discovery" | jq --argjson env "$env_data" '. + {discovered_env_vars: $env}')
        fi
    fi

    echo "$discovery" > "${ZCP_TMP_DIR:-/tmp}/discovery.json"

    # Also persist to state directory (STATE_DIR is set by utils.sh sourced from bootstrap.sh)
    if [ -n "${STATE_DIR:-}" ]; then
        mkdir -p "$STATE_DIR/workflow/evidence" 2>/dev/null || true
        cp "${ZCP_TMP_DIR:-/tmp}/discovery.json" "$STATE_DIR/workflow/evidence/" 2>/dev/null || true
    fi

    # =========================================================================
    # SYNTHESIZE EVIDENCE FILES from subagent completion data
    # This ensures workflow ends in same state as normal DONE phase
    # =========================================================================

    # Aggregate dev verification results
    local dev_verify_results='[]'
    local dev_total_passed=0
    local dev_total_failed=0
    local i=0
    while [ "$i" -lt "$count" ]; do
        local dev_name
        dev_name=$(echo "$handoffs" | jq -r ".[$i].dev_hostname")
        local completion_data
        completion_data=$(read_completion_data "$dev_name")

        local dev_passed dev_failed
        dev_passed=$(echo "$completion_data" | jq -r '.verification.dev.passed // 0')
        dev_failed=$(echo "$completion_data" | jq -r '.verification.dev.failed // 0')
        dev_total_passed=$((dev_total_passed + dev_passed))
        dev_total_failed=$((dev_total_failed + dev_failed))

        local dev_result
        dev_result=$(jq -n \
            --arg service "$dev_name" \
            --argjson passed "$dev_passed" \
            --argjson failed "$dev_failed" \
            '{service: $service, passed: $passed, failed: $failed}')
        dev_verify_results=$(echo "$dev_verify_results" | jq --argjson r "$dev_result" '. + [$r]')

        i=$((i + 1))
    done

    # Create dev_verify.json
    local dev_verify_json
    dev_verify_json=$(jq -n \
        --arg session "$session_id" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson passed "$dev_total_passed" \
        --argjson failed "$dev_total_failed" \
        --argjson results "$dev_verify_results" \
        --argjson service_count "$count" \
        '{
            session_id: $session,
            timestamp: $ts,
            passed: $passed,
            failed: $failed,
            service_count: $service_count,
            results: $results,
            aggregated_from: "bootstrap"
        }')
    echo "$dev_verify_json" > "${ZCP_TMP_DIR:-/tmp}/dev_verify.json"

    # Aggregate stage verification results
    local stage_verify_results='[]'
    local stage_total_passed=0
    local stage_total_failed=0
    i=0
    while [ "$i" -lt "$count" ]; do
        local dev_name stage_name
        dev_name=$(echo "$handoffs" | jq -r ".[$i].dev_hostname")
        stage_name=$(echo "$handoffs" | jq -r ".[$i].stage_hostname")
        local completion_data
        completion_data=$(read_completion_data "$dev_name")

        local stage_passed stage_failed
        stage_passed=$(echo "$completion_data" | jq -r '.verification.stage.passed // 0')
        stage_failed=$(echo "$completion_data" | jq -r '.verification.stage.failed // 0')
        stage_total_passed=$((stage_total_passed + stage_passed))
        stage_total_failed=$((stage_total_failed + stage_failed))

        local stage_result
        stage_result=$(jq -n \
            --arg service "$stage_name" \
            --argjson passed "$stage_passed" \
            --argjson failed "$stage_failed" \
            '{service: $service, passed: $passed, failed: $failed}')
        stage_verify_results=$(echo "$stage_verify_results" | jq --argjson r "$stage_result" '. + [$r]')

        i=$((i + 1))
    done

    # Create stage_verify.json
    local stage_verify_json
    stage_verify_json=$(jq -n \
        --arg session "$session_id" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson passed "$stage_total_passed" \
        --argjson failed "$stage_total_failed" \
        --argjson results "$stage_verify_results" \
        --argjson service_count "$count" \
        '{
            session_id: $session,
            timestamp: $ts,
            passed: $passed,
            failed: $failed,
            service_count: $service_count,
            results: $results,
            aggregated_from: "bootstrap"
        }')
    echo "$stage_verify_json" > "${ZCP_TMP_DIR:-/tmp}/stage_verify.json"

    # Create deploy_evidence.json
    local deploy_evidence
    deploy_evidence=$(jq -n \
        --arg session "$session_id" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson service_count "$count" \
        --argjson services "$services" \
        '{
            session_id: $session,
            timestamp: $ts,
            status: "deployed",
            service_count: $service_count,
            services: $services,
            aggregated_from: "bootstrap"
        }')
    echo "$deploy_evidence" > "${ZCP_TMP_DIR:-/tmp}/deploy_evidence.json"

    # Persist evidence files to state directory
    if [ -n "${STATE_DIR:-}" ]; then
        cp "${ZCP_TMP_DIR:-/tmp}/dev_verify.json" "$STATE_DIR/workflow/evidence/" 2>/dev/null || true
        cp "${ZCP_TMP_DIR:-/tmp}/stage_verify.json" "$STATE_DIR/workflow/evidence/" 2>/dev/null || true
        cp "${ZCP_TMP_DIR:-/tmp}/deploy_evidence.json" "$STATE_DIR/workflow/evidence/" 2>/dev/null || true
    fi

    # Create bootstrap_complete.json
    local complete_data
    complete_data=$(jq -n \
        --arg session "$session_id" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --argjson count "$count" \
        '{
            session_id: $session,
            completed_at: $ts,
            status: "completed",
            services_bootstrapped: $count
        }')

    echo "$complete_data" > "${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"

    # Also persist to bootstrap state (BOOTSTRAP_STATE_DIR is set by state.sh sourced from bootstrap.sh)
    if [ -n "${BOOTSTRAP_STATE_DIR:-}" ]; then
        mkdir -p "$BOOTSTRAP_STATE_DIR" 2>/dev/null || true
        cp "${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json" "$BOOTSTRAP_STATE_DIR/complete.json" 2>/dev/null || true
    fi

    # Set workflow state via unified API - DONE phase (bootstrap is complete, use iterate for new work)
    set_mode "full"
    set_phase "DONE"

    # Build next_steps guidance - user should use iterate to start new work
    local next_steps
    next_steps=$(jq -n \
        '[
            "⛔ STOP! Before doing ANY work, you MUST run the iterate command first.",
            "MANDATORY: .zcp/workflow.sh iterate \"description of what you want to build\"",
            "This is NOT optional - the workflow will NOT track your progress without it.",
            "Do NOT skip this step. Do NOT start editing files. Run iterate FIRST."
        ]')

    # Record step completion
    # CRITICAL: required_action tells agents what they MUST do before any other work
    local data
    data=$(jq -n \
        --argjson count "$count" \
        --argjson discovery "$discovery" \
        --argjson results "$results" \
        --argjson next_steps "$next_steps" \
        --argjson dev_passed "$dev_total_passed" \
        --argjson dev_failed "$dev_total_failed" \
        --argjson stage_passed "$stage_total_passed" \
        --argjson stage_failed "$stage_total_failed" \
        '{
            all_complete: true,
            services_count: $count,
            discovery: $discovery,
            results: $results,
            workflow_phase: "DONE",
            workflow_mode: "full",
            evidence_created: {
                dev_verify: {passed: $dev_passed, failed: $dev_failed},
                stage_verify: {passed: $stage_passed, failed: $stage_failed},
                deploy_evidence: true
            },
            required_action: {
                command: ".zcp/workflow.sh iterate \"<task description>\"",
                blocking: true,
                reason: "Workflow will NOT track progress without running iterate first",
                must_run_before: ["editing files", "writing code", "making any changes"]
            },
            next_steps: $next_steps
        }')

    # Build msg with VERY clear guidance for using iterate - agents must not skip this
    # Put REQUIRED ACTION first so it's the first thing seen when summarizing
    local msg
    msg="⛔⛔⛔ REQUIRED ACTION - READ THIS FIRST ⛔⛔⛔

When the user gives you a task, your FIRST action MUST be:

  .zcp/workflow.sh iterate \"<the task they described>\"

Do NOT start editing files or writing code until you run iterate.
This is blocking - the workflow will not track your work otherwise.

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✅ Bootstrap complete — $count service pair(s) deployed and verified.

Infrastructure is ready. Services have status pages deployed.

TO START YOUR FIRST REAL TASK:
  1. User tells you what to build
  2. YOU run: .zcp/workflow.sh iterate \"what they told you\"
  3. THEN you can start implementing

Example flow:
  User: \"Build the Starfield visualization\"
  You:  .zcp/workflow.sh iterate \"Build the Starfield visualization\"
  Then: Start implementing..."

    json_response "aggregate-results" "$msg" "$data" "null"
}

export -f step_aggregate_results
