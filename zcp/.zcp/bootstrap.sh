#!/usr/bin/env bash
# .zcp/bootstrap.sh
# Bootstrap command dispatcher - Approach E: Hybrid Chain + Recovery
#
# Commands:
#   step <name>   Execute a specific workflow step (with gate validation)
#   resume        Show current state and next command
#   status        Full diagnostic output (human use)
#   init [args]   Initialize bootstrap workflow
#   reset         Clear all bootstrap state

set -euo pipefail

# Secure default umask
umask 077

# Signal handlers for cleanup
cleanup() {
    local exit_code=$?
    rm -f "${ZCP_TMP_DIR:-/tmp}"/*.tmp.$$ 2>/dev/null
    exit $exit_code
}
trap cleanup EXIT
trap 'trap - EXIT; cleanup; exit 130' INT
trap 'trap - EXIT; cleanup; exit 143' TERM

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Export for use by sourced scripts
# CRITICAL: Export SCRIPT_DIR too - state.sh uses it for BOOTSTRAP_STATE_DIR fallback
export SCRIPT_DIR
export ZCP_SCRIPT_DIR="$SCRIPT_DIR"
export ZCP_STATE_DIR="$SCRIPT_DIR/state"
export STATE_DIR="$SCRIPT_DIR/state"

# Source utilities
source "$SCRIPT_DIR/lib/utils.sh"

# Source bootstrap modules
source "$SCRIPT_DIR/lib/bootstrap/output.sh"
source "$SCRIPT_DIR/lib/unified-state.sh"
source "$SCRIPT_DIR/lib/bootstrap/detect.sh"
source "$SCRIPT_DIR/lib/bootstrap/import-gen.sh"

# State is now managed via unified STATE_FILE in state.sh

# =============================================================================
# STEP DEFINITIONS (single source of truth)
# =============================================================================
BOOTSTRAP_STEPS=(
    "plan"
    "recipe-search"
    "generate-import"
    "import-services"
    "wait-services"
    "mount-dev"
    "discover-services"
    "finalize"
    "spawn-subagents"
    "aggregate-results"
)

# =============================================================================
# HELPER FUNCTIONS
# =============================================================================

# Check if step name is valid
is_valid_step() {
    local step_name="$1"
    for step in "${BOOTSTRAP_STEPS[@]}"; do
        [[ "$step" == "$step_name" ]] && return 0
    done
    return 1
}

# Get step index (0-based)
get_step_index() {
    local step_name="$1"
    local i=0
    for step in "${BOOTSTRAP_STEPS[@]}"; do
        [[ "$step" == "$step_name" ]] && echo "$i" && return
        ((i++))
    done
    echo "-1"
}

# Get step by index
get_step_by_index() {
    local index="$1"
    if [[ $index -ge 0 ]] && [[ $index -lt ${#BOOTSTRAP_STEPS[@]} ]]; then
        echo "${BOOTSTRAP_STEPS[$index]}"
    fi
}

# =============================================================================
# COMMAND: step <name>
# Execute a specific step with gate validation and idempotency
# =============================================================================
cmd_step() {
    local step_name="${1:-}"
    shift || true

    if [[ -z "$step_name" ]]; then
        echo "Error: Step name required"
        echo "Usage: .zcp/bootstrap.sh step <name>"
        exit 1
    fi

    # Validate step exists
    if ! is_valid_step "$step_name"; then
        echo ""
        echo "✗ Unknown step: ${step_name}"
        echo ""
        emit_resume
        exit 1
    fi

    # Ensure workflow state exists
    if ! bootstrap_active; then
        echo ""
        echo "✗ No active workflow"
        echo ""
        echo "Start → .zcp/bootstrap.sh init"
        echo ""
        exit 1
    fi

    # Gate validation: previous step must be complete
    local step_index
    step_index=$(get_step_index "$step_name")

    if [[ $step_index -gt 0 ]]; then
        local prev_step
        prev_step=$(get_step_by_index $((step_index - 1)))

        if [[ -n "$prev_step" ]] && ! is_step_complete "$prev_step"; then
            emit_gate_error "$step_name" "$prev_step"
            exit 1
        fi
    fi

    # Idempotency: if already complete, just show next step
    if is_step_complete "$step_name"; then
        emit_already_complete "$step_name"
        exit 0
    fi

    # Mark step as in_progress
    set_step_status "$step_name" "in_progress"

    # Execute the step
    local step_script="$SCRIPT_DIR/lib/bootstrap/steps/${step_name}.sh"

    if [[ ! -f "$step_script" ]]; then
        set_step_status "$step_name" "failed"
        emit_error "$step_name" "Step implementation not found" "Check ${step_script} exists"
        exit 1
    fi

    source "$step_script"

    # Step functions use legacy naming: step_<name>
    local func_name="step_${step_name//-/_}"

    if ! type "$func_name" &>/dev/null; then
        set_step_status "$step_name" "failed"
        emit_error "$step_name" "Step function not found: $func_name" "Check step implementation"
        exit 1
    fi

    # Capture step output (steps still output JSON for data, we convert to text)
    local step_output
    local step_exit_code=0

    step_output=$("$func_name" "$@" 2>&1) || step_exit_code=$?

    if [[ $step_exit_code -eq 0 ]]; then
        # Validate JSON output from step
        if ! echo "$step_output" | jq -e . >/dev/null 2>&1; then
            # Output is not valid JSON - treat as plain text success
            complete_step "$step_name" "{}"
            emit_success "$step_name"
            return 0
        fi

        # Check if step output indicates success
        local status
        status=$(echo "$step_output" | jq -r '.status // "complete"' 2>/dev/null || echo "complete")

        if [[ "$status" == "complete" ]]; then
            complete_step "$step_name" "$(echo "$step_output" | jq '.data // {}' 2>/dev/null || echo '{}')"

            # Special handling for spawn-subagents: output the instructions
            if [[ "$step_name" == "spawn-subagents" ]]; then
                emit_spawn_instructions "$step_output"
            else
                emit_success "$step_name"
            fi
        elif [[ "$status" == "needs_action" ]]; then
            complete_step "$step_name" "$(echo "$step_output" | jq '.data // {}' 2>/dev/null || echo '{}')"
            emit_needs_action "$step_name" "$step_output"
        elif [[ "$status" == "in_progress" ]]; then
            # Step needs to be re-run (e.g., wait-services polling)
            local message
            message=$(echo "$step_output" | jq -r '.message // "In progress"' 2>/dev/null)
            echo ""
            echo "⟳ ${step_name}: ${message}"
            echo ""
            echo "Run → .zcp/bootstrap.sh step ${step_name}"
            echo ""
        else
            # Step failed
            set_step_status "$step_name" "failed"
            local error_msg
            error_msg=$(echo "$step_output" | jq -r '.message // .data.error // "Step failed"' 2>/dev/null)
            emit_error "$step_name" "$error_msg" "Check logs and resolve the issue"
            exit 1
        fi
    else
        set_step_status "$step_name" "failed"
        emit_error "$step_name" "Step execution failed (exit code: ${step_exit_code})" "Check logs and resolve the issue"
        exit 1
    fi
}

# =============================================================================
# COMMAND: resume
# Show current state and provide next command
# =============================================================================
cmd_resume() {
    emit_resume
}

# =============================================================================
# COMMAND: status
# Full diagnostic output for humans
# =============================================================================
cmd_status() {
    if ! bootstrap_active; then
        echo "No active workflow."
        echo ""
        echo "Start with: .zcp/bootstrap.sh init --runtime <type>"
        exit 0
    fi

    echo "=== Workflow Status ==="
    echo ""
    get_state | jq -r '
        "Workflow ID: \(.workflow_id)",
        "Started: \(.started_at)",
        "Progress: \(.current_step)/\(.steps | length)",
        "",
        "Steps:",
        (.steps[] | "  [\(.status | if . == "complete" then "✓" elif . == "in_progress" then "→" elif . == "failed" then "✗" else " " end)] \(.name)")
    '
}

# =============================================================================
# COMMAND: init
# Initialize bootstrap workflow
# =============================================================================
cmd_init() {
    local runtime="" services="" prefix="app" ha_mode="false"

    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --runtime) runtime="$2"; shift 2 ;;
            --services) services="$2"; shift 2 ;;
            --prefix) prefix="$2"; shift 2 ;;
            --ha) ha_mode="true"; shift ;;
            -h|--help) show_init_help; return 0 ;;
            *) echo "Unknown option: $1" >&2; return 1 ;;
        esac
    done

    # Validate runtime required
    if [[ -z "$runtime" ]]; then
        echo "ERROR: --runtime required" >&2
        echo "Usage: .zcp/bootstrap.sh init --runtime <types> [--services <types>]" >&2
        exit 1
    fi

    # Check zcli authentication
    local zcli_test_result zcli_exit=0
    zcli_test_result=$(zcli service list -P "$projectId" --format json 2>&1) || zcli_exit=$?

    if [[ $zcli_exit -ne 0 ]]; then
        if echo "$zcli_test_result" | grep -qiE "unauthorized|auth|login|token|403"; then
            cat >&2 <<'ZCLI_AUTH'
zcli is not authenticated. Run:
   zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"
ZCLI_AUTH
            exit 1
        elif [[ -z "$projectId" ]]; then
            echo "projectId is not set. Are you running inside ZCP?" >&2
            exit 1
        fi
    fi

    # Run the plan step with arguments (use array to prevent word splitting)
    # Note: plan.sh calls init_bootstrap() to initialize state
    local -a step_args=("--runtime" "$runtime" "--prefix" "$prefix")
    [[ -n "$services" ]] && step_args+=("--services" "$services")
    [[ "$ha_mode" == "true" ]] && step_args+=("--ha")

    # Source and run plan step
    source "$SCRIPT_DIR/lib/bootstrap/steps/plan.sh"
    set_step_status "plan" "in_progress"

    local plan_output
    local plan_exit=0
    plan_output=$(step_plan "${step_args[@]}" 2>&1) || plan_exit=$?

    if [[ $plan_exit -eq 0 ]]; then
        local status
        status=$(echo "$plan_output" | jq -r '.status // "complete"' 2>/dev/null || echo "complete")
        if [[ "$status" == "complete" ]]; then
            # plan.sh handles its own state via init_bootstrap() + complete_step()
            echo ""
            echo "✓ Workflow initialized"
            echo ""
            echo "Next → .zcp/bootstrap.sh step recipe-search"
            echo ""
        else
            set_step_status "plan" "failed"
            emit_error "plan" "Plan step failed" "Check arguments and retry"
            exit 1
        fi
    else
        set_step_status "plan" "failed"
        emit_error "plan" "Plan step failed (exit: $plan_exit)" "Check arguments and retry"
        exit 1
    fi
}

# =============================================================================
# COMMAND: reset
# Clear all bootstrap state
# =============================================================================
cmd_reset() {
    clear_bootstrap
    echo ""
    echo "✓ Workflow state cleared"
    echo ""
    echo "Start → .zcp/bootstrap.sh init --runtime <type>"
    echo ""
}

# =============================================================================
# COMMAND: done
# Validate bootstrap completion
# =============================================================================
cmd_done() {
    if ! bootstrap_active; then
        emit_error "done" "No bootstrap in progress" "Run: .zcp/bootstrap.sh init --runtime <type>"
        exit 1
    fi

    # Check if all steps are complete
    local all_complete
    all_complete=$(get_state | jq -r '[.steps[].status] | all(. == "complete")')

    if [[ "$all_complete" == "true" ]]; then
        emit_complete
    else
        echo ""
        echo "✗ Workflow not complete"
        echo ""
        emit_resume
        exit 1
    fi
}

# =============================================================================
# HELP
# =============================================================================
show_init_help() {
    cat <<'EOF'
BOOTSTRAP INIT - Initialize workflow

USAGE:
    .zcp/bootstrap.sh init --runtime <type> [--services <list>] [--prefix <name>]

OPTIONS:
    --runtime <type>     Runtime type(s), comma-separated
    --services <list>    Managed services, comma-separated
    --prefix <name>      Hostname prefix: app → appdev/appstage
    --ha                 Use HA mode for managed services
EOF
}

show_help() {
    cat <<'EOF'
.zcp/bootstrap.sh - Bootstrap workflow dispatcher

USAGE:
    .zcp/bootstrap.sh <command> [args]

COMMANDS:
    step <name>         Execute a specific workflow step
    resume              Show current state and next command
    status              Full diagnostic output
    init [args]         Initialize bootstrap workflow
    reset               Clear all bootstrap state
    done                Validate completion
    mark-complete <h>   Mark a subagent's service as complete
    wait-subagents      Wait for all spawned subagents to finish

WORKFLOW:
    After init, follow the commands shown in output.
    Each step outputs the exact next command to run.
    If confused, run: .zcp/bootstrap.sh resume
EOF
}

# =============================================================================
# MAIN DISPATCHER
# =============================================================================
main() {
    local command="${1:-}"
    shift 2>/dev/null || true

    case "$command" in
        step)
            cmd_step "$@"
            ;;
        resume)
            cmd_resume
            ;;
        status)
            cmd_status "$@"
            ;;
        init)
            cmd_init "$@"
            ;;
        reset)
            cmd_reset
            ;;
        done)
            cmd_done
            ;;
        mark-complete)
            local hostname="${1:-}"
            if [[ -z "$hostname" ]]; then
                echo "Error: hostname required" >&2
                echo "Usage: .zcp/bootstrap.sh mark-complete <hostname>" >&2
                exit 1
            fi
            exec "$SCRIPT_DIR/mark-complete.sh" "$hostname"
            ;;
        wait-subagents)
            exec "$SCRIPT_DIR/wait-for-subagents.sh" "$@"
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
