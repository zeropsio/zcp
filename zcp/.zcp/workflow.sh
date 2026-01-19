#!/bin/bash

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

# Get script directory for sourcing modules
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source all modules
source "$SCRIPT_DIR/lib/utils.sh"
source "$SCRIPT_DIR/lib/help.sh"
source "$SCRIPT_DIR/lib/gates.sh"
source "$SCRIPT_DIR/lib/commands.sh"

# ============================================================================
# MAIN
# ============================================================================

main() {
    local command="$1"
    shift

    case "$command" in
        init)
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
        show)
            cmd_show
            ;;
        complete)
            cmd_complete
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
        refresh_discovery)
            cmd_refresh_discovery "$@"
            ;;
        upgrade-to-full)
            cmd_upgrade_to_full
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

Commands:
  .zcp/workflow.sh show              # Current status
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
            echo "  show                        Current status"
            echo "  complete                    Verify and finish"
            echo "  reset [--keep-discovery]    Clear state"
            echo "  extend {file.yml}           Add services"
            echo "  refresh_discovery           Validate discovery"
            echo "  upgrade-to-full             Upgrade dev-only to full"
            echo "  record_deployment {svc}     Manual deploy evidence"
            return 1
            ;;
    esac
}

main "$@"
