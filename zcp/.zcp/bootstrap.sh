#!/usr/bin/env bash
# .zcp/bootstrap.sh
# Bootstrap command dispatcher - agent-orchestrated architecture
#
# Commands:
#   step <name> [args]   - Run individual step (JSON response)
#   status [--services]  - Show bootstrap status (JSON)
#   reset                - Clear all bootstrap state
#   resume               - Get next step to execute
#   orchestrate [args]   - Run full bootstrap (called by workflow.sh bootstrap)
#   done                 - Validate completion (called by workflow.sh bootstrap-done)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source utilities (for ZCP_TMP_DIR, STATE_DIR, colors, etc.)
source "$SCRIPT_DIR/lib/utils.sh"

# Source bootstrap modules
source "$SCRIPT_DIR/lib/bootstrap/output.sh"
source "$SCRIPT_DIR/lib/bootstrap/state.sh"
source "$SCRIPT_DIR/lib/bootstrap/detect.sh"
source "$SCRIPT_DIR/lib/bootstrap/import-gen.sh"

# Initialize state directory
init_bootstrap_state

# Step definitions and their order
BOOTSTRAP_STEPS=(
    "plan"
    "recipe-search"
    "generate-import"
    "import-services"
    "wait-services"
    "mount-dev"
    "finalize"
)

# Get next step after a given step
get_next_step() {
    local current="$1"
    local found=false

    for step in "${BOOTSTRAP_STEPS[@]}"; do
        if [ "$found" = true ]; then
            echo "$step"
            return
        fi
        if [ "$step" = "$current" ]; then
            found=true
        fi
    done

    echo "null"
}

# Run a specific step
run_step() {
    local step_name="$1"
    shift

    local step_script="$SCRIPT_DIR/lib/bootstrap/steps/${step_name}.sh"

    if [ ! -f "$step_script" ]; then
        json_error "$step_name" "Step not found: $step_name" '{}' '["Check step name spelling", "Run: .zcp/bootstrap.sh --help"]'
        exit 1
    fi

    source "$step_script"

    # Each step script defines a step_<name> function (with - replaced by _)
    local func_name="step_${step_name//-/_}"

    if ! type "$func_name" &>/dev/null; then
        json_error "$step_name" "Step function not found: $func_name" '{}' '[]'
        exit 1
    fi

    "$func_name" "$@"
}

# Show bootstrap status
cmd_status() {
    local show_services=false

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --services) show_services=true; shift ;;
            *) shift ;;
        esac
    done

    generate_status_json
}

# Reset bootstrap state
cmd_reset() {
    clear_bootstrap_state
    echo '{"status": "reset", "message": "Bootstrap state cleared"}'
}

# Get next step to run (for resume)
cmd_resume() {
    local state
    state=$(get_bootstrap_state)

    if [ "$state" = '{}' ]; then
        jq -n '{
            status: "no_bootstrap",
            next: "plan",
            message: "No bootstrap in progress - start with plan step"
        }'
        return
    fi

    local checkpoint
    checkpoint=$(echo "$state" | jq -r '.checkpoint // "none"')

    # Find next incomplete step
    for step in "${BOOTSTRAP_STEPS[@]}"; do
        if ! is_step_complete "$step"; then
            jq -n \
                --arg step "$step" \
                --arg checkpoint "$checkpoint" \
                '{
                    status: "resume",
                    next: $step,
                    checkpoint: $checkpoint,
                    message: "Resume from step: \($step)"
                }'
            return
        fi
    done

    # All steps complete
    jq -n '{
        status: "complete",
        next: null,
        message: "All infrastructure steps complete - ready for code generation"
    }'
}

# Orchestrate full bootstrap (sequential steps with progress output)
cmd_orchestrate() {
    local runtime="" services="" prefix="app" ha_mode="false"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --runtime) runtime="$2"; shift 2 ;;
            --services) services="$2"; shift 2 ;;
            --prefix) prefix="$2"; shift 2 ;;
            --ha) ha_mode="true"; shift ;;
            -h|--help) show_orchestrate_help; return 0 ;;
            *) echo "Unknown option: $1" >&2; return 1 ;;
        esac
    done

    # Require runtime
    if [ -z "$runtime" ]; then
        echo "ERROR: --runtime required" >&2
        echo "Usage: .zcp/bootstrap.sh orchestrate --runtime go --services postgresql" >&2
        exit 1
    fi

    # Check zcli authentication
    local zcli_test_result zcli_exit
    zcli_test_result=$(zcli service list -P "$projectId" --format json 2>&1) || zcli_exit=$?

    if [ "${zcli_exit:-0}" -ne 0 ]; then
        if echo "$zcli_test_result" | grep -qiE "unauthorized|auth|login|token|403"; then
            cat >&2 <<'ZCLI_AUTH'
zcli is not authenticated. Run this first:

   zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"

Then re-run:

   .zcp/workflow.sh bootstrap --runtime go --services postgresql,valkey
ZCLI_AUTH
            exit 1
        elif [ -z "$projectId" ]; then
            echo "projectId is not set. Are you running inside ZCP?" >&2
            exit 1
        fi
    fi

    # Check project state
    local project_state
    project_state=$(detect_project_state)

    case "$project_state" in
        CONFORMANT)
            echo "Project already has dev/stage pairs."
            echo "Use standard workflow: .zcp/workflow.sh init"
            return 0
            ;;
        NON_CONFORMANT)
            echo "WARNING: Project has services but no dev/stage pairs."
            echo "Bootstrap will add new runtime pair alongside existing services."
            ;;
        FRESH)
            echo "Fresh project detected. Starting full bootstrap."
            ;;
        ERROR)
            echo "ERROR: Could not detect project state" >&2
            exit 1
            ;;
    esac

    echo ""
    echo "=========================================="
    echo "  BOOTSTRAP: Agent-Orchestrated Mode"
    echo "=========================================="
    echo ""
    echo "Runtime: $runtime"
    echo "Services: ${services:-none}"
    echo "Prefix: $prefix"
    echo ""

    # Run steps sequentially
    local step_args

    # Step 1: Plan
    echo "--- Step 1/7: Plan ---"
    step_args="--runtime $runtime --prefix $prefix"
    [ -n "$services" ] && step_args="$step_args --services $services"
    [ "$ha_mode" = "true" ] && step_args="$step_args --ha"
    run_step "plan" $step_args
    echo ""

    # Step 2: Recipe Search
    echo "--- Step 2/7: Recipe Search ---"
    run_step "recipe-search"
    echo ""

    # Step 3: Generate Import
    echo "--- Step 3/7: Generate Import ---"
    run_step "generate-import"
    echo ""

    # Step 4: Import Services
    echo "--- Step 4/7: Import Services ---"
    run_step "import-services"
    echo ""

    # Step 5: Wait Services
    echo "--- Step 5/7: Wait Services ---"
    local wait_result wait_status
    while true; do
        wait_result=$(run_step "wait-services")
        wait_status=$(echo "$wait_result" | jq -r '.status')
        echo "$wait_result" | jq -r '.message'

        if [ "$wait_status" = "complete" ]; then
            break
        elif [ "$wait_status" = "failed" ]; then
            echo "ERROR: Services failed to start" >&2
            echo "$wait_result"
            exit 1
        fi

        sleep 10
    done
    echo ""

    # Step 6: Mount Dev (for each dev hostname)
    echo "--- Step 6/7: Mount Dev ---"
    local plan_data dev_hostnames
    plan_data=$(get_plan)
    dev_hostnames=$(echo "$plan_data" | jq -r '.dev_hostnames[]' 2>/dev/null || echo "$plan_data" | jq -r '.dev_hostname')

    for hostname in $dev_hostnames; do
        echo "Mounting $hostname..."
        run_step "mount-dev" "$hostname"
    done
    echo ""

    # Step 7: Finalize
    echo "--- Step 7/7: Finalize ---"
    local finalize_result
    finalize_result=$(run_step "finalize")
    echo ""

    # Output handoff information
    echo "=========================================="
    echo "  INFRASTRUCTURE COMPLETE"
    echo "=========================================="
    echo ""
    echo "Service handoffs for code generation:"
    echo "$finalize_result" | jq -r '.data.service_handoffs[] | "  - \(.dev_hostname): \(.mount_path)"'
    echo ""
    echo "Next: Agent should spawn subagents for code generation"
    echo "      or handle directly for single-service bootstraps."
    echo ""
    echo "When all code is deployed, run:"
    echo "  .zcp/workflow.sh bootstrap-done"
    echo ""

    # Output the finalize result for agent parsing
    echo "$finalize_result"
}

# Validate bootstrap completion
cmd_done() {
    local state
    state=$(get_bootstrap_state)

    if [ "$state" = '{}' ]; then
        json_error "done" "No bootstrap in progress" '{}' '["Run: .zcp/workflow.sh bootstrap --runtime <type>"]'
        exit 1
    fi

    local plan
    plan=$(echo "$state" | jq '.plan')

    # Get dev hostnames from plan
    local dev_hostnames
    dev_hostnames=$(echo "$plan" | jq -r '.dev_hostnames[]' 2>/dev/null || echo "$plan" | jq -r '.dev_hostname')

    echo "Verifying bootstrap completion..."
    echo ""

    local all_passed=true
    local checks=()

    for hostname in $dev_hostnames; do
        local mount_path="/var/www/$hostname"

        # Check 1: zerops.yml exists
        local zerops_yml="$mount_path/zerops.yml"
        if [ ! -f "$zerops_yml" ]; then
            echo "FAIL: zerops.yml not found: $zerops_yml"
            all_passed=false
            checks+=("zerops.yml missing for $hostname")
        else
            local yml_size
            yml_size=$(wc -c < "$zerops_yml")
            if [ "$yml_size" -lt 100 ]; then
                echo "WARN: zerops.yml looks incomplete ($yml_size bytes): $zerops_yml"
                checks+=("zerops.yml incomplete for $hostname")
            else
                echo "OK: zerops.yml exists ($yml_size bytes)"
            fi
        fi

        # Check 2: Some code exists
        local has_code=false
        for pattern in main.go index.js app.py main.py server.go cmd/main.go src/index.ts; do
            if [ -f "$mount_path/$pattern" ]; then
                has_code=true
                echo "OK: Application code found: $pattern"
                break
            fi
        done

        if [ "$has_code" = false ]; then
            # Check for any source files
            local code_files
            code_files=$(find "$mount_path" -maxdepth 2 -type f \( -name "*.go" -o -name "*.js" -o -name "*.py" -o -name "*.rs" -o -name "*.ts" \) 2>/dev/null | head -3)
            if [ -n "$code_files" ]; then
                echo "OK: Found source files"
            else
                echo "FAIL: No source code found in $mount_path"
                all_passed=false
                checks+=("No code for $hostname")
            fi
        fi
    done

    echo ""

    if [ "$all_passed" = false ]; then
        local checks_json
        checks_json=$(printf '%s\n' "${checks[@]}" | jq -R . | jq -s .)
        json_error "done" "Bootstrap verification failed" "{\"checks\": $checks_json}" '["Complete zerops.yml", "Write application code", "Push and verify deployments"]'
        exit 1
    fi

    # Write completion evidence
    local complete_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"
    jq -n \
        --arg session "$(cat "${ZCP_TMP_DIR:-/tmp}/claude_session" 2>/dev/null || echo "unknown")" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        --argjson plan "$plan" \
        '{
            session_id: $session,
            completed_at: $ts,
            status: "completed",
            plan: $plan
        }' > "$complete_file"

    # Also persist to state
    cp "$complete_file" "$BOOTSTRAP_STATE_DIR/complete.json" 2>/dev/null || true

    echo "=========================================="
    echo "  BOOTSTRAP COMPLETE"
    echo "=========================================="
    echo ""
    echo "For every new development task, run:"
    echo "  .zcp/workflow.sh init"
    echo ""

    json_response "done" "Bootstrap complete" '{"verified": true}'
}

# Help for orchestrate command
show_orchestrate_help() {
    cat <<'EOF'
BOOTSTRAP ORCHESTRATE - Run full bootstrap sequence

USAGE:
    .zcp/bootstrap.sh orchestrate --runtime <type> [--services <list>] [--prefix <name>]

OPTIONS:
    --runtime <type>     Runtime type: go, nodejs, python, php, rust, bun, java, dotnet
    --services <list>    Managed services: postgresql,valkey,elasticsearch (comma-separated)
    --prefix <name>      Hostname prefix (default: app) creates appdev, appstage
    --ha                 Use HA mode for managed services

EXAMPLES:
    .zcp/bootstrap.sh orchestrate --runtime go --services postgresql,valkey
    .zcp/bootstrap.sh orchestrate --runtime nodejs --prefix api

This command runs all bootstrap steps sequentially:
    1. plan           - Create bootstrap plan
    2. recipe-search  - Fetch runtime patterns
    3. generate-import - Generate import.yml
    4. import-services - Import services via zcli
    5. wait-services   - Wait for services to be RUNNING
    6. mount-dev       - Mount dev service(s) via SSHFS
    7. finalize        - Create handoff data for code generation

After orchestrate completes, agent handles code generation.
EOF
}

# Main help
show_help() {
    cat <<'EOF'
.zcp/bootstrap.sh - Bootstrap command dispatcher

USAGE:
    .zcp/bootstrap.sh <command> [args]

COMMANDS:
    step <name> [args]   Run individual step (returns JSON)
    status [--services]  Show bootstrap status (JSON)
    reset                Clear all bootstrap state
    resume               Get next step to execute
    orchestrate [args]   Run full bootstrap (used by workflow.sh bootstrap)
    done                 Validate completion (used by workflow.sh bootstrap-done)

STEPS:
    plan                 Create bootstrap plan (--runtime, --services, --prefix)
    recipe-search        Fetch runtime patterns from recipe API
    generate-import      Generate import.yml from plan
    import-services      Import services via zcli
    wait-services        Wait for services to reach RUNNING state
    mount-dev <hostname> Mount dev service via SSHFS
    finalize             Create per-service handoffs for code generation

EXAMPLES:
    # Run individual steps (agent-orchestrated)
    .zcp/bootstrap.sh step plan --runtime go --services postgresql --prefix api
    .zcp/bootstrap.sh step recipe-search
    .zcp/bootstrap.sh step mount-dev apidev

    # Check status
    .zcp/bootstrap.sh status

    # Full orchestration (called by workflow.sh)
    .zcp/bootstrap.sh orchestrate --runtime go --services postgresql

    # Mark complete
    .zcp/bootstrap.sh done

Each step returns JSON with this contract:
{
    "status": "complete" | "in_progress" | "failed" | "needs_action",
    "step": "<step_name>",
    "data": { ... step-specific data ... },
    "next": "<suggested_next_step>" | null,
    "message": "Human-readable status"
}
EOF
}

# Main dispatcher
main() {
    local command="${1:-}"
    shift 2>/dev/null || true

    case "$command" in
        step)
            local step_name="${1:-}"
            shift 2>/dev/null || true
            if [ -z "$step_name" ]; then
                echo "ERROR: step name required" >&2
                echo "Usage: .zcp/bootstrap.sh step <name> [args]" >&2
                exit 1
            fi
            run_step "$step_name" "$@"
            ;;
        status)
            cmd_status "$@"
            ;;
        reset)
            cmd_reset
            ;;
        resume)
            cmd_resume
            ;;
        orchestrate)
            cmd_orchestrate "$@"
            ;;
        done)
            cmd_done
            ;;
        -h|--help|help|"")
            show_help
            ;;
        *)
            echo "Unknown command: $command" >&2
            show_help
            exit 1
            ;;
    esac
}

main "$@"
