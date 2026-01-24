#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # Variables used by sourced scripts

# =============================================================================
# WIGGUM - Workflow Infrastructure for Guided Gates and Unified Management
# =============================================================================
#
# State management layer for ZCP workflow orchestration.
# Handles phase transitions, evidence tracking, and state persistence.
#
# All state operations go through this file - single source of truth.
#
# Key responsibilities:
#   - Phase sequence management (standard vs synthesis flows)
#   - Workflow state JSON generation and persistence
#   - Recovery hints for blocked gates
# =============================================================================

# Ensure utils.sh variables are available
# WORKFLOW_STATE_FILE and WORKFLOW_STATE_PERSISTENT defined in utils.sh
if [ -z "$WORKFLOW_STATE_FILE" ]; then
    echo "Error: state.sh must be sourced after utils.sh" >&2
    return 1 2>/dev/null || exit 1
fi

# =============================================================================
# PHASE DEFINITIONS
# =============================================================================

# Full synthesis mode phases (in order)
PHASES_FULL_SYNTHESIS=("INIT" "COMPOSE" "EXTEND" "SYNTHESIZE" "DEVELOP" "DEPLOY" "VERIFY" "DONE")

# Standard mode (no synthesis)
PHASES_FULL_STANDARD=("INIT" "DISCOVER" "DEVELOP" "DEPLOY" "VERIFY" "DONE")

# Dev-only mode
PHASES_DEV_ONLY=("INIT" "DISCOVER" "DEVELOP" "DONE")

# Dev-only synthesis mode
PHASES_DEV_ONLY_SYNTHESIS=("INIT" "COMPOSE" "EXTEND" "SYNTHESIZE" "DEVELOP" "DONE")

# Get phase sequence for current mode
get_phase_sequence() {
    local mode="${1:-$(get_mode 2>/dev/null || echo "full")}"
    local synthesis="${2:-false}"

    if [ "$synthesis" = "true" ]; then
        if [ "$mode" = "dev-only" ]; then
            echo "${PHASES_DEV_ONLY_SYNTHESIS[@]}"
        else
            echo "${PHASES_FULL_SYNTHESIS[@]}"
        fi
    elif [ "$mode" = "dev-only" ]; then
        echo "${PHASES_DEV_ONLY[@]}"
    else
        echo "${PHASES_FULL_STANDARD[@]}"
    fi
}

# Get phase index (1-based for display)
get_phase_index() {
    local phase="$1"
    local mode="${2:-$(get_mode 2>/dev/null || echo "full")}"
    local synthesis="${3:-false}"

    local -a phases
    IFS=' ' read -ra phases <<< "$(get_phase_sequence "$mode" "$synthesis")"

    local i=1
    for p in "${phases[@]}"; do
        if [ "$p" = "$phase" ]; then
            echo "$i"
            return
        fi
        ((i++))
    done
    echo "0"
}

# Get total phases
get_total_phases() {
    local mode="${1:-$(get_mode 2>/dev/null || echo "full")}"
    local synthesis="${2:-false}"

    local -a phases
    IFS=' ' read -ra phases <<< "$(get_phase_sequence "$mode" "$synthesis")"
    echo "${#phases[@]}"
}

# Get remaining phases
get_remaining_phases() {
    local current="$1"
    local mode="${2:-$(get_mode 2>/dev/null || echo "full")}"
    local synthesis="${3:-false}"

    local -a phases
    IFS=' ' read -ra phases <<< "$(get_phase_sequence "$mode" "$synthesis")"

    local found=false
    local remaining=()

    for p in "${phases[@]}"; do
        if [ "$found" = "true" ]; then
            remaining+=("$p")
        fi
        if [ "$p" = "$current" ]; then
            found=true
        fi
    done

    echo "${remaining[@]}"
}

# =============================================================================
# PROGRESS CALCULATION
# =============================================================================

calculate_progress() {
    local phase="$1"
    local mode="${2:-$(get_mode 2>/dev/null || echo "full")}"
    local synthesis="${3:-false}"

    local index=$(get_phase_index "$phase" "$mode" "$synthesis")
    local total=$(get_total_phases "$mode" "$synthesis")

    if [ "$total" -eq 0 ]; then
        echo "0"
        return
    fi

    # Calculate percentage (phase completion = at start of phase)
    # So INIT=0%, after INIT=12.5%, etc for 8 phases
    local percent=$(( (index - 1) * 100 / total ))
    echo "$percent"
}

generate_progress_bar() {
    local percent="$1"
    local width=20
    local filled=$(( percent * width / 100 ))
    local empty=$(( width - filled ))

    local bar=""
    for ((i=0; i<filled; i++)); do bar+="█"; done
    for ((i=0; i<empty; i++)); do bar+="░"; done

    echo "$bar"
}

# =============================================================================
# EVIDENCE STATUS
# =============================================================================

# Evidence file definitions - bash 3.x compatible using functions
# get_evidence_file returns: file_path|gate_name|create_command
get_evidence_file_info() {
    local name="$1"
    local tmp="${ZCP_TMP_DIR:-/tmp}"
    case "$name" in
        recipe_review) echo "${tmp}/recipe_review.json|Gate 0|.zcp/recipe-search.sh quick" ;;
        synthesis_plan) echo "${tmp}/synthesis_plan.json|Compose|.zcp/workflow.sh compose" ;;
        synthesized_import) echo "${tmp}/synthesized_import.yml|Compose|.zcp/workflow.sh compose" ;;
        services_imported) echo "${tmp}/services_imported.json|Gate 2|.zcp/workflow.sh extend" ;;
        synthesis_complete) echo "${tmp}/synthesis_complete.json|Gate S|.zcp/workflow.sh verify_synthesis" ;;
        discovery) echo "${tmp}/discovery.json|Gate 1|.zcp/workflow.sh create_discovery" ;;
        dev_verify) echo "${tmp}/dev_verify.json|Gate 5|.zcp/verify.sh {dev}" ;;
        deploy_evidence) echo "${tmp}/deploy_evidence.json|Gate 6|.zcp/status.sh --wait" ;;
        stage_verify) echo "${tmp}/stage_verify.json|Gate 7|.zcp/verify.sh {stage}" ;;
        *) echo "" ;;
    esac
}

# List of all evidence names for iteration
EVIDENCE_NAMES="recipe_review synthesis_plan synthesized_import services_imported synthesis_complete discovery dev_verify deploy_evidence stage_verify"

get_evidence_status() {
    local name="$1"
    local info=$(get_evidence_file_info "$name")
    local file="${info%%|*}"

    if [ ! -f "$file" ]; then
        echo "pending"
        return
    fi

    # Check session match for JSON files
    if [[ "$file" == *.json ]]; then
        local session=$(get_session 2>/dev/null || echo "")
        local evidence_session=$(jq -r '.session_id // empty' "$file" 2>/dev/null)

        if [ -n "$session" ] && [ -n "$evidence_session" ] && [ "$session" != "$evidence_session" ]; then
            echo "stale"
            return
        fi

        # Check for failures in verify files
        if [[ "$name" == *"verify"* ]]; then
            local failed=$(jq -r '.failed // 0' "$file" 2>/dev/null)
            if [ "$failed" != "0" ] && [ "$failed" != "null" ]; then
                echo "failed"
                return
            fi
        fi
    fi

    echo "complete"
}

get_evidence_timestamp() {
    local name="$1"
    local info=$(get_evidence_file_info "$name")
    local file="${info%%|*}"

    if [ ! -f "$file" ]; then
        echo ""
        return
    fi

    jq -r '.timestamp // empty' "$file" 2>/dev/null
}

# =============================================================================
# NEXT ACTION DETERMINATION
# =============================================================================

# Determines next action based on current state
determine_next_action() {
    local phase=$(get_phase 2>/dev/null || echo "INIT")
    local mode=$(get_mode 2>/dev/null || echo "full")
    local synthesis=$(check_synthesis_mode)

    case "$phase" in
        "INIT")
            if [ "$(get_evidence_status recipe_review)" != "complete" ]; then
                echo '{"command":".zcp/recipe-search.sh quick {runtime} [managed-service]","description":"Review recipes and extract patterns (Gate 0)","on_success":{"next":".zcp/workflow.sh compose --runtime {runtime} --services {services}"},"on_failure":{"check":"Is the runtime type valid?","common_issues":["Invalid runtime type","Network error fetching recipes"]}}'
            else
                echo '{"command":".zcp/workflow.sh transition_to COMPOSE","description":"Transition to COMPOSE phase","on_success":{"next":"Run compose command"}}'
            fi
            ;;
        "COMPOSE")
            if [ "$(get_evidence_status synthesis_plan)" != "complete" ]; then
                echo "{\"command\":\".zcp/workflow.sh compose --runtime {runtime} --services {services}\",\"description\":\"Generate synthesis plan and import.yml\",\"on_success\":{\"next\":\".zcp/workflow.sh extend ${ZCP_TMP_DIR:-/tmp}/synthesized_import.yml\"},\"on_failure\":{\"check\":\"Are runtime and services specified?\",\"common_issues\":[\"Missing --runtime flag\",\"Invalid service type\"]}}"
            else
                echo '{"command":".zcp/workflow.sh transition_to EXTEND","description":"Transition to EXTEND phase"}'
            fi
            ;;
        "EXTEND")
            if [ "$(get_evidence_status services_imported)" != "complete" ]; then
                echo "{\"command\":\".zcp/workflow.sh extend ${ZCP_TMP_DIR:-/tmp}/synthesized_import.yml\",\"description\":\"Import services to Zerops\",\"on_success\":{\"next\":\"Create code in /var/www/{dev}/\"},\"on_failure\":{\"check\":\"Is import.yml valid?\",\"analyze\":\"cat ${ZCP_TMP_DIR:-/tmp}/synthesized_import.yml\",\"common_issues\":[\"Invalid YAML syntax\",\"Missing startWithoutCode or buildFromGit\",\"Service type not available\"]}}"
            else
                echo '{"command":"Create code files in /var/www/{dev}/","description":"Agent creates zerops.yml, main code, dependencies","type":"agent_action","files_required":["zerops.yml","main.{ext}","{deps_file}"],"on_success":{"next":".zcp/workflow.sh verify_synthesis"}}'
            fi
            ;;
        "SYNTHESIZE")
            if [ "$(get_evidence_status synthesis_complete)" != "complete" ]; then
                echo '{"command":".zcp/workflow.sh verify_synthesis","description":"Validate synthesized code structure","on_success":{"next":".zcp/workflow.sh transition_to DEVELOP"},"on_failure":{"check":"Do all required files exist?","analyze":"ls -la /var/www/{dev}/","common_issues":["Missing zerops.yml","Missing main code file","zerops.yml missing zerops: wrapper"]}}'
            else
                echo '{"command":".zcp/workflow.sh transition_to DEVELOP","description":"Transition to DEVELOP phase"}'
            fi
            ;;
        "DISCOVER")
            if [ "$(get_evidence_status discovery)" != "complete" ]; then
                echo '{"command":".zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}","description":"Record service discovery","on_success":{"next":".zcp/workflow.sh transition_to DEVELOP"},"on_failure":{"check":"Are service IDs correct?","analyze":"zcli service list -P $projectId","common_issues":["Wrong service IDs","Services not running"]}}'
            else
                echo '{"command":".zcp/workflow.sh transition_to DEVELOP","description":"Transition to DEVELOP phase"}'
            fi
            ;;
        "DEVELOP")
            if [ "$(get_evidence_status dev_verify)" != "complete" ]; then
                echo '{"command":".zcp/verify.sh {dev} 8080 / /status","description":"Verify dev endpoints work","prerequisites":["Build and start app: ssh {dev} \"go build -o app && ./app &\""],"on_success":{"next":".zcp/workflow.sh transition_to DEPLOY"},"on_failure":{"analyze":"ssh {dev} \"cat /var/log/app.log | tail -50\"","common_issues":["App not running","Database connection failed","Port conflict"]}}'
            else
                echo '{"command":".zcp/workflow.sh transition_to DEPLOY","description":"Transition to DEPLOY phase"}'
            fi
            ;;
        "DEPLOY")
            if [ "$(get_evidence_status deploy_evidence)" != "complete" ]; then
                echo '{"command":"ssh {dev} \"zcli push {stage_id} --setup=api\" && .zcp/status.sh --wait {stage}","description":"Deploy to stage and wait for completion","on_success":{"next":".zcp/workflow.sh transition_to VERIFY"},"on_failure":{"analyze":"ssh {dev} \"zcli service log {stage_id}\"","common_issues":["Build failed on stage","Missing deployFiles","Authentication expired"]}}'
            else
                echo '{"command":".zcp/workflow.sh transition_to VERIFY","description":"Transition to VERIFY phase"}'
            fi
            ;;
        "VERIFY")
            if [ "$(get_evidence_status stage_verify)" != "complete" ]; then
                echo '{"command":".zcp/verify.sh {stage} 8080 / /status","description":"Verify stage endpoints work","on_success":{"next":".zcp/workflow.sh transition_to DONE"},"on_failure":{"analyze":"Check stage logs and compare to dev","common_issues":["Different environment variables","Missing database migration","Network policy blocking"]}}'
            else
                echo '{"command":".zcp/workflow.sh transition_to DONE","description":"Transition to DONE phase"}'
            fi
            ;;
        "DONE")
            echo '{"command":".zcp/workflow.sh complete","description":"Mark workflow complete","on_success":{"output":"<completed>WORKFLOW_DONE</completed>"}}'
            ;;
        *)
            echo '{"command":".zcp/workflow.sh show","description":"Check current state"}'
            ;;
    esac
}

# Check if in synthesis mode
check_synthesis_mode() {
    if [ -f "${ZCP_TMP_DIR:-/tmp}/synthesis_plan.json" ]; then
        echo "true"
    else
        echo "false"
    fi
}

# =============================================================================
# WORKFLOW STATE GENERATION
# =============================================================================

generate_workflow_state() {
    local session=$(get_session 2>/dev/null || echo "unknown")
    local mode=$(get_mode 2>/dev/null || echo "full")
    local phase=$(get_phase 2>/dev/null || echo "INIT")
    local iteration=$(get_iteration 2>/dev/null || echo "1")
    local synthesis=$(check_synthesis_mode)
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local phase_index=$(get_phase_index "$phase" "$mode" "$synthesis")
    local total_phases=$(get_total_phases "$mode" "$synthesis")
    local progress=$(calculate_progress "$phase" "$mode" "$synthesis")
    local progress_bar=$(generate_progress_bar "$progress")
    local remaining=$(get_remaining_phases "$phase" "$mode" "$synthesis")

    local next_action=$(determine_next_action)

    # Build evidence status
    local evidence_json="{"
    local first=true
    for name in $EVIDENCE_NAMES; do
        local status=$(get_evidence_status "$name")
        local at=$(get_evidence_timestamp "$name")

        if [ "$first" = "true" ]; then
            first=false
        else
            evidence_json+=","
        fi

        evidence_json+="\"$name\":{\"status\":\"$status\""
        if [ -n "$at" ]; then
            evidence_json+=",\"at\":\"$at\""
        fi
        evidence_json+="}"
    done
    evidence_json+="}"

    # Get intent if exists (use jq for proper JSON escaping)
    local intent=""
    local tmp="${ZCP_TMP_DIR:-/tmp}"
    if [ -f "${tmp}/claude_intent.txt" ]; then
        # jq -Rs reads raw input and outputs as JSON string (with proper escaping)
        # Then strip the surrounding quotes for embedding in our JSON
        intent=$(jq -Rs '.' "${tmp}/claude_intent.txt" 2>/dev/null | sed 's/^"//;s/"$//' || echo "")
    fi

    # Get last error if exists
    local last_error="null"
    if [ -f "${tmp}/claude_context.json" ]; then
        last_error=$(jq '.last_error // null' "${tmp}/claude_context.json" 2>/dev/null || echo "null")
    fi

    # Build discovery info if exists
    local discovery_json="null"
    if [ -f "${tmp}/discovery.json" ]; then
        discovery_json=$(cat "${tmp}/discovery.json" 2>/dev/null | jq -c '.' 2>/dev/null || echo "null")
    fi

    # Build remaining array
    local remaining_array="[]"
    if [ -n "$remaining" ]; then
        remaining_array=$(echo "$remaining" | tr ' ' '\n' | grep -v '^$' | jq -R . | jq -s . 2>/dev/null || echo "[]")
    fi

    # Construct full state
    cat <<EOF
{
  "session_id": "$session",
  "mode": "$mode",
  "iteration": $iteration,
  "updated_at": "$timestamp",
  "synthesis_mode": $synthesis,

  "phase": {
    "current": "$phase",
    "index": $phase_index,
    "total": $total_phases,
    "remaining": $remaining_array
  },

  "progress": {
    "percent": $progress,
    "bar": "$progress_bar",
    "message": "$(if [ "$phase" != "DONE" ]; then echo "YOU ARE NOT DONE. $((total_phases - phase_index)) phases remaining."; else echo "Workflow complete."; fi)"
  },

  "evidence": $evidence_json,

  "discovery": $discovery_json,

  "next_action": $next_action,

  "context": {
    "intent": "$intent",
    "last_error": $last_error
  },

  "completion": {
    "signal": "<completed>WORKFLOW_DONE</completed>",
    "condition": "All evidence complete AND phase == DONE AND complete command run"
  }
}
EOF
}

# =============================================================================
# STATE OPERATIONS
# =============================================================================

# Update and persist state
update_workflow_state() {
    local state=$(generate_workflow_state)

    # Write to ephemeral
    echo "$state" | jq '.' > "$WORKFLOW_STATE_FILE" 2>/dev/null || echo "$state" > "$WORKFLOW_STATE_FILE"

    # Write to persistent
    local persistent_dir=$(dirname "$WORKFLOW_STATE_PERSISTENT")
    if mkdir -p "$persistent_dir" 2>/dev/null; then
        echo "$state" | jq '.' > "$WORKFLOW_STATE_PERSISTENT" 2>/dev/null || echo "$state" > "$WORKFLOW_STATE_PERSISTENT"
    fi
}

# Emit state to stdout (for agent consumption)
emit_workflow_state() {
    local format="${1:-full}"

    update_workflow_state

    case "$format" in
        "full")
            cat "$WORKFLOW_STATE_FILE" 2>/dev/null || generate_workflow_state
            ;;
        "compact")
            jq -c '.' "$WORKFLOW_STATE_FILE" 2>/dev/null || generate_workflow_state | jq -c '.'
            ;;
        "next")
            jq '.next_action' "$WORKFLOW_STATE_FILE" 2>/dev/null
            ;;
        "progress")
            jq '{phase: .phase.current, progress: .progress, next: .next_action.command}' "$WORKFLOW_STATE_FILE" 2>/dev/null
            ;;
    esac
}

# Read current state
read_workflow_state() {
    if [ -f "$WORKFLOW_STATE_FILE" ]; then
        cat "$WORKFLOW_STATE_FILE"
    elif [ -f "$WORKFLOW_STATE_PERSISTENT" ]; then
        cat "$WORKFLOW_STATE_PERSISTENT"
    else
        generate_workflow_state
    fi
}

# Get specific field from state
get_workflow_state_field() {
    local field="$1"
    read_workflow_state | jq -r "$field"
}

# =============================================================================
# WIGGUM DISPLAY
# =============================================================================

# Display WIGGUM-style status output
display_wiggum_status() {
    update_workflow_state

    local state=$(read_workflow_state)
    local phase=$(echo "$state" | jq -r '.phase.current')
    local progress=$(echo "$state" | jq -r '.progress.percent')
    local bar=$(echo "$state" | jq -r '.progress.bar')
    local remaining=$(echo "$state" | jq -r '.phase.remaining | length')
    local synthesis=$(echo "$state" | jq -r '.synthesis_mode')
    local next_cmd=$(echo "$state" | jq -r '.next_action.command')

    echo ""
    echo "┌─────────────────────────────────────────────────────────────────────────────┐"
    echo "│  WORKFLOW STATE                                                              │"
    echo "└─────────────────────────────────────────────────────────────────────────────┘"
    echo ""
    echo "  Phase:    $phase"
    echo "  Progress: $bar ${progress}%"
    echo "  Mode:     $(echo "$state" | jq -r '.mode')$(if [ "$synthesis" = "true" ]; then echo " (synthesis)"; fi)"
    echo ""

    if [ "$phase" != "DONE" ]; then
        echo "  ⚠️  YOU ARE NOT DONE. $remaining phases remaining."
        echo ""
        echo "  Remaining: $(echo "$state" | jq -r '.phase.remaining | join(" → ")')"
    else
        echo "  ✅ Workflow complete."
    fi

    echo ""
    echo "┌─────────────────────────────────────────────────────────────────────────────┐"
    echo "│  NEXT ACTION                                                                 │"
    echo "└─────────────────────────────────────────────────────────────────────────────┘"
    echo ""
    echo "  Command: $next_cmd"
    echo "  $(echo "$state" | jq -r '.next_action.description')"
    echo ""

    # Show JSON state
    echo "┌─────────────────────────────────────────────────────────────────────────────┐"
    echo "│  FULL STATE (JSON)                                                           │"
    echo "└─────────────────────────────────────────────────────────────────────────────┘"
    echo ""
    echo "$state" | jq '.'
}
