#!/usr/bin/env bash
# .zcp/lib/bootstrap/steps/aggregate-results.sh
# Step: Wait for all subagents to complete, create discovery.json, transition to DEVELOP
#
# This step aggregates results from all spawned subagents by checking their
# per-service state. When all services are complete, it creates the discovery.json
# file and completes the bootstrap process.
#
# FALLBACK DETECTION: If state file is missing but evidence files exist
# (zerops.yml, source code), this step will auto-mark the service complete.
# This handles cases where subagent couldn't write the state file.
#
# Inputs: Per-service state from subagents (via set_service_state or mark-complete.sh)
# Outputs: discovery.json, bootstrap_complete.json, workflow ready for DEVELOP

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

        local data
        data=$(jq -n \
            --argjson complete "$complete" \
            --argjson total "$count" \
            --argjson pending "$pending_json" \
            --argjson results "$results" \
            '{
                complete: $complete,
                total: $total,
                pending: $pending,
                results: $results,
                action: "Wait for subagents to complete, then re-run this step",
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
    session_id=$(cat "${ZCP_TMP_DIR:-/tmp}/claude_session" 2>/dev/null || echo "bootstrap-$(date +%s)")

    # Build services array from all handoffs
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
            --argjson single "$single_mode" \
            '{
                dev: { id: $dev_id, name: $dev_name },
                stage: { id: $stage_id, name: $stage_name },
                runtime: $runtime,
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

    local primary_single_mode="false"
    if [ "$primary_dev_id" = "$primary_stage_id" ]; then
        primary_single_mode="true"
    fi

    # Create discovery.json
    local discovery
    discovery=$(jq -n \
        --arg session "$session_id" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg dev_id "$primary_dev_id" \
        --arg dev_name "$primary_dev_name" \
        --arg stage_id "$primary_stage_id" \
        --arg stage_name "$primary_stage_name" \
        --argjson single "$primary_single_mode" \
        --argjson services "$services" \
        --argjson service_count "$count" \
        '{
            session_id: $session,
            timestamp: $ts,
            single_mode: $single,
            dev: { id: $dev_id, name: $dev_name },
            stage: { id: $stage_id, name: $stage_name },
            services: $services,
            service_count: $service_count
        }')

    echo "$discovery" > "${ZCP_TMP_DIR:-/tmp}/discovery.json"

    # Also persist to state directory (STATE_DIR is set by utils.sh sourced from bootstrap.sh)
    if [ -n "$STATE_DIR" ]; then
        mkdir -p "$STATE_DIR/workflow/evidence" 2>/dev/null || true
        cp "${ZCP_TMP_DIR:-/tmp}/discovery.json" "$STATE_DIR/workflow/evidence/" 2>/dev/null || true
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
    if [ -n "$BOOTSTRAP_STATE_DIR" ]; then
        mkdir -p "$BOOTSTRAP_STATE_DIR" 2>/dev/null || true
        cp "${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json" "$BOOTSTRAP_STATE_DIR/complete.json" 2>/dev/null || true
    fi

    # Set workflow state files
    echo "full" > "${ZCP_TMP_DIR:-/tmp}/claude_mode"
    echo "DEVELOP" > "${ZCP_TMP_DIR:-/tmp}/claude_phase"

    # Build next_steps guidance with correct commands
    local next_steps
    next_steps=$(jq -n \
        --arg dev_name "$primary_dev_name" \
        --arg dev_id "$primary_dev_id" \
        --arg stage_id "$primary_stage_id" \
        '[
            "Edit files directly in /var/www/\($dev_name)/",
            "Run builds: ssh \($dev_name) \"cd /var/www && go build\" (or runtime equivalent)",
            "Deploy to dev: ssh \($dev_name) \"cd /var/www && zcli push \($dev_id) --setup=dev --deploy-git-folder\"",
            "Deploy to stage: ssh \($dev_name) \"cd /var/www && zcli push \($stage_id) --setup=prod\"",
            "Run .zcp/workflow.sh show anytime for guidance"
        ]')

    # Record step completion
    local data
    data=$(jq -n \
        --argjson count "$count" \
        --argjson discovery "$discovery" \
        --argjson results "$results" \
        --argjson next_steps "$next_steps" \
        '{
            all_complete: true,
            services_count: $count,
            discovery: $discovery,
            results: $results,
            workflow_phase: "DEVELOP",
            workflow_mode: "full",
            next_steps: $next_steps
        }')

    record_step "aggregate-results" "complete" "$data"

    local msg
    if [ "$count" -eq 1 ]; then
        msg="Bootstrap complete - 1 service pair ready, workflow in DEVELOP phase"
    else
        msg="Bootstrap complete - $count service pairs ready, workflow in DEVELOP phase"
    fi

    json_response "aggregate-results" "$msg" "$data" "null"
}

export -f step_aggregate_results
