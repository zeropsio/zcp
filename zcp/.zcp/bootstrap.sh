#!/usr/bin/env bash
# .zcp/bootstrap.sh
# Bootstrap command dispatcher - agent-orchestrated architecture
#
# Commands:
#   init [args]          - Initialize bootstrap (runs plan only, agent continues)
#   step <name> [args]   - Run individual step (JSON response)
#   status [--services]  - Show bootstrap status (JSON)
#   reset                - Clear all bootstrap state
#   resume               - Get next step to execute
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

# Initialize bootstrap (agent-orchestrated mode) - runs only plan step
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
    if [ -z "$runtime" ]; then
        echo "ERROR: --runtime required" >&2
        echo "Usage: .zcp/workflow.sh bootstrap --runtime go --services postgresql" >&2
        exit 1
    fi

    # Check zcli authentication
    local zcli_test_result zcli_exit
    zcli_test_result=$(zcli service list -P "$projectId" --format json 2>&1) || zcli_exit=$?

    if [ "${zcli_exit:-0}" -ne 0 ]; then
        if echo "$zcli_test_result" | grep -qiE "unauthorized|auth|login|token|403"; then
            cat >&2 <<'ZCLI_AUTH'
zcli is not authenticated. Run:
   zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"
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
            echo "Use: .zcp/workflow.sh init"
            return 0
            ;;
        NON_CONFORMANT)
            echo "WARNING: Project has services but no dev/stage pairs."
            ;;
        FRESH)
            echo "Fresh project detected."
            ;;
        ERROR)
            echo "ERROR: Could not detect project state" >&2
            exit 1
            ;;
    esac

    # Run ONLY the plan step
    local step_args="--runtime $runtime --prefix $prefix"
    [ -n "$services" ] && step_args="$step_args --services $services"
    [ "$ha_mode" = "true" ] && step_args="$step_args --ha"

    echo ""
    echo "Creating bootstrap plan..."
    run_step "plan" $step_args

    # Output guidance for agent
    echo ""
    echo "=============================================="
    echo "  BOOTSTRAP INITIALIZED - AGENT CONTINUE"
    echo "=============================================="
    echo ""
    echo "Plan created. Run these steps in order:"
    echo ""
    echo "  1. .zcp/bootstrap.sh step recipe-search"
    echo "  2. .zcp/bootstrap.sh step generate-import"
    echo "  3. .zcp/bootstrap.sh step import-services"
    echo "  4. .zcp/bootstrap.sh step wait-services  # Loop until status=complete"
    echo "  5. .zcp/bootstrap.sh step mount-dev {hostname}"
    echo "  6. .zcp/bootstrap.sh step finalize"
    echo ""
    echo "Or check progress anytime:"
    echo "  .zcp/bootstrap.sh status"
    echo "  .zcp/bootstrap.sh resume  # Returns next step to run"
    echo ""
}

# Help for init command
show_init_help() {
    cat <<'EOF'
BOOTSTRAP INIT - Initialize agent-orchestrated bootstrap

USAGE:
    .zcp/workflow.sh bootstrap --runtime <type> [--services <list>] [--prefix <name>]

OPTIONS:
    --runtime <type>     Runtime type: go, nodejs, python, php, rust, bun, java, dotnet
    --services <list>    Managed services: postgresql,valkey,elasticsearch (comma-separated)
    --prefix <name>      Hostname prefix (default: app) creates appdev, appstage
    --ha                 Use HA mode for managed services

EXAMPLES:
    .zcp/workflow.sh bootstrap --runtime go --services postgresql,valkey
    .zcp/workflow.sh bootstrap --runtime nodejs --prefix api

This command initializes bootstrap and returns immediately with guidance.
Agent then runs steps individually for visibility and error handling:

    .zcp/bootstrap.sh step recipe-search
    .zcp/bootstrap.sh step generate-import
    .zcp/bootstrap.sh step import-services
    .zcp/bootstrap.sh step wait-services   # Poll until complete
    .zcp/bootstrap.sh step mount-dev <hostname>
    .zcp/bootstrap.sh step finalize
EOF
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

# Main help
show_help() {
    cat <<'EOF'
.zcp/bootstrap.sh - Bootstrap command dispatcher

USAGE:
    .zcp/bootstrap.sh <command> [args]

COMMANDS:
    init [args]          Initialize bootstrap, run plan only (default for workflow.sh bootstrap)
    step <name> [args]   Run individual step (returns JSON)
    status [--services]  Show bootstrap status (JSON)
    reset                Clear all bootstrap state
    resume               Get next step to execute
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
    # Initialize bootstrap (via workflow.sh)
    .zcp/workflow.sh bootstrap --runtime go --services postgresql

    # Run individual steps
    .zcp/bootstrap.sh step recipe-search
    .zcp/bootstrap.sh step generate-import
    .zcp/bootstrap.sh step mount-dev apidev

    # Check status
    .zcp/bootstrap.sh status

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
        init)
            cmd_init "$@"
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
