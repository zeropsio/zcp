#!/bin/bash
# Status and show commands for Zerops Workflow

# ============================================================================
# RUNTIME DEFAULTS (FIX-03: Resolve placeholders)
# ============================================================================

# Get runtime-specific defaults for guidance (sets variables via eval)
# Usage: eval "$(get_runtime_defaults "go")"
# Sets: proc, binary, build, port
get_runtime_defaults() {
    local runtime="$1"
    case "$runtime" in
        go|go@*)
            echo "proc=app binary=app build='go build -o app' port=8080"
            ;;
        nodejs|nodejs@*|node|node@*)
            echo "proc=node binary='node index.js' build='npm install && npm run build' port=8080"
            ;;
        bun|bun@*)
            echo "proc=bun binary='bun run index.ts' build='bun install' port=8080"
            ;;
        python|python@*)
            echo "proc=python binary='python app.py' build='pip install -r requirements.txt' port=8080"
            ;;
        rust|rust@*)
            echo "proc=app binary='./target/release/app' build='cargo build --release' port=8080"
            ;;
        php|php@*)
            echo "proc=php binary='php -S 0.0.0.0:8080' build='composer install' port=8080"
            ;;
        dotnet|dotnet@*)
            echo "proc=dotnet binary='dotnet run' build='dotnet build' port=8080"
            ;;
        java|java@*)
            echo "proc=java binary='java -jar app.jar' build='mvn package' port=8080"
            ;;
        *)
            echo "proc=app binary='./app' build='<see zerops.yml>' port=8080"
            ;;
    esac
}

# Read build command from zerops.yml if available
get_build_from_zerops_yml() {
    local hostname="$1"
    local config_file="/var/www/${hostname}/zerops.yml"

    if [ -f "$config_file" ] && command -v yq &>/dev/null; then
        # Try to extract buildCommands
        local build_cmd
        build_cmd=$(yq e ".zerops[] | select(.hostname == \"$hostname\" or .setup) | .build.buildCommands[0] // empty" "$config_file" 2>/dev/null)
        if [ -n "$build_cmd" ]; then
            echo "$build_cmd"
            return 0
        fi
    fi
    return 1
}

cmd_show() {
    local show_guidance=false
    local show_full=false

    # Parse flags
    while [ $# -gt 0 ]; do
        case "$1" in
            --guidance) show_guidance=true; shift ;;
            --full) show_full=true; shift ;;
            *) shift ;;
        esac
    done

    local session_id
    local mode
    local phase
    local iteration

    session_id=$(get_session)
    mode=$(get_mode)
    phase=$(get_phase)
    iteration=$(get_iteration 2>/dev/null || echo "1")

    # Check for incomplete bootstrap using new state file
    local bootstrap_state_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
    local bootstrap_complete_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json"
    local bootstrap_handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"
    local bootstrap_status=""

    if [ -f "$bootstrap_complete_file" ]; then
        bootstrap_status=$(jq -r '.status // ""' "$bootstrap_complete_file" 2>/dev/null)
    fi

    if [ "$mode" = "bootstrap" ] && [ "$bootstrap_status" != "completed" ]; then
        echo "╔══════════════════════════════════════════════════════════════════╗"
        echo "║  ⚠️  BOOTSTRAP IN PROGRESS - NOT COMPLETE                         ║"
        echo "╚══════════════════════════════════════════════════════════════════╝"
        echo ""

        # Check bootstrap state to determine progress
        local checkpoint=""
        if [ -f "$bootstrap_state_file" ]; then
            checkpoint=$(jq -r '.checkpoint // ""' "$bootstrap_state_file" 2>/dev/null)
        fi

        # Check checkpoint to determine what step to run next
        if [ "$checkpoint" = "finalize" ]; then
            # Finalize done → run spawn-subagents
            echo "✅ Infrastructure complete (finalize done)."
            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "📋 NEXT STEP: Run spawn-subagents"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "  .zcp/bootstrap.sh step spawn-subagents"
            echo ""
            echo "This outputs JSON with subagent instructions. Then spawn subagents"
            echo "via Task tool using the 'subagent_prompt' from each instruction."
            echo ""
            echo "⛔ DO NOT write code yourself - spawn subagents to do it!"

        elif [ "$checkpoint" = "spawn-subagents" ]; then
            # Spawn-subagents done → spawn subagents via Task tool, then aggregate-results
            echo "✅ Subagent instructions ready."
            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "📋 NEXT: Spawn subagents via Task tool"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "Spawn data saved to: /tmp/bootstrap_spawn.json"
            echo ""
            echo "Extract prompts and spawn:"
            echo "  cat /tmp/bootstrap_spawn.json | jq -r '.data.instructions[].subagent_prompt'"
            echo ""
            echo "For EACH instruction in .data.instructions[], use Task tool:"
            echo "  - subagent_type: \"general-purpose\""
            echo "  - prompt: <the subagent_prompt from that instruction>"
            echo "  - description: \"Bootstrap {hostname}\""
            echo ""
            echo "Launch ALL subagents in parallel (single message, multiple Task calls)."
            echo ""
            echo "After subagents complete:"
            echo "  .zcp/bootstrap.sh step aggregate-results"
            echo ""

            # Show service info from handoff
            if [ -f "$bootstrap_handoff_file" ]; then
                echo "Service pairs to bootstrap:"
                jq -r '.service_handoffs[] | "  • \(.dev_hostname) → \(.stage_hostname)"' "$bootstrap_handoff_file" 2>/dev/null
            fi

        elif [ -f "$bootstrap_handoff_file" ]; then
            # Handoff exists but checkpoint not finalize/spawn-subagents - likely aggregate-results pending
            echo "✅ Subagents should be running or complete."
            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "📋 NEXT: Check subagent completion"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "  .zcp/bootstrap.sh step aggregate-results"
            echo ""
            echo "This checks if all subagents completed successfully."
            echo "If pending, it will tell you which services are still in progress."

        elif [ -f "$bootstrap_state_file" ] && [ -n "$checkpoint" ]; then
            # Bootstrap in progress - show current step
            echo "Bootstrap at checkpoint: $checkpoint"
            echo ""

            # Check for manual work
            local dev_hostname mount_path manual_work_detected=false manual_info=""
            dev_hostname=$(jq -r '.plan.dev_hostname // "appdev"' "$bootstrap_state_file" 2>/dev/null)
            mount_path="/var/www/$dev_hostname"

            if [ -d "$mount_path" ]; then
                if [ -f "$mount_path/zerops.yml" ] || [ -f "$mount_path/zerops.yaml" ]; then
                    manual_work_detected=true
                    manual_info="zerops.yml found"
                fi
                if [ -f "$mount_path/main.go" ] || [ -f "$mount_path/index.js" ] || [ -f "$mount_path/app.py" ]; then
                    manual_work_detected=true
                    manual_info="${manual_info:+$manual_info, }application code found"
                fi
            fi

            if [ "$manual_work_detected" = true ]; then
                echo "⚠️  Manual work detected: $manual_info"
                echo ""
                echo "To validate and complete:"
                echo "   .zcp/workflow.sh bootstrap-done"
            else
                echo "To check next step:"
                echo "   .zcp/bootstrap.sh resume"
            fi
        else
            # No state file - bootstrap not started or cleared
            cat <<'BOOTSTRAP_START'
Bootstrap not started or state cleared.

To start fresh:
   .zcp/workflow.sh bootstrap --runtime <types> --services <types>

Use user's exact words for runtime and service names.
BOOTSTRAP_START
        fi
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    fi

    cat <<EOF
╔══════════════════════════════════════════════════════════════════╗
║  WORKFLOW STATUS                                                 ║
╚══════════════════════════════════════════════════════════════════╝

Session:    ${session_id:-none}
Mode:       ${mode:-none}
Phase:      ${phase:-none}
Iteration:  ${iteration}

Evidence:
EOF

    # Check discovery
    if check_evidence_session "$DISCOVERY_FILE"; then
        echo "  ✓ /tmp/discovery.json (current session)"
    elif [ -f "$DISCOVERY_FILE" ]; then
        echo "  ✗ /tmp/discovery.json (stale session)"
    else
        echo "  ✗ /tmp/discovery.json (missing)"
    fi

    # Check dev verify
    if check_evidence_session "$DEV_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        echo "  ✓ /tmp/dev_verify.json (current session, $failures failures)"
    elif [ -f "$DEV_VERIFY_FILE" ]; then
        echo "  ✗ /tmp/dev_verify.json (stale session)"
    else
        echo "  ✗ /tmp/dev_verify.json (missing)"
    fi

    # Check stage verify
    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        echo "  ✓ /tmp/stage_verify.json (current session, $failures failures)"
    elif [ -f "$STAGE_VERIFY_FILE" ]; then
        echo "  ✗ /tmp/stage_verify.json (stale session)"
    else
        echo "  ✗ /tmp/stage_verify.json (missing)"
        # Indicate if evidence was invalidated by backward transition
        if [ "$(get_phase)" = "DEVELOP" ] && [ -f "$DEV_VERIFY_FILE" ]; then
            echo "    ⚠️  Stage evidence may have been invalidated (backward transition)"
        fi
    fi

    # Check deploy evidence
    if [ -f "$DEPLOY_EVIDENCE_FILE" ] 2>/dev/null; then
        if check_evidence_session "$DEPLOY_EVIDENCE_FILE"; then
            echo "  ✓ /tmp/deploy_evidence.json (current session)"
        else
            echo "  ✗ /tmp/deploy_evidence.json (stale session)"
        fi
    fi

    # Show discovered services if discovery exists (FIX-01: Multi-service display)
    if [ -f "$DISCOVERY_FILE" ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

        # Read service count and determine display mode
        local service_count
        service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

        if [ "$service_count" -gt 1 ]; then
            # Multi-service mode: show all services
            echo "📦 DISCOVERED SERVICES ($service_count pairs)"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""

            # Gap 45: Check if deploy_order exists
            local has_deploy_order
            has_deploy_order=$(jq -r '[.services[] | select(.deploy_order)] | length' "$DISCOVERY_FILE" 2>/dev/null)

            if [ "$has_deploy_order" -gt 0 ]; then
                # Show with deploy order
                printf "  %-5s %-12s %-20s %-20s %-15s\n" "ORDER" "RUNTIME" "DEV" "STAGE" "DEPENDS ON"
                printf "  %-5s %-12s %-20s %-20s %-15s\n" "─────" "───────" "────────────────────" "────────────────────" "─────────────"
                jq -r '.services | sort_by(.deploy_order // 99) | .[] |
                    "  \(.deploy_order // "?")\t\(.runtime // "?")\t\(.dev.name // "?")\t\(.stage.name // "?")\t\(.depends_on // [] | join(","))"' \
                    "$DISCOVERY_FILE" 2>/dev/null | \
                    while IFS=$'\t' read -r order runtime dev_name stage_name deps; do
                        printf "  %-5s %-12s %-20s %-20s %-15s\n" "$order" "$runtime" "$dev_name" "$stage_name" "${deps:-—}"
                    done
            else
                # No deploy order, show standard format
                printf "  %-12s %-20s %-20s\n" "RUNTIME" "DEV" "STAGE"
                printf "  %-12s %-20s %-20s\n" "───────" "────────────────────" "────────────────────"
                jq -r '.services[] | "  \(.runtime // "?")\t\(.dev.name // "?")\t\(.stage.name // "?")"' "$DISCOVERY_FILE" 2>/dev/null | \
                    while IFS=$'\t' read -r runtime dev_name stage_name; do
                        printf "  %-12s %-20s %-20s\n" "$runtime" "$dev_name" "$stage_name"
                    done
            fi
            echo ""

            # For backward compat, also set first service vars
            local dev_name dev_id stage_name stage_id
            dev_name=$(jq -r '.services[0].dev.name // .dev.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
            dev_id=$(jq -r '.services[0].dev.id // .dev.id // "?"' "$DISCOVERY_FILE" 2>/dev/null)
            stage_name=$(jq -r '.services[0].stage.name // .stage.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
            stage_id=$(jq -r '.services[0].stage.id // .stage.id // "?"' "$DISCOVERY_FILE" 2>/dev/null)

            echo "  Primary (first): $dev_name → $stage_name"

            # Gap 48: Show internal connectivity guidance for multi-service
            echo ""
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "CONNECTION CONNECTIVITY"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "  Services communicate via internal network (vxlan):"
            echo "    http://{hostname}:{port}"
            echo ""
            echo "  Test connectivity between services:"
            echo "    .zcp/check-connectivity.sh {from} {to} {port}"
            echo ""
            echo "  Example:"
            echo "    .zcp/check-connectivity.sh apidev workerdev 8080 /health"

            # Gap 45: Show deploy order guidance if dependencies detected
            if [ "$has_deploy_order" -gt 0 ]; then
                echo ""
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                echo "📦 DEPLOY ORDER (dependencies detected)"
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                echo ""
                echo "⚠️  Deploy in this order. Wait for each before starting next:"
                echo ""

                # Generate sequential commands using process substitution to avoid subshell
                local step_num=1
                while IFS='|' read -r svc_dev_name svc_stage_id svc_stage_name; do
                    echo "Step $step_num: $svc_stage_name"
                    echo "   .zcp/deploy.sh stage $svc_dev_name"
                    echo "   .zcp/status.sh --wait $svc_stage_name"
                    echo ""
                    step_num=$((step_num + 1))
                done < <(jq -r '.services | sort_by(.deploy_order // 99) | .[] |
                    "\(.dev.name)|\(.stage.id)|\(.stage.name)"' "$DISCOVERY_FILE")
            fi
        else
            # Single-service mode: original behavior
            echo "📦 DISCOVERED SERVICES"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            local dev_name dev_id stage_name stage_id
            dev_name=$(jq -r '.dev.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
            dev_id=$(jq -r '.dev.id // "?"' "$DISCOVERY_FILE" 2>/dev/null)
            stage_name=$(jq -r '.stage.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
            stage_id=$(jq -r '.stage.id // "?"' "$DISCOVERY_FILE" 2>/dev/null)
            echo ""
            echo "  Dev:   $dev_name ($dev_id)"
            echo "  Stage: $stage_name ($stage_id)"
        fi
        echo ""
        echo "  Runtime (SSH ✓):  Use ssh {hostname} for shell access"
        echo "  Managed (NO SSH): db, cache, etc. → use psql, redis-cli from ZCP"
        echo ""
        echo "  DB access:  psql \"\$db_connectionString\""
        echo "  Variables:  \$db_hostname, \$db_user, \$db_database, \$db_connectionString"
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "💡 NEXT STEPS"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Give specific guidance based on phase and what's missing
    case "$phase" in
        INIT)
            # Check if runtime services exist (bootstrap vs standard)
            local has_runtime_services=false
            local pid
            pid=$(cat /tmp/projectId 2>/dev/null || echo "${projectId:-}")

            if [ -n "$pid" ] && command -v zcli &>/dev/null; then
                local service_check
                service_check=$(zcli service list -P "$pid" --format json 2>/dev/null | \
                    sed 's/\x1b\[[0-9;]*m//g' | \
                    awk '/^\s*[\{\[]/{found=1} found{print}' | \
                    jq '[.services[] | select(.type | test("^(go|nodejs|php|python|rust|bun|dotnet|java|nginx|static|alpine)@"))] | length' 2>/dev/null || echo "0")
                [ "$service_check" -gt 0 ] && has_runtime_services=true
            fi

            if [ "$has_runtime_services" = false ]; then
                # Bootstrap mode - no services
                cat <<'GUIDANCE'
🆕 NO RUNTIME SERVICES - Bootstrap required

   Run the bootstrap command:
   .zcp/workflow.sh bootstrap --runtime <types> --services <types>

   Use user's exact words for runtime and service names.

   This will:
   • Search recipes for patterns
   • Generate and import services
   • Wait for services to be RUNNING
   • Mount dev filesystem
   • Hand off to you for code generation

   For help: .zcp/workflow.sh bootstrap --help
GUIDANCE
            elif ! check_evidence_session "$DISCOVERY_FILE"; then
                cat <<'GUIDANCE'
Services detected. Discover them:

   .zcp/workflow.sh transition_to DISCOVER
   (Follow the guidance it outputs)
GUIDANCE
            else
                echo "Discovery exists. Run: .zcp/workflow.sh transition_to DISCOVER"
            fi
            ;;
        COMPOSE|EXTEND|SYNTHESIZE)
            cat <<'GUIDANCE'
⚠️  DEPRECATED PHASE

   The synthesis phases (COMPOSE, EXTEND, SYNTHESIZE) have been replaced.
   Use the bootstrap command instead:

   .zcp/workflow.sh bootstrap --runtime <type> --services <list>

   For help: .zcp/workflow.sh bootstrap --help

   To start fresh: .zcp/workflow.sh reset
GUIDANCE
            ;;
        DISCOVER)
            if check_evidence_session "$DISCOVERY_FILE"; then
                echo "Discovery complete. Run: .zcp/workflow.sh transition_to DEVELOP"
            else
                cat <<'GUIDANCE'
Discovery missing or stale. Re-run:
   zcli service list -P $projectId   ← -P flag required!
   .zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
GUIDANCE
            fi
            ;;
        DEVELOP)
            if ! check_evidence_session "$DEV_VERIFY_FILE"; then
                # FIX-03: Multi-service aware guidance with resolved placeholders
                local service_count
                service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

                echo "⚠️  DEVELOP PHASE REMINDERS:"
                echo "   • Server start: run_in_background=true (NOT for builds/push!)"
                echo "   • HTTP 200 ≠ working — check content, logs, console"
                echo ""

                if [ "$service_count" -gt 1 ]; then
                    # Multi-service mode: show guidance for EACH service
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo "📦 SERVICES TO DEVELOP ($service_count total)"
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo ""

                    local i=0
                    while [ "$i" -lt "$service_count" ]; do
                        local svc_dev_name svc_runtime
                        svc_dev_name=$(jq -r ".services[$i].dev.name // \"service$i\"" "$DISCOVERY_FILE" 2>/dev/null)
                        svc_runtime=$(jq -r ".services[$i].runtime // \"unknown\"" "$DISCOVERY_FILE" 2>/dev/null)

                        # Get defaults for this runtime
                        local proc binary build port
                        eval "$(get_runtime_defaults "$svc_runtime")"

                        # Try to get actual build from zerops.yml
                        local actual_build
                        if actual_build=$(get_build_from_zerops_yml "$svc_dev_name" 2>/dev/null); then
                            build="$actual_build"
                        fi

                        echo "[$((i+1))] $svc_dev_name ($svc_runtime)"
                        echo "────────────────────────────────────────────────────────────────"
                        echo "   Kill:   ssh $svc_dev_name 'pkill -9 $proc; fuser -k $port/tcp 2>/dev/null; true'"
                        echo "   Build:  ssh $svc_dev_name \"cd /var/www && $build\""
                        echo "   Run:    ssh $svc_dev_name \"cd /var/www && $binary\""
                        echo "   Verify: .zcp/verify.sh $svc_dev_name $port / /health"
                        echo ""

                        i=$((i + 1))
                    done

                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo "GATE REQUIREMENT: ALL $service_count services must pass verification"
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                else
                    # Single-service mode
                    local dev_name runtime
                    dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
                    runtime=$(jq -r '.services[0].runtime // "unknown"' "$DISCOVERY_FILE" 2>/dev/null)

                    local proc binary build port
                    eval "$(get_runtime_defaults "$runtime")"

                    local actual_build
                    if actual_build=$(get_build_from_zerops_yml "$dev_name" 2>/dev/null); then
                        build="$actual_build"
                    fi

                    echo "Service: $dev_name ($runtime)"
                    echo ""
                    echo "1. Kill existing process:"
                    echo "   ssh $dev_name 'pkill -9 $proc; fuser -k $port/tcp 2>/dev/null; true'"
                    echo ""
                    echo "2. Build and run:"
                    echo "   ssh $dev_name \"cd /var/www && $build && $binary\""
                    echo ""
                    echo "3. Verify endpoints:"
                    echo "   .zcp/verify.sh $dev_name $port / /health /status"
                fi
                echo ""
                echo "Then: .zcp/workflow.sh transition_to DEPLOY"
            else
                echo "Dev verified. Run: .zcp/workflow.sh transition_to DEPLOY"
            fi
            ;;
        DEPLOY)
            if ! check_evidence_session "$DEPLOY_EVIDENCE_FILE" 2>/dev/null; then
                # FIX-03: Multi-service aware deploy guidance
                local service_count
                service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

                echo "⚠️  DEPLOY PHASE REMINDERS:"
                echo "   • Deploy from dev container, NOT ZCP"
                echo "   • deployFiles must include ALL artifacts"
                echo ""

                if [ "$service_count" -gt 1 ]; then
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo "📦 SERVICES TO DEPLOY ($service_count total)"
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo ""

                    local i=0
                    while [ "$i" -lt "$service_count" ]; do
                        local svc_dev_name svc_stage_id svc_stage_name
                        svc_dev_name=$(jq -r ".services[$i].dev.name" "$DISCOVERY_FILE" 2>/dev/null)
                        svc_stage_id=$(jq -r ".services[$i].stage.id" "$DISCOVERY_FILE" 2>/dev/null)
                        svc_stage_name=$(jq -r ".services[$i].stage.name" "$DISCOVERY_FILE" 2>/dev/null)

                        echo "[$((i+1))] $svc_dev_name → $svc_stage_name"
                        echo "────────────────────────────────────────────────────────────────"
                        echo "   Check:  cat /var/www/$svc_dev_name/zerops.yml | grep -A10 deployFiles"
                        echo "   Deploy: .zcp/deploy.sh stage $svc_dev_name"
                        echo "   Wait:   .zcp/status.sh --wait $svc_stage_name"
                        echo ""

                        i=$((i + 1))
                    done
                else
                    local dev_name stage_id stage_name
                    dev_name=$(jq -r '.dev.name // "appdev"' "$DISCOVERY_FILE" 2>/dev/null)
                    stage_id=$(jq -r '.stage.id // "STAGE_ID"' "$DISCOVERY_FILE" 2>/dev/null)
                    stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)

                    echo "1. Check deployFiles in zerops.yml includes all artifacts:"
                    echo "   cat /var/www/$dev_name/zerops.yml | grep -A10 deployFiles"
                    echo ""
                    echo "2. Deploy to stage:"
                    echo "   .zcp/deploy.sh stage"
                    echo ""
                    echo "3. Wait for completion:"
                    echo "   .zcp/status.sh --wait $stage_name"
                fi
                echo ""
                echo "Then: .zcp/workflow.sh transition_to VERIFY"
            else
                echo "Deploy complete. Run: .zcp/workflow.sh transition_to VERIFY"
            fi
            ;;
        VERIFY)
            if ! check_evidence_session "$STAGE_VERIFY_FILE"; then
                # FIX-03: Multi-service aware verify guidance
                local service_count
                service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

                echo "⚠️  VERIFY PHASE REMINDERS:"
                echo "   • HTTP 200 ≠ working — check content, logs, console"
                echo "   • zeropsSubdomain is full URL — don't prepend https://"
                echo ""

                if [ "$service_count" -gt 1 ]; then
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo "📦 SERVICES TO VERIFY ($service_count total)"
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo ""

                    local i=0
                    while [ "$i" -lt "$service_count" ]; do
                        local svc_stage_name svc_runtime
                        svc_stage_name=$(jq -r ".services[$i].stage.name" "$DISCOVERY_FILE" 2>/dev/null)
                        svc_runtime=$(jq -r ".services[$i].runtime // \"unknown\"" "$DISCOVERY_FILE" 2>/dev/null)

                        local proc binary build port
                        eval "$(get_runtime_defaults "$svc_runtime")"

                        echo "[$((i+1))] $svc_stage_name ($svc_runtime)"
                        echo "────────────────────────────────────────────────────────────────"
                        echo "   Verify: .zcp/verify.sh $svc_stage_name $port / /health"
                        echo "   Logs:   ssh $svc_stage_name \"tail -50 /var/log/*.log\""
                        echo ""

                        i=$((i + 1))
                    done

                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo "GATE REQUIREMENT: ALL $service_count services must pass verification"
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                else
                    local stage_name runtime
                    stage_name=$(jq -r '.stage.name // "appstage"' "$DISCOVERY_FILE" 2>/dev/null)
                    runtime=$(jq -r '.services[0].runtime // "unknown"' "$DISCOVERY_FILE" 2>/dev/null)

                    local proc binary build port
                    eval "$(get_runtime_defaults "$runtime")"

                    echo "1. Verify stage endpoints:"
                    echo "   .zcp/verify.sh $stage_name $port / /health /status"
                    echo ""
                    echo "2. Browser check (if frontend):"
                    echo "   URL=\$(ssh $stage_name \"echo \\\$zeropsSubdomain\")"
                    echo "   agent-browser open \"\$URL\""
                    echo "   agent-browser errors   # Must be empty"
                fi
                echo ""
                echo "Then: .zcp/workflow.sh transition_to DONE"
            else
                echo "Stage verified. Run: .zcp/workflow.sh transition_to DONE"
            fi
            ;;
        DONE)
            cat <<'GUIDANCE'
✅ Workflow complete.

To continue working (bug fix, new feature, iteration):
   .zcp/workflow.sh iterate "description"     Start new iteration
   .zcp/workflow.sh iterate --to VERIFY       Skip to verify (no code changes)

To finish:
   .zcp/workflow.sh complete                  Mark session complete
GUIDANCE
            ;;
        QUICK)
            cat <<'GUIDANCE'
Quick mode - no workflow enforcement

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  CRITICAL RULES (you are on ZCP, not inside containers)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
• Runtime services: ssh {service} "command"
• Managed services (db, cache, etc.): NO SSH!
  Use client tools directly from ZCP:
  psql "$db_connectionString"
  redis-cli -u "$cache_connectionString"
• Variables: ${hostname}_VAR from ZCP, $VAR inside ssh
• zcli from ZCP: login first, then -P $projectId
  zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZEROPS_ZCP_API_KEY"
• Files: /var/www/{service}/ via SSHFS
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
GUIDANCE
            ;;
        *)
            echo "⛔ NO ACTIVE WORKFLOW SESSION"
            echo ""
            echo "You must start a workflow before doing anything else."
            echo ""
            echo "┌───────────────────────────────────────────────────────────────────────┐"
            echo "│  NEW PROJECT (no services yet)?                                     │"
            echo "│  → .zcp/workflow.sh bootstrap --runtime <types> --services <types>  │"
            echo "│                                                                     │"
            echo "│  EXISTING PROJECT (services already exist)?                         │"
            echo "│  → .zcp/workflow.sh init                                            │"
            echo "└───────────────────────────────────────────────────────────────────────┘"
            echo ""

            # Dynamic type listing from data.json
            local script_dir
            script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
            if [ -f "$script_dir/lib/bootstrap/resolve-types.sh" ]; then
                source "$script_dir/lib/bootstrap/resolve-types.sh"
                if ensure_data_json 2>/dev/null; then
                    local runtimes services
                    runtimes=$(jq -r '[to_entries[] | select(.value | has("base")) | .key] | sort | join(", ")' "$CACHE_FILE" 2>/dev/null)
                    services=$(jq -r '[to_entries[] | select(.value | has("base") | not) | .key] | sort | join(", ")' "$CACHE_FILE" 2>/dev/null)

                    if [ -n "$runtimes" ]; then
                        echo "Runtimes: $runtimes"
                    fi
                    if [ -n "$services" ]; then
                        echo "Services: $services"
                    fi
                    echo ""
                    echo "Use user's exact words for --runtime and --services flags."
                fi
            fi
            ;;
    esac

    # Show last error if any (automatically captured from verify/deploy failures)
    local last_error
    last_error=$(get_last_error 2>/dev/null)
    if [ -n "$last_error" ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "⚠️  LAST ERROR"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        echo "  $last_error"
    fi

    # If --full flag, show extended context
    if [ "$show_full" = true ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "📜 EXTENDED CONTEXT"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""

        # Show intent if set
        local intent
        intent=$(get_intent 2>/dev/null)
        if [ -n "$intent" ]; then
            echo "Intent: \"$intent\""
        else
            echo "Intent: (not set)"
        fi

        # Show iteration history summary
        local history_file="$WORKFLOW_STATE_DIR/iteration_history.json"
        if [ -f "$history_file" ]; then
            local iter_count
            iter_count=$(jq -r '.iterations | length' "$history_file" 2>/dev/null || echo "0")
            echo "Iteration history: $iter_count iteration(s) recorded"

            # Show last iteration summary
            local last_summary
            last_summary=$(jq -r '.iterations[-1].summary // ""' "$history_file" 2>/dev/null)
            if [ -n "$last_summary" ]; then
                echo "Current iteration: \"$last_summary\""
            fi
        fi

        # Show recent notes if any
        local notes_count
        notes_count=$(jq -r '.notes | length' "$CONTEXT_FILE" 2>/dev/null || echo "0")
        if [ "$notes_count" -gt 0 ]; then
            echo ""
            echo "Recent notes:"
            jq -r '.notes[-3:][] | "  [\(.at | split("T")[1] | split(".")[0])] \(.text)"' "$CONTEXT_FILE" 2>/dev/null
        fi

        # Show context file timestamp
        if [ -f "$CONTEXT_FILE" ]; then
            local ctx_ts
            ctx_ts=$(jq -r '.last_error.timestamp // empty' "$CONTEXT_FILE" 2>/dev/null)
            if [ -n "$ctx_ts" ]; then
                echo ""
                echo "Last context update: $ctx_ts"
            fi
        fi
    fi

    # If --guidance flag, output full phase guidance
    if [ "$show_guidance" = true ] && [ -n "$phase" ] && [ "$phase" != "NONE" ] && [ "$phase" != "QUICK" ]; then
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "📖 PHASE GUIDANCE"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        output_phase_guidance "$phase"
    fi
}

cmd_recover() {
    local critical_only=false

    # Parse flags
    while [ $# -gt 0 ]; do
        case "$1" in
            --critical|-c)
                critical_only=true
                shift
                ;;
            *)
                shift
                ;;
        esac
    done

    if [ "$critical_only" = true ]; then
        # CRITICAL MODE: Copy-paste ready commands only
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "CRITICAL RECOVERY - COPY-PASTE COMMANDS"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""

        local phase
        phase=$(get_phase 2>/dev/null || echo "NONE")
        echo "Current Phase: $phase"
        echo ""

        # Service info from discovery
        if [ -f "$DISCOVERY_FILE" ]; then
            local service_count
            service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)

            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "SERVICES ($service_count)"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""

            local i=0
            while [ "$i" -lt "$service_count" ]; do
                local dev_name dev_id stage_name stage_id
                if [ "$service_count" -eq 1 ]; then
                    dev_name=$(jq -r '.dev.name' "$DISCOVERY_FILE")
                    dev_id=$(jq -r '.dev.id' "$DISCOVERY_FILE")
                    stage_name=$(jq -r '.stage.name' "$DISCOVERY_FILE")
                    stage_id=$(jq -r '.stage.id' "$DISCOVERY_FILE")
                else
                    dev_name=$(jq -r ".services[$i].dev.name" "$DISCOVERY_FILE")
                    dev_id=$(jq -r ".services[$i].dev.id" "$DISCOVERY_FILE")
                    stage_name=$(jq -r ".services[$i].stage.name" "$DISCOVERY_FILE")
                    stage_id=$(jq -r ".services[$i].stage.id" "$DISCOVERY_FILE")
                fi

                echo "Service $((i+1)):"
                echo "  DEV:   $dev_name  (ID: $dev_id)"
                echo "  STAGE: $stage_name  (ID: $stage_id)"
                echo ""
                i=$((i + 1))
            done

            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "DEPLOYMENT COMMANDS"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo ""
            echo "# Use the deploy helper:"
            echo ".zcp/deploy.sh stage"
            echo ""
            echo "# Or manual command:"
            i=0
            while [ "$i" -lt "$service_count" ]; do
                local dev_name stage_id
                if [ "$service_count" -eq 1 ]; then
                    dev_name=$(jq -r '.dev.name' "$DISCOVERY_FILE")
                    stage_id=$(jq -r '.stage.id' "$DISCOVERY_FILE")
                else
                    dev_name=$(jq -r ".services[$i].dev.name" "$DISCOVERY_FILE")
                    stage_id=$(jq -r ".services[$i].stage.id" "$DISCOVERY_FILE")
                fi
                echo "ssh $dev_name 'cd /var/www && zcli login --region=gomibako --regionUrl=\"https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli\" \"\$ZEROPS_ZCP_API_KEY\" && zcli push $stage_id --setup=prod --deploy-git-folder'"
                echo ""
                i=$((i + 1))
            done
        else
            echo "No discovery.json found."
            echo ""
            echo "Run: .zcp/workflow.sh show"
            echo "     .zcp/workflow.sh init"
        fi

        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "VERIFICATION"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        echo ".zcp/verify.sh {service} 8080 / /health"
        echo ""
        return 0
    fi

    # FULL MODE: Original behavior
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║  FULL CONTEXT RECOVERY                                           ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""

    # Run show with guidance
    cmd_show --guidance

    # Output critical rules reminder
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "⚠️  CRITICAL RULES (always remember)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    cat <<'RULES'
• Kill orphans:     ssh {svc} 'pkill -9 {proc}; killall -9 {proc} 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true'
• Server start:     run_in_background=true (NOT for builds/push!)
• HTTP 200:         Does NOT mean working — check content, logs, console
• Deploy from:      Dev container (ssh {dev} "zcli push..."), NOT from ZCP
• deployFiles:      Must include ALL artifacts — check before every deploy
• zeropsSubdomain:  Already full URL — don't prepend https://
RULES

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "💡 For copy-paste commands only: .zcp/workflow.sh recover --critical"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

cmd_state() {
    local session_id mode phase
    session_id=$(get_session)
    mode=$(get_mode)
    phase=$(get_phase)

    local dev_name stage_name dev_verify stage_verify
    dev_name="?"
    stage_name="?"
    dev_verify="missing"
    stage_verify="missing"

    if [ -f "$DISCOVERY_FILE" ]; then
        dev_name=$(jq -r '.dev.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
        stage_name=$(jq -r '.stage.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
    fi

    if check_evidence_session "$DEV_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        dev_verify="${failures}_failures"
    fi

    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        stage_verify="${failures}_failures"
    fi

    echo "${phase:-NONE} | ${mode:-none} | dev=${dev_name} stage=${stage_name} | dev_verify=${dev_verify} stage_verify=${stage_verify}"
}

cmd_complete() {
    local session_id
    local strict_mode=false
    local skip_verify=false

    # Parse flags
    while [ $# -gt 0 ]; do
        case "$1" in
            --strict)
                strict_mode=true
                shift
                ;;
            --skip-verify)
                skip_verify=true
                shift
                ;;
            *)
                shift
                ;;
        esac
    done

    session_id=$(get_session)

    if [ -z "$session_id" ]; then
        echo "❌ No active session"
        return 1
    fi

    local all_valid=true
    local messages=()

    # Check all evidence
    if check_evidence_session "$DISCOVERY_FILE"; then
        messages+=("   • Discovery: /tmp/discovery.json ✓")
    else
        messages+=("   ✗ Discovery: /tmp/discovery.json MISSING or stale")
        all_valid=false
    fi

    if check_evidence_session "$DEV_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$DEV_VERIFY_FILE" 2>/dev/null)
        if [ "$failures" -eq 0 ]; then
            messages+=("   • Dev verify: /tmp/dev_verify.json (0 failures) ✓")
        else
            messages+=("   ✗ Dev verify: /tmp/dev_verify.json ($failures failures)")
            all_valid=false
        fi
    else
        messages+=("   ✗ Dev verify: /tmp/dev_verify.json MISSING or stale")
        all_valid=false
    fi

    if check_evidence_session "$STAGE_VERIFY_FILE"; then
        local failures
        failures=$(jq -r '.failed // 0' "$STAGE_VERIFY_FILE" 2>/dev/null)
        if [ "$failures" -eq 0 ]; then
            messages+=("   • Stage verify: /tmp/stage_verify.json (0 failures) ✓")
        else
            messages+=("   ✗ Stage verify: /tmp/stage_verify.json ($failures failures)")
            all_valid=false
        fi
    else
        messages+=("   ✗ Stage verify: /tmp/stage_verify.json MISSING or stale")
        all_valid=false
    fi

    # STRICT MODE: Actually verify endpoints are reachable
    if [ "$strict_mode" = true ] && [ "$all_valid" = true ] && [ "$skip_verify" = false ]; then
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "🔍 STRICT MODE: Live endpoint verification"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""

        if [ -f "$DISCOVERY_FILE" ]; then
            local stage_name port
            stage_name=$(jq -r '.stage.name // .services[0].stage.name // ""' "$DISCOVERY_FILE" 2>/dev/null)
            # Try to get port from stage verify file
            port=$(jq -r '.port // 8080' "$STAGE_VERIFY_FILE" 2>/dev/null)

            if [ -n "$stage_name" ]; then
                echo "   Checking $stage_name:$port..."

                # Quick connectivity check via SSH
                local check_result
                check_result=$(ssh "$stage_name" "curl -sf -o /dev/null -w '%{http_code}' http://localhost:$port/ 2>/dev/null" 2>/dev/null || echo "000")

                if [ "$check_result" = "200" ] || [ "$check_result" = "201" ] || [ "$check_result" = "204" ]; then
                    messages+=("   • Live check: $stage_name:$port → HTTP $check_result ✓")
                else
                    messages+=("   ✗ Live check: $stage_name:$port → HTTP $check_result (FAILED)")
                    all_valid=false
                    echo ""
                    echo "⚠️  Stage service is not responding correctly!"
                    echo "   Check: ssh $stage_name 'tail -50 /var/log/*.log'"
                    echo ""
                fi
            fi
        fi
    fi

    if [ "$all_valid" = true ]; then
        # Mark iteration as complete in history
        if type mark_iteration_complete &>/dev/null; then
            mark_iteration_complete
        fi

        echo "✅ Evidence validated:"
        echo "   • Session: $session_id"
        printf '%s\n' "${messages[@]}"
        echo ""
        echo "<completed>WORKFLOW_DONE</completed>"
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "📋 Next task? Run workflow.sh again to decide:"
        echo "   .zcp/workflow.sh init    → deploying"
        echo "   .zcp/workflow.sh --quick → exploring"
        return 0
    else
        echo "❌ Evidence validation failed:"
        echo ""
        echo "   • Session: $session_id"
        printf '%s\n' "${messages[@]}"
        echo ""
        echo "💡 Fix the issues above and run: .zcp/workflow.sh complete"
        if [ "$strict_mode" = false ]; then
            echo ""
            echo "💡 For stricter verification: .zcp/workflow.sh complete --strict"
        fi
        return 3
    fi
}

cmd_reset() {
    local keep_discovery=false
    if [ "$1" = "--keep-discovery" ]; then
        keep_discovery=true
    fi

    # Always clear session state and verification evidence
    rm -f "$SESSION_FILE" "$MODE_FILE" "$PHASE_FILE"
    rm -f "$DEV_VERIFY_FILE" "$STAGE_VERIFY_FILE" "$DEPLOY_EVIDENCE_FILE"

    if [ "$keep_discovery" = true ]; then
        if [ -f "$DISCOVERY_FILE" ]; then
            echo "✓ Discovery preserved"
            echo "  Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")"
            echo "  Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")"
            echo ""
            echo "💡 Next: .zcp/workflow.sh init"
            echo "   Discovery will be reused with new session"
        else
            echo "⚠️  No discovery to preserve"
            rm -f "$DISCOVERY_FILE"
            echo ""
            echo "💡 Start fresh: .zcp/workflow.sh init"
        fi
    else
        rm -f "$DISCOVERY_FILE"
        echo "✅ All workflow state cleared"
        echo ""
        echo "💡 Start fresh: .zcp/workflow.sh init"
    fi
}

# ============================================================================
# CONTEXT RECOVERY COMMAND (FIX-05)
# ============================================================================
# Outputs all recoverable context after session loss (context compaction,
# container restart). Can be sourced to restore environment variables.

cmd_context() {
    echo "# ═══════════════════════════════════════════════════════════════════"
    echo "# ZCP SESSION CONTEXT RECOVERY"
    echo "# Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "# Copy this to restore context after session loss"
    echo "# ═══════════════════════════════════════════════════════════════════"
    echo ""

    # Session info
    if [ -f "$SESSION_FILE" ]; then
        echo "# Session ID: $(cat "$SESSION_FILE")"
    fi
    if [ -f "$MODE_FILE" ]; then
        echo "# Mode: $(cat "$MODE_FILE")"
    fi
    if [ -f "$PHASE_FILE" ]; then
        echo "# Phase: $(cat "$PHASE_FILE")"
    fi
    echo ""

    # Service context
    if [ -f "$DISCOVERY_FILE" ]; then
        echo "# === DISCOVERED SERVICES ==="
        local service_count
        service_count=$(jq -r '.service_count // 1' "$DISCOVERY_FILE" 2>/dev/null)
        echo "# Service Count: $service_count"
        echo ""

        local i=0
        while [ "$i" -lt "$service_count" ]; do
            local dev_name dev_id stage_name stage_id runtime
            dev_name=$(jq -r ".services[$i].dev.name // .dev.name" "$DISCOVERY_FILE" 2>/dev/null)
            dev_id=$(jq -r ".services[$i].dev.id // .dev.id" "$DISCOVERY_FILE" 2>/dev/null)
            stage_name=$(jq -r ".services[$i].stage.name // .stage.name" "$DISCOVERY_FILE" 2>/dev/null)
            stage_id=$(jq -r ".services[$i].stage.id // .stage.id" "$DISCOVERY_FILE" 2>/dev/null)
            runtime=$(jq -r ".services[$i].runtime // \"unknown\"" "$DISCOVERY_FILE" 2>/dev/null)

            echo "# Service $((i+1)): $dev_name ($runtime)"
            echo "export SVC${i}_DEV_NAME=\"$dev_name\""
            echo "export SVC${i}_DEV_ID=\"$dev_id\""
            echo "export SVC${i}_STAGE_NAME=\"$stage_name\""
            echo "export SVC${i}_STAGE_ID=\"$stage_id\""
            echo "export SVC${i}_RUNTIME=\"$runtime\""
            echo ""

            i=$((i + 1))
        done

        # First service as default (backward compat)
        echo "# Default service (first)"
        echo "export DEV_NAME=\"$(jq -r '.dev.name // .services[0].dev.name' "$DISCOVERY_FILE")\""
        echo "export DEV_ID=\"$(jq -r '.dev.id // .services[0].dev.id' "$DISCOVERY_FILE")\""
        echo "export STAGE_NAME=\"$(jq -r '.stage.name // .services[0].stage.name' "$DISCOVERY_FILE")\""
        echo "export STAGE_ID=\"$(jq -r '.stage.id // .services[0].stage.id' "$DISCOVERY_FILE")\""
    else
        echo "# WARNING: No discovery.json found"
    fi

    echo ""
    echo "# === STANDARD ZEROPS VALUES ==="
    echo "export PORT=8080"
    echo "export SETUP_DEV=dev"
    echo "export SETUP_PROD=prod"

    # Bootstrap handoff if available
    if [ -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json" ]; then
        echo ""
        echo "# === BOOTSTRAP HANDOFF ==="
        jq -r '.service_handoffs[] | "# Mount: \(.mount_path) → \(.dev_hostname)"' \
            "${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json" 2>/dev/null
    fi

    echo ""
    echo "# === EVIDENCE STATUS ==="
    for f in dev_verify.json stage_verify.json deploy_evidence.json; do
        local fpath="${ZCP_TMP_DIR:-/tmp}/$f"
        if [ -f "$fpath" ]; then
            if check_evidence_session "$fpath" 2>/dev/null; then
                echo "# ✓ $f (valid)"
            else
                echo "# ⚠ $f (stale session)"
            fi
        else
            echo "# ✗ $f (missing)"
        fi
    done

    echo ""
    echo "# === RUNTIME DEFAULTS ==="
    echo "# Use these when runtime-specific commands are needed:"
    if [ -f "$DISCOVERY_FILE" ]; then
        local runtime
        runtime=$(jq -r '.services[0].runtime // "unknown"' "$DISCOVERY_FILE" 2>/dev/null)
        get_runtime_defaults_for_context "$runtime"
    fi
}

# Helper: Get runtime-specific defaults for context recovery
get_runtime_defaults_for_context() {
    local runtime="$1"
    case "$runtime" in
        go|go@*)
            echo "# Runtime: Go"
            echo "export PROC_NAME=app"
            echo "export BINARY_NAME=app"
            echo "export BUILD_CMD='go build -o app'"
            echo "export DEFAULT_PORT=8080"
            ;;
        nodejs|nodejs@*|node|node@*)
            echo "# Runtime: Node.js"
            echo "export PROC_NAME=node"
            echo "export BINARY_NAME='node index.js'"
            echo "export BUILD_CMD='npm install && npm run build'"
            echo "export DEFAULT_PORT=8080"
            ;;
        bun|bun@*)
            echo "# Runtime: Bun"
            echo "export PROC_NAME=bun"
            echo "export BINARY_NAME='bun run index.ts'"
            echo "export BUILD_CMD='bun install'"
            echo "export DEFAULT_PORT=8080"
            ;;
        python|python@*)
            echo "# Runtime: Python"
            echo "export PROC_NAME=python"
            echo "export BINARY_NAME='python app.py'"
            echo "export BUILD_CMD='pip install -r requirements.txt'"
            echo "export DEFAULT_PORT=8080"
            ;;
        rust|rust@*)
            echo "# Runtime: Rust"
            echo "export PROC_NAME=app"
            echo "export BINARY_NAME='./target/release/app'"
            echo "export BUILD_CMD='cargo build --release'"
            echo "export DEFAULT_PORT=8080"
            ;;
        php|php@*)
            echo "# Runtime: PHP"
            echo "export PROC_NAME=php"
            echo "export BINARY_NAME='php -S 0.0.0.0:8080'"
            echo "export BUILD_CMD='composer install'"
            echo "export DEFAULT_PORT=8080"
            ;;
        dotnet|dotnet@*)
            echo "# Runtime: .NET"
            echo "export PROC_NAME=dotnet"
            echo "export BINARY_NAME='dotnet run'"
            echo "export BUILD_CMD='dotnet build'"
            echo "export DEFAULT_PORT=8080"
            ;;
        java|java@*)
            echo "# Runtime: Java"
            echo "export PROC_NAME=java"
            echo "export BINARY_NAME='java -jar app.jar'"
            echo "export BUILD_CMD='mvn package'"
            echo "export DEFAULT_PORT=8080"
            ;;
        *)
            echo "# Runtime: Unknown ($runtime)"
            echo "export PROC_NAME=app"
            echo "export BINARY_NAME='./app'"
            echo "export BUILD_CMD='<see zerops.yml>'"
            echo "export DEFAULT_PORT=8080"
            ;;
    esac
}
