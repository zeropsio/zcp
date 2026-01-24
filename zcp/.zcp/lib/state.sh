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
#   - Phase sequence management (standard and dev-only flows)
#   - Workflow state JSON generation and persistence
#   - Recovery hints for blocked gates
#   - Bootstrap mode detection
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

# Bootstrap runs BEFORE the standard workflow as a pre-workflow setup step.
# After bootstrap completes, run: .zcp/workflow.sh init

# Full standard mode phases (in order)
PHASES_FULL_STANDARD=("INIT" "DISCOVER" "DEVELOP" "DEPLOY" "VERIFY" "DONE")

# Dev-only mode (shorter sequence, no stage deployment)
PHASES_DEV_ONLY=("INIT" "DISCOVER" "DEVELOP" "DONE")

# Get phase sequence for current mode
get_phase_sequence() {
    local mode="${1:-$(get_mode 2>/dev/null || echo "full")}"

    if [ "$mode" = "dev-only" ]; then
        echo "${PHASES_DEV_ONLY[@]}"
    else
        echo "${PHASES_FULL_STANDARD[@]}"
    fi
}

# Get phase index (1-based for display)
get_phase_index() {
    local phase="$1"
    local mode="${2:-$(get_mode 2>/dev/null || echo "full")}"

    local -a phases
    IFS=' ' read -ra phases <<< "$(get_phase_sequence "$mode")"

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

    local -a phases
    IFS=' ' read -ra phases <<< "$(get_phase_sequence "$mode")"
    echo "${#phases[@]}"
}

# Get remaining phases
get_remaining_phases() {
    local current="$1"
    local mode="${2:-$(get_mode 2>/dev/null || echo "full")}"

    local -a phases
    IFS=' ' read -ra phases <<< "$(get_phase_sequence "$mode")"

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

    local index=$(get_phase_index "$phase" "$mode")
    local total=$(get_total_phases "$mode")

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
        # Standard workflow evidence
        recipe_review) echo "${tmp}/recipe_review.json|Gate 0|.zcp/recipe-search.sh quick" ;;
        discovery) echo "${tmp}/discovery.json|Gate 1|.zcp/workflow.sh create_discovery" ;;
        dev_verify) echo "${tmp}/dev_verify.json|Gate 2|.zcp/verify.sh {dev}" ;;
        deploy_evidence) echo "${tmp}/deploy_evidence.json|Gate 3|.zcp/status.sh --wait" ;;
        stage_verify) echo "${tmp}/stage_verify.json|Gate 4|.zcp/verify.sh {stage}" ;;

        # Bootstrap evidence (created by bootstrap command)
        bootstrap_plan) echo "${tmp}/bootstrap_plan.json|Bootstrap|.zcp/workflow.sh bootstrap" ;;
        bootstrap_import) echo "${tmp}/bootstrap_import.yml|Bootstrap|.zcp/workflow.sh bootstrap" ;;
        bootstrap_coordination) echo "${tmp}/bootstrap_coordination.json|Bootstrap|.zcp/workflow.sh bootstrap" ;;
        bootstrap_complete) echo "${tmp}/bootstrap_complete.json|Bootstrap|.zcp/workflow.sh bootstrap" ;;
        *) echo "" ;;
    esac
}

# List of all evidence names for iteration (standard workflow only)
EVIDENCE_NAMES="recipe_review discovery dev_verify deploy_evidence stage_verify"

# Bootstrap evidence names (separate tracking)
BOOTSTRAP_EVIDENCE_NAMES="bootstrap_plan bootstrap_import bootstrap_coordination bootstrap_complete"

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

    case "$phase" in
        "INIT")
            if [ "$(get_evidence_status recipe_review)" != "complete" ]; then
                echo '{"command":".zcp/recipe-search.sh quick {runtime} [managed-service]","description":"Review recipes and extract patterns (Gate 0)","on_success":{"next":".zcp/workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}"},"on_failure":{"check":"Is the runtime type valid?","common_issues":["Invalid runtime type","Network error fetching recipes"]}}'
            else
                echo '{"command":".zcp/workflow.sh transition_to DISCOVER","description":"Transition to DISCOVER phase","on_success":{"next":"Record service discovery"}}'
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

# Check if bootstrap was run
check_bootstrap_mode() {
    if [ -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_complete.json" ]; then
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
    local bootstrap=$(check_bootstrap_mode)
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local phase_index=$(get_phase_index "$phase" "$mode")
    local total_phases=$(get_total_phases "$mode")
    local progress=$(calculate_progress "$phase" "$mode")
    local progress_bar=$(generate_progress_bar "$progress")
    local remaining=$(get_remaining_phases "$phase" "$mode")

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
  "bootstrap_completed": $bootstrap,

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
    local bootstrap=$(echo "$state" | jq -r '.bootstrap_completed')
    local next_cmd=$(echo "$state" | jq -r '.next_action.command')

    echo ""
    echo "┌─────────────────────────────────────────────────────────────────────────────┐"
    echo "│  WORKFLOW STATE                                                              │"
    echo "└─────────────────────────────────────────────────────────────────────────────┘"
    echo ""
    echo "  Phase:    $phase"
    echo "  Progress: $bar ${progress}%"
    echo "  Mode:     $(echo "$state" | jq -r '.mode')$(if [ "$bootstrap" = "true" ]; then echo " (bootstrapped)"; fi)"
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
