#!/bin/bash
# Zerops Deployment Helper - Single command for foolproof deployment
# Addresses agent failure cascade issue: "Deploy Commands Are Complex"
#
# Usage:
#   .zcp/deploy.sh stage              # Deploy all services to stage
#   .zcp/deploy.sh stage appdev       # Deploy specific service to stage
#   .zcp/deploy.sh dev                # Deploy all services to dev
#   .zcp/deploy.sh --commands         # Show copy-paste commands only
#   .zcp/deploy.sh --help             # Show help

set -o pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DISCOVERY_FILE="${ZCP_TMP_DIR:-/tmp}/discovery.json"

# Source utils for session management
if [ -f "$SCRIPT_DIR/lib/utils.sh" ]; then
    source "$SCRIPT_DIR/lib/utils.sh"
fi

# ============================================================================
# HELP
# ============================================================================

show_help() {
    cat <<'EOF'
.zcp/deploy.sh - Foolproof deployment helper

USAGE:
  .zcp/deploy.sh {target} [service]
  .zcp/deploy.sh --commands [target]
  .zcp/deploy.sh --help

TARGETS:
  stage    Deploy from dev containers to stage services (production)
  dev      Deploy from dev containers back to dev services (refresh)

OPTIONS:
  --commands    Show copy-paste ready commands without executing
  --dry-run     Same as --commands
  --help        Show this help

EXAMPLES:
  .zcp/deploy.sh stage              # Deploy ALL services to stage
  .zcp/deploy.sh stage appdev       # Deploy ONLY appdev -> appstage
  .zcp/deploy.sh --commands stage   # Show commands without executing

REQUIREMENTS:
  - discovery.json must exist (run workflow init first)
  - zcli must be authenticated
  - Source code must be in dev containers at /var/www

HOW IT WORKS:
  1. Reads service IDs from /tmp/discovery.json
  2. Authenticates zcli inside dev containers
  3. Pushes code using git-based deploy (--deploy-git-folder)
  4. Waits for deployment completion
  5. Records evidence for workflow

DEPLOYMENT METHOD:
  Uses: ssh {dev} "cd /var/www && zcli push {id} --setup=prod --deploy-git-folder"

  The --deploy-git-folder flag is preferred because:
  - Works with git-initialized directories (standard setup)
  - Preserves git history for version tracking
  - Uses .gitignore patterns automatically

  If you need --noGit (rare), use manual commands instead.

EOF
}

# ============================================================================
# VALIDATION
# ============================================================================

check_prerequisites() {
    local errors=()

    # Check discovery.json exists
    if [ ! -f "$DISCOVERY_FILE" ]; then
        errors+=("discovery.json missing - run: .zcp/workflow.sh init")
    fi

    # Check session exists
    local session
    session=$(get_session 2>/dev/null)
    if [ -z "$session" ]; then
        errors+=("No active workflow session - run: .zcp/workflow.sh init")
    fi

    # Check zcli is available
    if ! command -v zcli &>/dev/null; then
        errors+=("zcli not found - install from https://docs.zerops.io/references/cli")
    fi

    if [ ${#errors[@]} -gt 0 ]; then
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "PREREQUISITES NOT MET"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        for err in "${errors[@]}"; do
            echo "  - $err"
        done
        echo ""
        return 1
    fi
    return 0
}

# ============================================================================
# SERVICE DISCOVERY
# ============================================================================

get_service_count() {
    jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null
}

get_service_dev_name() {
    local index="$1"
    local count
    count=$(get_service_count)

    if [ "$count" -eq 1 ]; then
        jq -r '.dev.name // .services[0].dev.name // ""' "$DISCOVERY_FILE" 2>/dev/null
    else
        jq -r ".services[$index].dev.name // \"\"" "$DISCOVERY_FILE" 2>/dev/null
    fi
}

get_service_stage_id() {
    local index="$1"
    local count
    count=$(get_service_count)

    if [ "$count" -eq 1 ]; then
        jq -r '.stage.id // .services[0].stage.id // ""' "$DISCOVERY_FILE" 2>/dev/null
    else
        jq -r ".services[$index].stage.id // \"\"" "$DISCOVERY_FILE" 2>/dev/null
    fi
}

get_service_stage_name() {
    local index="$1"
    local count
    count=$(get_service_count)

    if [ "$count" -eq 1 ]; then
        jq -r '.stage.name // .services[0].stage.name // ""' "$DISCOVERY_FILE" 2>/dev/null
    else
        jq -r ".services[$index].stage.name // \"\"" "$DISCOVERY_FILE" 2>/dev/null
    fi
}

get_service_dev_id() {
    local index="$1"
    local count
    count=$(get_service_count)

    if [ "$count" -eq 1 ]; then
        jq -r '.dev.id // .services[0].dev.id // ""' "$DISCOVERY_FILE" 2>/dev/null
    else
        jq -r ".services[$index].dev.id // \"\"" "$DISCOVERY_FILE" 2>/dev/null
    fi
}

# Find service index by dev or stage name
find_service_index() {
    local name="$1"
    local count
    count=$(get_service_count)

    local i=0
    while [ "$i" -lt "$count" ]; do
        local dev_name stage_name
        dev_name=$(get_service_dev_name "$i")
        stage_name=$(get_service_stage_name "$i")

        if [ "$dev_name" = "$name" ] || [ "$stage_name" = "$name" ]; then
            echo "$i"
            return 0
        fi
        i=$((i + 1))
    done

    echo "-1"
    return 1
}

# ============================================================================
# DEPLOYMENT
# ============================================================================

# Generate the zcli auth command
get_auth_command() {
    echo 'zcli login --region=gomibako --regionUrl="https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli" "$ZEROPS_ZCP_API_KEY"'
}

# Generate deploy command for a service
generate_deploy_command() {
    local dev_name="$1"
    local target_id="$2"
    local setup="${3:-prod}"

    local auth_cmd
    auth_cmd=$(get_auth_command)

    # Prefer --deploy-git-folder over --noGit (git-based is standard)
    echo "ssh $dev_name 'cd /var/www && $auth_cmd && zcli push $target_id --setup=$setup --deploy-git-folder'"
}

# Show copy-paste ready commands
show_commands() {
    local target="$1"
    local specific_service="$2"

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "COPY-PASTE DEPLOY COMMANDS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    local count
    count=$(get_service_count)

    local i=0
    local step=1

    while [ "$i" -lt "$count" ]; do
        local dev_name stage_name stage_id dev_id
        dev_name=$(get_service_dev_name "$i")
        stage_name=$(get_service_stage_name "$i")
        stage_id=$(get_service_stage_id "$i")
        dev_id=$(get_service_dev_id "$i")

        # Skip if specific service requested and this isn't it
        if [ -n "$specific_service" ]; then
            if [ "$dev_name" != "$specific_service" ] && [ "$stage_name" != "$specific_service" ]; then
                i=$((i + 1))
                continue
            fi
        fi

        if [ "$target" = "stage" ]; then
            echo "# Step $step: Deploy $dev_name -> $stage_name"
            generate_deploy_command "$dev_name" "$stage_id" "prod"
            echo ""
            echo "# Wait for completion:"
            echo ".zcp/status.sh --wait $stage_name"
            echo ""
        elif [ "$target" = "dev" ]; then
            echo "# Step $step: Refresh $dev_name"
            generate_deploy_command "$dev_name" "$dev_id" "dev"
            echo ""
            echo "# Wait for completion:"
            echo ".zcp/status.sh --wait $dev_name"
            echo ""
        fi

        step=$((step + 1))
        i=$((i + 1))
    done

    if [ "$step" -eq 1 ]; then
        if [ -n "$specific_service" ]; then
            echo "Service '$specific_service' not found in discovery.json"
            echo ""
            echo "Available services:"
            i=0
            while [ "$i" -lt "$count" ]; do
                local dn sn
                dn=$(get_service_dev_name "$i")
                sn=$(get_service_stage_name "$i")
                echo "  - $dn (dev) / $sn (stage)"
                i=$((i + 1))
            done
        fi
        return 1
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# Execute deployment
execute_deploy() {
    local target="$1"
    local specific_service="$2"

    local count
    count=$(get_service_count)

    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║  DEPLOYING TO ${target^^}                                              ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""

    local i=0
    local deployed=0
    local failed=0

    while [ "$i" -lt "$count" ]; do
        local dev_name stage_name stage_id dev_id target_id target_name setup
        dev_name=$(get_service_dev_name "$i")
        stage_name=$(get_service_stage_name "$i")
        stage_id=$(get_service_stage_id "$i")
        dev_id=$(get_service_dev_id "$i")

        # Skip if specific service requested and this isn't it
        if [ -n "$specific_service" ]; then
            if [ "$dev_name" != "$specific_service" ] && [ "$stage_name" != "$specific_service" ]; then
                i=$((i + 1))
                continue
            fi
        fi

        if [ "$target" = "stage" ]; then
            target_id="$stage_id"
            target_name="$stage_name"
            setup="prod"
        else
            target_id="$dev_id"
            target_name="$dev_name"
            setup="dev"
        fi

        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "[$((i+1))/$count] $dev_name -> $target_name"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""

        # Check deployFiles first
        echo "Checking deployFiles in zerops.yml..."
        if ! ssh "$dev_name" "cat /var/www/zerops.yml 2>/dev/null | grep -q deployFiles" 2>/dev/null; then
            echo "  zerops.yml not found or no deployFiles section"
            echo "  Make sure /var/www/$dev_name/zerops.yml exists with deployFiles"
        fi

        # Generate and execute command
        local auth_cmd
        auth_cmd=$(get_auth_command)

        echo "Authenticating zcli..."
        if ! ssh "$dev_name" "cd /var/www && $auth_cmd" 2>&1; then
            echo "  Auth failed - check ZEROPS_ZCP_API_KEY"
            failed=$((failed + 1))
            i=$((i + 1))
            continue
        fi

        echo "Pushing to $target_name ($target_id)..."
        if ssh "$dev_name" "cd /var/www && zcli push $target_id --setup=$setup --deploy-git-folder" 2>&1; then
            echo ""
            echo "  Push initiated. Waiting for completion..."

            # Wait for deployment using status.sh if available
            if [ -x "$SCRIPT_DIR/status.sh" ]; then
                if "$SCRIPT_DIR/status.sh" --wait "$target_name" 2>&1; then
                    echo "  Deployment complete"
                    deployed=$((deployed + 1))
                else
                    echo "  Deployment may have failed - check logs"
                    failed=$((failed + 1))
                fi
            else
                echo "  Check status manually: zcli service log -S $target_id -P \$projectId"
                deployed=$((deployed + 1))
            fi
        else
            echo "  Push failed"
            failed=$((failed + 1))
        fi

        echo ""
        i=$((i + 1))
    done

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "SUMMARY: $deployed deployed, $failed failed"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

    if [ "$failed" -gt 0 ]; then
        return 1
    fi
    return 0
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    local target=""
    local specific_service=""
    local show_commands_only=false

    # Parse arguments
    while [ $# -gt 0 ]; do
        case "$1" in
            --help|-h)
                show_help
                exit 0
                ;;
            --commands|--dry-run)
                show_commands_only=true
                shift
                ;;
            stage|dev)
                target="$1"
                shift
                ;;
            *)
                if [ -z "$target" ]; then
                    echo "Unknown target: $1"
                    echo "Use: stage or dev"
                    exit 1
                else
                    specific_service="$1"
                fi
                shift
                ;;
        esac
    done

    # Default to stage if no target
    if [ -z "$target" ]; then
        target="stage"
    fi

    # Check prerequisites
    if ! check_prerequisites; then
        exit 1
    fi

    if [ "$show_commands_only" = true ]; then
        show_commands "$target" "$specific_service"
    else
        execute_deploy "$target" "$specific_service"
    fi
}

main "$@"
