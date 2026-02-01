#!/bin/bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # Variables used by sourced scripts

# Zerops Workflow Management System
# Self-documenting phase orchestration with enforcement gates
#
# Structure:
#   workflow.sh          - Main entry point and command router
#   lib/utils.sh         - State management and utility functions
#   lib/help.sh          - Help system loader
#   lib/help/full.sh     - Full platform reference
#   lib/help/topics.sh   - Topic-specific help functions
#   lib/commands.sh      - Command loader
#   lib/commands/init.sh       - init, quick commands
#   lib/commands/transition.sh - transition_to, phase guidance
#   lib/commands/discovery.sh  - create_discovery, refresh_discovery
#   lib/commands/status.sh     - show, complete, reset
#   lib/commands/extend.sh     - extend, upgrade-to-full, record_deployment
#   lib/gates.sh         - Phase transition gate checks

set -o pipefail

# HIGH-5: Secure default umask
umask 077

# HIGH-4: Signal handlers for cleanup
cleanup() {
    local exit_code=$?
    # Clean up any temp files created by this script
    rm -f "${ZCP_TMP_DIR:-/tmp}"/*.tmp.$$ 2>/dev/null
    exit $exit_code
}
trap cleanup EXIT
trap 'trap - EXIT; cleanup; exit 130' INT
trap 'trap - EXIT; cleanup; exit 143' TERM

# Get script directory for sourcing modules
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source all modules
source "$SCRIPT_DIR/lib/utils.sh"
source "$SCRIPT_DIR/lib/help.sh"
source "$SCRIPT_DIR/lib/gates.sh"
source "$SCRIPT_DIR/lib/commands.sh"
source "$SCRIPT_DIR/lib/state.sh"

# ============================================================================
# MAIN
# ============================================================================

main() {
    local command="$1"
    shift

    # Initialize persistent storage on startup
    init_persistent_storage 2>/dev/null

    # Check for required dependencies
    if ! command -v jq &>/dev/null; then
        echo "Error: jq is required but not installed" >&2
        echo "Install with: brew install jq (macOS) or apt install jq (Linux)" >&2
        exit 1
    fi

    # Check zcli authentication for commands that need it
    # Skip for: --help, "", reset (state-only ops)
    case "$command" in
        --help|""|reset)
            # These commands don't need zcli auth
            ;;
        *)
            # All other commands may need zcli - check auth
            if ! command -v zcli &>/dev/null; then
                echo "❌ zcli not found" >&2
                echo "   Install: https://docs.zerops.io/references/cli" >&2
                exit 1
            fi

            # Quick auth check - try a lightweight command
            local zcli_test
            zcli_test=$(zcli project list 2>&1)
            local zcli_exit=$?

            if [ $zcli_exit -ne 0 ] && echo "$zcli_test" | grep -qiE "unauthorized|unauthenticated|auth|login|token|403|not logged"; then
                cat <<'ZCLI_AUTH_ERROR'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔐 ZCLI NOT AUTHENTICATED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Run this first:

   zcli login --region=gomibako \
       --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' \
       "$ZEROPS_ZCP_API_KEY"

Then re-run: .zcp/workflow.sh show
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
ZCLI_AUTH_ERROR
                exit 1
            fi
            ;;
    esac

    case "$command" in
        init)
            # Gate B: Validate bootstrap if applicable
            if [[ "$(check_bootstrap_mode 2>/dev/null)" == "true" ]]; then
                if ! check_gate_bootstrap_to_workflow; then
                    echo ""
                    echo "❌ Bootstrap validation failed"
                    exit 1
                fi
            fi
            cmd_init "$@"
            ;;
        --quick)
            cmd_quick
            ;;
        --help)
            if [ -z "$1" ]; then
                show_full_help
            else
                show_topic_help "$1"
            fi
            ;;
        transition_to)
            cmd_transition_to "$@"
            ;;
        create_discovery)
            cmd_create_discovery "$@"
            ;;
        create_discovery_multi)
            cmd_create_discovery_multi "$@"
            ;;
        detect_shared_database)
            cmd_detect_shared_database
            ;;
        show)
            cmd_show "$@"
            ;;
        recover)
            cmd_recover "$@"
            ;;
        state)
            cmd_state
            ;;
        context)
            cmd_context
            ;;
        complete)
            cmd_complete "$@"
            ;;
        reset)
            cmd_reset "$@"
            ;;
        record_deployment)
            cmd_record_deployment "$@"
            ;;
        extend)
            cmd_extend "$@"
            ;;
        # === BOOTSTRAP COMMANDS (agent-orchestrated architecture) ===
        bootstrap)
            exec "$SCRIPT_DIR/bootstrap.sh" init "$@"
            ;;
        bootstrap-done)
            exec "$SCRIPT_DIR/bootstrap.sh" done "$@"
            ;;
        validate_config|validate_code)
            "$SCRIPT_DIR/validate-config.sh" "$@"
            ;;
        refresh_discovery)
            cmd_refresh_discovery "$@"
            ;;
        upgrade-to-full)
            cmd_upgrade_to_full
            ;;
        # === CONTINUITY COMMANDS ===
        iterate)
            cmd_iterate "$@"
            ;;
        retarget)
            cmd_retarget "$@"
            ;;
        intent)
            cmd_intent "$@"
            ;;
        note)
            cmd_note "$@"
            ;;
        "")
            cat <<'EOF'
╔══════════════════════════════════════════════════════════════════╗
║  ZEROPS WORKFLOW                                                 ║
╚══════════════════════════════════════════════════════════════════╝

Will this work be deployed (now or later)?

┌─────────────────────────────────────────────────────────────────┐
│  YES  →  .zcp/workflow.sh init                                  │
│          Features, bug fixes to ship, config changes,           │
│          schema changes, new files/directories                  │
│                                                                 │
│          Enforced phases with gates that catch mistakes         │
│          You can stop at any phase and resume later             │
├─────────────────────────────────────────────────────────────────┤
│  NO   →  .zcp/workflow.sh --quick                               │
│          Debugging, exploration, checking logs/state,           │
│          reading/understanding code, temporary tests            │
│                                                                 │
│          No enforcement, no evidence required                   │
└─────────────────────────────────────────────────────────────────┘

Other modes:
  .zcp/workflow.sh init --dev-only   # Dev iteration without deployment
  .zcp/workflow.sh init --hotfix     # Skip dev verification (use recent discovery)

New project? Bootstrap first:
  .zcp/workflow.sh bootstrap --runtime <types> --services <types>
  Use user's exact words. Run: .zcp/workflow.sh show (lists valid types)

Commands:
  .zcp/workflow.sh show              # Current status
  .zcp/workflow.sh bootstrap --help  # Bootstrap help
  .zcp/workflow.sh --help            # Full reference
  .zcp/workflow.sh --help {topic}    # Topic help (discover, develop, deploy, verify, done, vars, services, extend, bootstrap)
EOF
            ;;
        *)
            echo "❌ Unknown command: $command"
            echo ""
            echo "Usage: .zcp/workflow.sh {command}"
            echo ""
            echo "Commands:"
            echo "  init [--dev-only|--hotfix]  Start workflow session"
            echo "  --quick                     Quick mode (no enforcement)"
            echo "  --help [topic]              Show help"
            echo "  transition_to [--back] {phase}  Move to phase"
            echo "  create_discovery [--single] ...  Record services"
            echo "  show [--guidance|--full]    Current status"
            echo "  recover                     Full context recovery"
            echo "  context                     Exportable context dump (FIX-05)"
            echo "  state                       One-line state summary"
            echo "  complete                    Verify and finish"
            echo "  reset [--keep-discovery]    Clear state"
            echo "  extend {file.yml}           Add services"
            echo "  refresh_discovery           Validate discovery"
            echo "  upgrade-to-full             Upgrade dev-only to full"
            echo "  record_deployment {svc}     Manual deploy evidence"
            echo ""
            echo "Bootstrap Commands:"
            echo "  bootstrap --runtime <rt> [--services <s>] [--prefix <p>]  Initialize bootstrap"
            echo "  .zcp/bootstrap.sh step <name>   Run individual step"
            echo "  .zcp/bootstrap.sh resume        Get next step to run"
            echo "  bootstrap-done              Mark bootstrap complete (unlocks workflow)"
            echo ""
            echo "Continuity Commands:"
            echo "  iterate [--service {name}] [--to PHASE] [summary]  Start new iteration"
            echo "  retarget {dev|stage} {id} {name}  Change deployment target"
            echo "  intent [text]               Set/show workflow intent"
            echo "  note [text]                 Add/show workflow notes"
            return 1
            ;;
    esac
}

main "$@"
