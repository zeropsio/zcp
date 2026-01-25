#!/usr/bin/env bash
# .zcp/lib/bootstrap/orchestrator.sh
# Main bootstrap orchestrator

# Source dependencies
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/detect.sh"
source "$SCRIPT_DIR/import-gen.sh"
source "$SCRIPT_DIR/zerops-yml-gen.sh"

# Note: utils.sh is sourced by workflow.sh before this script runs
# This gives us: get_session, ZCP_TMP_DIR, STATE_DIR, colors, etc.

# Evidence file paths
BOOTSTRAP_PLAN_FILE="${ZCP_TMP_DIR:-/tmp}/bootstrap_plan.json"
BOOTSTRAP_IMPORT_FILE="${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml"
BOOTSTRAP_COORDINATION_FILE="${ZCP_TMP_DIR:-/tmp}/bootstrap_coordination.json"
BOOTSTRAP_COMPLETE_FILE="${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"

# Timeout constants
BOOTSTRAP_SERVICE_TIMEOUT=600    # 10 minutes for service creation
BOOTSTRAP_PUSH_TIMEOUT=300       # 5 minutes for push operations
BOOTSTRAP_SUBDOMAIN_TIMEOUT=60   # 1 minute for subdomain enable

# Initialize bootstrap plan
init_bootstrap_plan() {
    local runtime="$1"
    local services="$2"
    local prefix="$3"

    local session_id
    session_id=$(get_session)

    jq -n \
        --arg session "$session_id" \
        --arg runtime "$runtime" \
        --arg services "$services" \
        --arg prefix "$prefix" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        '{
            session_id: $session,
            timestamp: $ts,
            status: "planning",
            runtime: $runtime,
            managed_services: ($services | split(",") | map(select(. != ""))),
            hostname_prefix: $prefix,
            dev_hostname: "\($prefix)dev",
            stage_hostname: "\($prefix)stage"
        }' > "$BOOTSTRAP_PLAN_FILE"
}

# Update coordination file atomically
update_coordination() {
    local update_expr="$1"

    local coord
    coord=$(cat "$BOOTSTRAP_COORDINATION_FILE" 2>/dev/null || echo '{}')
    coord=$(echo "$coord" | jq "$update_expr")

    # Atomic write
    local tmp_file="${BOOTSTRAP_COORDINATION_FILE}.tmp.$$"
    echo "$coord" > "$tmp_file"
    mv "$tmp_file" "$BOOTSTRAP_COORDINATION_FILE"

    # Write-through to persistent storage
    if [ -d "$STATE_DIR" ]; then
        mkdir -p "$STATE_DIR/bootstrap"
        cp "$BOOTSTRAP_COORDINATION_FILE" "$STATE_DIR/bootstrap/coordination.json"
    fi
}

# Write checkpoint
write_checkpoint() {
    local step="$1"
    local status="$2"
    local data="${3:-{}}"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Use jq args to safely inject values (avoids shell quoting issues)
    update_coordination "$(jq -n \
        --arg step "$step" \
        --arg status "$status" \
        --arg ts "$timestamp" \
        --argjson data "$data" \
        '. + {checkpoints: ((.checkpoints // {}) + {($step): {status: $status, at: $ts, data: $data}})}'
    )"
}

# Check if step completed
is_step_complete() {
    local step="$1"
    local status
    status=$(jq -r ".checkpoints[\"$step\"].status // \"\"" "$BOOTSTRAP_COORDINATION_FILE" 2>/dev/null)
    [ "$status" = "complete" ]
}

# Wait for services to be RUNNING
wait_for_services() {
    local timeout="$BOOTSTRAP_SERVICE_TIMEOUT"
    local start_time=$(date +%s)

    while true; do
        local elapsed=$(($(date +%s) - start_time))
        if [ $elapsed -gt $timeout ]; then
            echo "ERROR: Timeout waiting for services" >&2
            return 1
        fi

        local services
        services=$(zcli service list -P "$projectId" --format json 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g')

        local pending
        pending=$(echo "$services" | jq '[.services[] | select(.status != "RUNNING" and .status != "ACTIVE")] | length')

        if [ "$pending" = "0" ]; then
            echo "All services RUNNING"
            return 0
        fi

        echo "Waiting... ($pending services pending, ${elapsed}s elapsed)"
        sleep 5
    done
}

# Enable subdomain for a service
enable_subdomain() {
    local service_id="$1"
    local hostname="$2"

    zcli service enable-subdomain -S "$service_id" 2>&1 || {
        echo "WARNING: Could not enable subdomain for $service_id" >&2
        return 1
    }

    # Get the subdomain URL from inside the service
    sleep 2
    local url
    url=$(ssh "$hostname" 'echo $zeropsSubdomain' 2>/dev/null)
    echo "$url"
}

# Main bootstrap command
cmd_bootstrap() {
    local runtime="" services="" prefix="app" resume="false" ha_mode="false"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --runtime) runtime="$2"; shift 2 ;;
            --services) services="$2"; shift 2 ;;
            --prefix) prefix="$2"; shift 2 ;;
            --resume) resume="true"; shift ;;
            --ha) ha_mode="true"; shift ;;
            -h|--help) show_bootstrap_help; return 0 ;;
            *) echo "Unknown option: $1" >&2; return 1 ;;
        esac
    done

    # Resume mode
    if [ "$resume" = "true" ]; then
        if [ ! -f "$BOOTSTRAP_COORDINATION_FILE" ]; then
            # Try persistent storage
            if [ -f "$STATE_DIR/bootstrap/coordination.json" ]; then
                cp "$STATE_DIR/bootstrap/coordination.json" "$BOOTSTRAP_COORDINATION_FILE"
            else
                echo "ERROR: No bootstrap in progress to resume" >&2
                return 1
            fi
        fi
        echo "Resuming bootstrap from last checkpoint..."
        # Fall through to orchestration - read plan file too
        if [ -f "$STATE_DIR/bootstrap/plan.json" ]; then
            cp "$STATE_DIR/bootstrap/plan.json" "$BOOTSTRAP_PLAN_FILE"
        fi
    else
        # New bootstrap - require runtime
        if [ -z "$runtime" ]; then
            echo "ERROR: --runtime required" >&2
            echo "Usage: .zcp/workflow.sh bootstrap --runtime go --services postgresql,valkey" >&2
            return 1
        fi

        # Check zcli authentication and project access
        local zcli_test_result zcli_exit
        zcli_test_result=$(zcli service list -P "$projectId" --format json 2>&1)
        zcli_exit=$?

        if [ $zcli_exit -ne 0 ]; then
            # Check for specific error types
            if echo "$zcli_test_result" | grep -qiE "unauthorized|auth|login|token|403"; then
                cat <<'ZCLI_AUTH'
⛔ zcli is not authenticated. Run this first:

   zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"

Then re-run:

   .zcp/workflow.sh bootstrap --runtime go --services postgresql,valkey
ZCLI_AUTH
            elif [ -z "$projectId" ]; then
                echo "⛔ projectId is not set. Are you running inside ZCP?" >&2
            else
                echo "⛔ zcli service list failed:" >&2
                echo "$zcli_test_result" >&2
            fi
            return 1
        fi

        # Check project state
        local state
        state=$(detect_project_state)

        case "$state" in
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
                echo "Check: is projectId set? is zcli authenticated?" >&2
                return 1
                ;;
        esac

        # Initialize plan
        init_bootstrap_plan "$runtime" "$services" "$prefix"

        # Persist plan
        if [ -d "$STATE_DIR" ]; then
            mkdir -p "$STATE_DIR/bootstrap"
            cp "$BOOTSTRAP_PLAN_FILE" "$STATE_DIR/bootstrap/plan.json"
        fi

        # Initialize coordination file
        jq -n \
            --arg session "$(get_session)" \
            --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
            '{
                session_id: $session,
                started_at: $ts,
                status: "in_progress",
                checkpoints: {}
            }' > "$BOOTSTRAP_COORDINATION_FILE"
    fi

    # === STEP 1: Recipe Search ===
    if ! is_step_complete "recipe_search"; then
        echo ""
        echo -e "${CYAN}=== STEP 1: Recipe Search ===${NC}"

        local runtime_type
        runtime_type=$(jq -r '.runtime' "$BOOTSTRAP_PLAN_FILE")

        if [ ! -f "${ZCP_TMP_DIR:-/tmp}/recipe_review.json" ]; then
            echo "Running recipe-search for $runtime_type..."
            local svc_list
            svc_list=$(jq -r '.managed_services | join(" ")' "$BOOTSTRAP_PLAN_FILE")

            # recipe-search.sh is in the parent .zcp directory
            local recipe_script="$SCRIPT_DIR/../../recipe-search.sh"
            if [ -f "$recipe_script" ]; then
                "$recipe_script" quick "$runtime_type" $svc_list || {
                    echo "WARNING: Recipe search failed, continuing with defaults" >&2
                }
            else
                echo "WARNING: recipe-search.sh not found at $recipe_script, using defaults" >&2
            fi
        fi

        write_checkpoint "recipe_search" "complete" '{"file": "/tmp/recipe_review.json"}'
        echo -e "${GREEN}[CHECKPOINT]${NC} Recipe search complete"
    fi

    # === STEP 2: Generate Import ===
    if ! is_step_complete "import_generated"; then
        echo ""
        echo -e "${CYAN}=== STEP 2: Generate import.yml ===${NC}"

        local rt svc pfx
        rt=$(jq -r '.runtime' "$BOOTSTRAP_PLAN_FILE")
        svc=$(jq -r '.managed_services | join(",")' "$BOOTSTRAP_PLAN_FILE")
        pfx=$(jq -r '.hostname_prefix' "$BOOTSTRAP_PLAN_FILE")

        local gen_args="--runtime $rt --prefix $pfx"
        [ -n "$svc" ] && gen_args="$gen_args --services $svc"
        [ "$ha_mode" = "true" ] && gen_args="$gen_args --ha"

        generate_import_yml $gen_args --output "$BOOTSTRAP_IMPORT_FILE"

        echo "Generated: $BOOTSTRAP_IMPORT_FILE"
        echo ""
        cat "$BOOTSTRAP_IMPORT_FILE"

        write_checkpoint "import_generated" "complete" "{\"file\": \"$BOOTSTRAP_IMPORT_FILE\"}"
        echo -e "${GREEN}[CHECKPOINT]${NC} Import file generated"
    fi

    # === STEP 3: Import Services ===
    if ! is_step_complete "services_imported"; then
        echo ""
        echo -e "${CYAN}=== STEP 3: Import Services ===${NC}"

        zcli project service-import "$BOOTSTRAP_IMPORT_FILE" -P "$projectId" || {
            echo "ERROR: Service import failed" >&2
            write_checkpoint "services_imported" "failed" '{"error": "zcli import failed"}'
            return 1
        }

        echo "Waiting for services to be RUNNING..."
        wait_for_services || return 1

        # Get service IDs
        local services_json
        services_json=$(zcli service list -P "$projectId" --format json | sed 's/\x1b\[[0-9;]*m//g')

        write_checkpoint "services_imported" "complete" "$services_json"
        echo -e "${GREEN}[CHECKPOINT]${NC} Services imported and running"
    fi

    # === STEP 4: Enable Subdomains ===
    if ! is_step_complete "subdomains_enabled"; then
        echo ""
        echo -e "${CYAN}=== STEP 4: Enable Subdomains ===${NC}"

        local dev_hostname stage_hostname
        dev_hostname=$(jq -r '.dev_hostname' "$BOOTSTRAP_PLAN_FILE")
        stage_hostname=$(jq -r '.stage_hostname' "$BOOTSTRAP_PLAN_FILE")

        local services_json dev_id stage_id
        services_json=$(jq -r '.checkpoints.services_imported.data' "$BOOTSTRAP_COORDINATION_FILE")
        dev_id=$(echo "$services_json" | jq -r --arg h "$dev_hostname" '.services[] | select(.name == $h) | .id')
        stage_id=$(echo "$services_json" | jq -r --arg h "$stage_hostname" '.services[] | select(.name == $h) | .id')

        local dev_url stage_url
        dev_url=$(enable_subdomain "$dev_id" "$dev_hostname")
        stage_url=$(enable_subdomain "$stage_id" "$stage_hostname")

        write_checkpoint "subdomains_enabled" "complete" \
            "$(jq -n --arg d "$dev_url" --arg s "$stage_url" '{dev_url: $d, stage_url: $s}')"
        echo -e "${GREEN}[CHECKPOINT]${NC} Subdomains enabled"
        echo "  Dev:   $dev_url"
        echo "  Stage: $stage_url"
    fi

    # === STEP 5: Generate zerops.yml Skeleton ===
    if ! is_step_complete "zerops_yml_generated"; then
        echo ""
        echo -e "${CYAN}=== STEP 5: Generate zerops.yml Skeleton ===${NC}"

        local dev_hostname
        dev_hostname=$(jq -r '.dev_hostname' "$BOOTSTRAP_PLAN_FILE")

        local mount_path="/var/www/$dev_hostname"
        if [ ! -d "$mount_path" ]; then
            echo -e "${YELLOW}Mount point not ready: $mount_path${NC}"
            echo "Run: .zcp/mount.sh $dev_hostname"
            echo "Then: .zcp/workflow.sh bootstrap --resume"
            return 1
        fi

        generate_zerops_yml_skeleton "$dev_hostname" "8080"
        echo "Generated: $mount_path/zerops.yml"

        write_checkpoint "zerops_yml_generated" "complete" \
            "$(jq -n --arg f "$mount_path/zerops.yml" '{file: $f}')"
        echo -e "${GREEN}[CHECKPOINT]${NC} zerops.yml skeleton generated"
    fi

    # === HANDOFF TO AGENT ===
    local dev_hostname stage_hostname
    dev_hostname=$(jq -r '.dev_hostname' "$BOOTSTRAP_PLAN_FILE")
    stage_hostname=$(jq -r '.stage_hostname' "$BOOTSTRAP_PLAN_FILE")

    echo ""
    echo -e "${BOLD}==========================================${NC}"
    echo -e "${BOLD}  BOOTSTRAP SCAFFOLDING COMPLETE${NC}"
    echo -e "${BOLD}==========================================${NC}"
    echo ""
    echo -e "${CYAN}Agent task now:${NC}"
    echo ""
    echo "1. COMPLETE zerops.yml at /var/www/${dev_hostname}/zerops.yml"
    echo "   - Fill in buildCommands from /tmp/recipe_review.json patterns"
    echo "   - Fill in deployFiles from recipe patterns"
    echo "   - Fill in start command for prod setup"
    echo ""
    echo "2. CREATE APPLICATION CODE"
    echo "   Goal: Minimal status page that:"
    echo "   - GET / returns {\"service\": \"<hostname>\", \"status\": \"running\"}"
    echo "   - GET /status returns health checks for each managed service"
    echo "   - Managed services to ping:"

    local managed_svcs
    managed_svcs=$(jq -r '.managed_services[]' "$BOOTSTRAP_PLAN_FILE" 2>/dev/null)
    for svc in $managed_svcs; do
        echo "     - $svc (use granular env vars: DB_HOST, DB_PORT, etc.)"
    done

    # Get service IDs for instructions
    local services_json dev_id stage_id
    services_json=$(jq -r '.checkpoints.services_imported.data' "$BOOTSTRAP_COORDINATION_FILE")
    dev_id=$(echo "$services_json" | jq -r --arg h "$dev_hostname" '.services[] | select(.name == $h) | .id')
    stage_id=$(echo "$services_json" | jq -r --arg h "$stage_hostname" '.services[] | select(.name == $h) | .id')

    echo ""
    echo "3. PUSH dev→dev to activate:"
    echo "   ssh $dev_hostname 'zcli push \$(hostname) --setup=dev'"
    echo ""
    echo "4. TEST dev endpoints:"
    echo "   .zcp/verify.sh $dev_hostname 8080 / /status"
    echo ""
    echo "5. PUSH dev→stage for production:"
    echo "   ssh $dev_hostname 'zcli push $stage_id --setup=prod'"
    echo ""
    echo "6. Continue with standard workflow:"
    echo "   .zcp/workflow.sh init"
    echo "   .zcp/workflow.sh create_discovery $dev_id $dev_hostname $stage_id $stage_hostname"
    echo ""

    # Write handoff evidence
    jq -n \
        --arg session "$(get_session)" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        --slurpfile plan "$BOOTSTRAP_PLAN_FILE" \
        --arg dev_id "$dev_id" \
        --arg stage_id "$stage_id" \
        '{
            session_id: $session,
            timestamp: $ts,
            status: "agent_handoff",
            plan: $plan[0],
            service_ids: {
                dev: $dev_id,
                stage: $stage_id
            },
            agent_tasks: [
                "Complete zerops.yml with build commands from recipe-search",
                "Create minimal status page with managed service health checks",
                "Push dev→dev to activate environment",
                "Test endpoints with verify.sh",
                "Push dev→stage for production",
                "Continue with workflow init and create_discovery"
            ]
        }' > "$BOOTSTRAP_COMPLETE_FILE"

    return 0
}

show_bootstrap_help() {
    cat <<'EOF'
BOOTSTRAP - Create services and scaffolding for new runtimes

USAGE:
    .zcp/workflow.sh bootstrap --runtime <type> [--services <list>] [--prefix <name>]
    .zcp/workflow.sh bootstrap --resume

OPTIONS:
    --runtime <type>     Runtime type: go, nodejs, python, php, rust, bun, java, dotnet
    --services <list>    Managed services: postgresql,valkey,elasticsearch (comma-separated)
    --prefix <name>      Hostname prefix (default: app) creates appdev, appstage
    --resume             Resume interrupted bootstrap
    --ha                 Use HA mode for managed services

EXAMPLES:
    # Go backend with PostgreSQL and Redis
    .zcp/workflow.sh bootstrap --runtime go --services postgresql,valkey

    # Node.js API with custom prefix
    .zcp/workflow.sh bootstrap --runtime nodejs --services postgresql --prefix api

    # Resume after interruption
    .zcp/workflow.sh bootstrap --resume

WHAT IT DOES:
    1. Runs recipe-search for runtime patterns
    2. Generates import.yml with managed services (priority: 10) and runtimes
    3. Imports services and waits for RUNNING
    4. Enables subdomains for dev and stage
    5. Generates zerops.yml skeleton with env var references
    6. Hands off to agent to:
       - Complete zerops.yml with build commands from recipe patterns
       - Write minimal status page code
       - Push and test

IMPORTANT:
    - Agent writes all application code (no templates)
    - Recipe-search provides patterns, not boilerplate
    - Granular env vars (DB_HOST, DB_PORT), not connection strings
EOF
}

export -f cmd_bootstrap
