#!/usr/bin/env bash
# .zcp/lib/bootstrap/orchestrator.sh
# Main bootstrap orchestrator

set -euo pipefail  # Fail fast on errors, undefined vars, pipe failures

# Source dependencies
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/detect.sh"
source "$SCRIPT_DIR/import-gen.sh"
source "$SCRIPT_DIR/zerops-yml-gen.sh"

# Note: utils.sh is sourced by workflow.sh before this script runs
# This gives us: get_session, ZCP_TMP_DIR, STATE_DIR, colors, etc.

# Color fallbacks (in case utils.sh wasn't sourced or colors not exported)
# These provide defaults for set -u compatibility
CYAN="${CYAN:-\033[0;36m}"
GREEN="${GREEN:-\033[0;32m}"
YELLOW="${YELLOW:-\033[1;33m}"
RED="${RED:-\033[0;31m}"
BOLD="${BOLD:-\033[1m}"
NC="${NC:-\033[0m}"

# Path fallbacks (in case utils.sh wasn't sourced)
ZCP_TMP_DIR="${ZCP_TMP_DIR:-/tmp}"
SESSION_FILE="${SESSION_FILE:-${ZCP_TMP_DIR}/claude_session}"
MODE_FILE="${MODE_FILE:-${ZCP_TMP_DIR}/claude_mode}"
PHASE_FILE="${PHASE_FILE:-${ZCP_TMP_DIR}/claude_phase}"
STATE_DIR="${STATE_DIR:-${SCRIPT_DIR}/state}"

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

    # Validate data is valid JSON, fallback to empty object if not
    if [ -z "$data" ]; then
        data='{}'
    elif ! echo "$data" | jq -e . >/dev/null 2>&1; then
        # Silently use empty object - warnings are noise, we handle it gracefully
        data='{}'
    fi

    # Use jq with proper JSON parsing to avoid shell quoting issues
    local update_expr
    update_expr=$(jq -n \
        --arg step "$step" \
        --arg status "$status" \
        --arg ts "$timestamp" \
        --argjson data "$data" \
        '. + {checkpoints: ((.checkpoints // {}) + {($step): {status: $status, at: $ts, data: $data}})}')

    update_coordination "$update_expr"
}

# Extract JSON from zcli output (skips log lines before JSON)
extract_json() {
    # zcli outputs log messages before JSON even with --format json
    # Strip ANSI codes, then find first line starting with { or [ and output from there
    local input
    input=$(sed 's/\x1b\[[0-9;]*m//g')
    # Find and output from first JSON line
    echo "$input" | awk '/^\s*[\{\[]/{found=1} found{print}'
}

# Get services as clean JSON
get_services_json() {
    local result
    result=$(zcli service list -P "$projectId" --format json 2>/dev/null | extract_json)
    if [ -z "$result" ] || ! echo "$result" | jq -e . >/dev/null 2>&1; then
        echo '{"services":[]}'
    else
        echo "$result"
    fi
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
        services=$(get_services_json)

        local pending
        pending=$(echo "$services" | jq '[(.services // [])[] | select(.status != "RUNNING" and .status != "ACTIVE")] | length')

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

        # Initialize session if not exists (bootstrap can be first command run)
        if [ -z "$(get_session)" ]; then
            local session_id
            session_id="$(date +%Y%m%d%H%M%S)-$RANDOM-$RANDOM"
            echo "$session_id" > "$SESSION_FILE"
            echo "bootstrap" > "$MODE_FILE"
            echo "INIT" > "$PHASE_FILE"
            echo "Bootstrap session created: $session_id"
        fi

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
            svc_list=$(jq -r '(.managed_services // []) | join(" ")' "$BOOTSTRAP_PLAN_FILE")

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

        write_checkpoint "recipe_search" "complete" "$(jq -n --arg f "/tmp/recipe_review.json" '{file: $f}')"
        echo -e "${GREEN}[CHECKPOINT]${NC} Recipe search complete"
    fi

    # === STEP 2: Generate Import ===
    if ! is_step_complete "import_generated"; then
        echo ""
        echo -e "${CYAN}=== STEP 2: Generate import.yml ===${NC}"

        local rt svc pfx
        rt=$(jq -r '.runtime' "$BOOTSTRAP_PLAN_FILE")
        svc=$(jq -r '(.managed_services // []) | join(",")' "$BOOTSTRAP_PLAN_FILE")
        pfx=$(jq -r '.hostname_prefix' "$BOOTSTRAP_PLAN_FILE")

        local gen_args="--runtime $rt --prefix $pfx"
        [ -n "$svc" ] && gen_args="$gen_args --services $svc"
        [ "$ha_mode" = "true" ] && gen_args="$gen_args --ha"

        generate_import_yml $gen_args --output "$BOOTSTRAP_IMPORT_FILE"

        echo "Generated: $BOOTSTRAP_IMPORT_FILE"
        echo ""
        cat "$BOOTSTRAP_IMPORT_FILE"

        write_checkpoint "import_generated" "complete" "$(jq -n --arg f "$BOOTSTRAP_IMPORT_FILE" '{file: $f}')"
        echo -e "${GREEN}[CHECKPOINT]${NC} Import file generated"
    fi

    # === STEP 3: Import Services ===
    if ! is_step_complete "services_imported"; then
        echo ""
        echo -e "${CYAN}=== STEP 3: Import Services ===${NC}"

        # Check if expected services already exist (resume after partial failure)
        local dev_hostname stage_hostname
        dev_hostname=$(jq -r '.dev_hostname' "$BOOTSTRAP_PLAN_FILE")
        stage_hostname=$(jq -r '.stage_hostname' "$BOOTSTRAP_PLAN_FILE")

        local existing_services
        existing_services=$(get_services_json)
        local dev_exists stage_exists
        dev_exists=$(echo "$existing_services" | jq -r --arg h "$dev_hostname" '.services[] | select(.name == $h) | .name' 2>/dev/null)
        stage_exists=$(echo "$existing_services" | jq -r --arg h "$stage_hostname" '.services[] | select(.name == $h) | .name' 2>/dev/null)

        if [ -n "$dev_exists" ] && [ -n "$stage_exists" ]; then
            echo "Services already exist (resuming after partial failure)"
            echo "  Found: $dev_hostname, $stage_hostname"
        else
            # Run import - ignore EOF errors (transient backend issue, assume success)
            echo "Importing services..."
            zcli project service-import "$BOOTSTRAP_IMPORT_FILE" -P "$projectId" 2>&1 || {
                echo "⚠️  Import returned error (often false positive due to EOF), continuing..."
            }
        fi

        echo "Waiting for services to be RUNNING..."
        wait_for_services || return 1

        # Get service IDs (using helper to extract clean JSON)
        local services_json
        services_json=$(get_services_json)

        # Pass JSON via process substitution to avoid shell quoting issues
        write_checkpoint "services_imported" "complete" "$(echo "$services_json" | jq -c .)"
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
        dev_id=$(echo "$services_json" | jq -r --arg h "$dev_hostname" '(.services // [])[] | select(.name == $h) | .id')
        stage_id=$(echo "$services_json" | jq -r --arg h "$stage_hostname" '(.services // [])[] | select(.name == $h) | .id')

        local dev_url stage_url
        dev_url=$(enable_subdomain "$dev_id" "$dev_hostname")
        stage_url=$(enable_subdomain "$stage_id" "$stage_hostname")

        write_checkpoint "subdomains_enabled" "complete" "$(jq -n --arg d "$dev_url" --arg s "$stage_url" '{dev_url: $d, stage_url: $s}')"
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

        write_checkpoint "zerops_yml_generated" "complete" "$(jq -n --arg f "$mount_path/zerops.yml" '{file: $f}')"
        echo -e "${GREEN}[CHECKPOINT]${NC} zerops.yml skeleton generated"
    fi

    # === HANDOFF TO AGENT ===
    local dev_hostname stage_hostname
    dev_hostname=$(jq -r '.dev_hostname' "$BOOTSTRAP_PLAN_FILE")
    stage_hostname=$(jq -r '.stage_hostname' "$BOOTSTRAP_PLAN_FILE")

    # Get service IDs for instructions
    local services_json dev_id stage_id
    services_json=$(jq -r '.checkpoints.services_imported.data' "$BOOTSTRAP_COORDINATION_FILE")
    dev_id=$(echo "$services_json" | jq -r --arg h "$dev_hostname" '(.services // [])[] | select(.name == $h) | .id')
    stage_id=$(echo "$services_json" | jq -r --arg h "$stage_hostname" '(.services // [])[] | select(.name == $h) | .id')

    echo ""
    echo -e "${GREEN}╔══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║  ✅ SCAFFOLDING COMPLETE - Services created and running          ║${NC}"
    echo -e "${GREEN}╚══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo "Services:"
    echo "  Dev:   $dev_hostname (ID: $dev_id)"
    echo "  Stage: $stage_hostname (ID: $stage_id)"
    echo "  Files: /var/www/$dev_hostname/"
    echo ""
    echo -e "${CYAN}╔══════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║  📋 START TASK 1 NOW - Complete zerops.yml                       ║${NC}"
    echo -e "${CYAN}╚══════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo "Edit: /var/www/${dev_hostname}/zerops.yml"
    echo ""
    echo "Fill in from /tmp/recipe_review.json:"
    echo "  - buildCommands (how to compile)"
    echo "  - deployFiles (what to deploy)"
    echo "  - start command"
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo -e "${CYAN}📋 TASK 2: Create application code${NC}"
    echo ""
    echo "Write in: /var/www/${dev_hostname}/"
    echo "Goal: Minimal status page with:"
    echo "  - GET / returns {\"service\": \"$dev_hostname\", \"status\": \"running\"}"
    echo "  - GET /status returns health checks"

    local managed_svcs
    managed_svcs=$(jq -r '(.managed_services // [])[]' "$BOOTSTRAP_PLAN_FILE" 2>/dev/null)
    if [ -n "$managed_svcs" ]; then
        echo ""
        echo "Managed services to ping:"
        for svc in $managed_svcs; do
            echo "  - $svc (use granular env vars: DB_HOST, DB_PORT, etc.)"
        done
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo -e "${CYAN}📋 TASK 3: Push and test dev${NC}"
    echo "   ssh $dev_hostname 'zcli push \$(hostname) --setup=dev'"
    echo "   .zcp/verify.sh $dev_hostname 8080 / /status"
    echo ""
    echo -e "${CYAN}📋 TASK 4: Push and test stage${NC}"
    echo "   ssh $dev_hostname 'zcli push $stage_id --setup=prod'"
    echo "   .zcp/verify.sh $stage_hostname 8080 / /status"
    echo ""
    echo -e "${CYAN}📋 TASK 5: Mark complete${NC}"
    echo "   .zcp/workflow.sh bootstrap-done"
    echo ""
    echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${YELLOW}⛔ DO NOT run 'workflow init' or 'transition_to' until TASK 5!${NC}"
    echo -e "${YELLOW}   Workflow transitions are BLOCKED until bootstrap-done.${NC}"
    echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "Run '.zcp/workflow.sh show' anytime to see pending tasks."
    echo ""

    # Write HANDOFF evidence (NOT complete - agent must run bootstrap-done after tasks)
    local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"
    jq -n \
        --arg session "$(get_session)" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        --slurpfile plan "$BOOTSTRAP_PLAN_FILE" \
        --arg dev_id "$dev_id" \
        --arg stage_id "$stage_id" \
        --arg dev_hostname "$dev_hostname" \
        --arg stage_hostname "$stage_hostname" \
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
                "TASK 1: Edit /var/www/\($dev_hostname)/zerops.yml - fill in buildCommands, deployFiles, start",
                "TASK 2: Create application code in /var/www/\($dev_hostname)/ - minimal status page",
                "TASK 3: Push to dev: ssh \($dev_hostname) \"zcli push $(hostname) --setup=dev\"",
                "TASK 4: Push to stage: ssh \($dev_hostname) \"zcli push \($stage_id) --setup=prod\"",
                "TASK 5: Run: .zcp/workflow.sh bootstrap-done"
            ],
            next_command: ".zcp/workflow.sh bootstrap-done"
        }' > "$handoff_file"

    # Also persist to state dir
    if [ -d "$STATE_DIR" ]; then
        cp "$handoff_file" "$STATE_DIR/bootstrap/handoff.json"
    fi

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

# Mark bootstrap as complete (agent runs this after completing all handoff tasks)
cmd_bootstrap_done() {
    local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"

    # Check handoff file exists
    if [ ! -f "$handoff_file" ]; then
        echo "❌ No bootstrap handoff found."
        echo ""
        echo "Either bootstrap hasn't run, or it's already complete."
        echo "Check: .zcp/workflow.sh show"
        return 1
    fi

    # Get plan info
    local dev_hostname stage_hostname
    dev_hostname=$(jq -r '.plan.dev_hostname' "$handoff_file")
    stage_hostname=$(jq -r '.plan.stage_hostname' "$handoff_file")

    echo "Verifying bootstrap tasks..."
    echo ""

    # Check 1: zerops.yml exists and has content
    local zerops_yml="/var/www/$dev_hostname/zerops.yml"
    if [ ! -f "$zerops_yml" ]; then
        echo "❌ zerops.yml not found: $zerops_yml"
        echo "   Complete step 1: Fill in zerops.yml"
        return 1
    fi

    local yml_size
    yml_size=$(wc -c < "$zerops_yml")
    if [ "$yml_size" -lt 100 ]; then
        echo "❌ zerops.yml looks incomplete (only $yml_size bytes)"
        echo "   Complete step 1: Fill in build commands and deployFiles"
        return 1
    fi
    echo "✓ zerops.yml exists ($yml_size bytes)"

    # Check 2: Some code exists (main.go, index.js, etc.)
    local has_code="false"
    for pattern in main.go index.js app.py main.py server.go cmd/main.go; do
        if [ -f "/var/www/$dev_hostname/$pattern" ]; then
            has_code="true"
            echo "✓ Application code found: $pattern"
            break
        fi
    done

    if [ "$has_code" = "false" ]; then
        echo "⚠️  No recognized application code found (checking for any source files...)"
        local code_files
        code_files=$(find "/var/www/$dev_hostname" -maxdepth 2 -type f \( -name "*.go" -o -name "*.js" -o -name "*.py" -o -name "*.rs" \) 2>/dev/null | head -3)
        if [ -n "$code_files" ]; then
            echo "✓ Found source files:"
            echo "$code_files" | while read -r f; do echo "   $f"; done
        else
            echo "❌ No source code found in /var/www/$dev_hostname"
            echo "   Complete step 2: Create application code"
            return 1
        fi
    fi

    # Check 3: Dev verification evidence (optional but encouraged)
    local dev_verify_file="${ZCP_TMP_DIR:-/tmp}/dev_verify.json"
    if [ -f "$dev_verify_file" ]; then
        echo "✓ Dev verification evidence found"
    else
        echo "⚠️  No dev verification evidence (/tmp/dev_verify.json)"
        echo "   Recommended: .zcp/verify.sh $dev_hostname 8080 / /status"
    fi

    # Check 4: Stage verification evidence (optional but encouraged)
    local stage_verify_file="${ZCP_TMP_DIR:-/tmp}/stage_verify.json"
    if [ -f "$stage_verify_file" ]; then
        echo "✓ Stage verification evidence found"
    else
        echo "⚠️  No stage verification evidence (/tmp/stage_verify.json)"
        echo "   Recommended: .zcp/verify.sh $stage_hostname 8080 / /status"
    fi

    echo ""

    # Write completion evidence
    local dev_id stage_id
    dev_id=$(jq -r '.service_ids.dev' "$handoff_file")
    stage_id=$(jq -r '.service_ids.stage' "$handoff_file")

    jq -n \
        --arg session "$(get_session)" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        --slurpfile handoff "$handoff_file" \
        --arg dev_id "$dev_id" \
        --arg stage_id "$stage_id" \
        '{
            session_id: $session,
            completed_at: $ts,
            status: "completed",
            handoff: $handoff[0],
            service_ids: {
                dev: $dev_id,
                stage: $stage_id
            }
        }' > "$BOOTSTRAP_COMPLETE_FILE"

    # Persist
    if [ -d "$STATE_DIR" ]; then
        cp "$BOOTSTRAP_COMPLETE_FILE" "$STATE_DIR/bootstrap/complete.json"
    fi

    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}  ✅ BOOTSTRAP COMPLETE${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "Workflow transitions are now unlocked."
    echo ""
    echo "Next steps:"
    echo "   .zcp/workflow.sh init"
    echo "   .zcp/workflow.sh create_discovery $dev_id $dev_hostname $stage_id $stage_hostname"
    echo "   .zcp/workflow.sh transition_to DEVELOP"
    echo ""

    return 0
}

export -f cmd_bootstrap cmd_bootstrap_done
