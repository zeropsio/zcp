#!/usr/bin/env bash

# =============================================================================
# OUTPUT FORMATTING FOR AGENT CONSUMPTION
# =============================================================================
# Design principles:
#   1. Minimal text — nothing to summarize
#   2. Isolated command — single line, no surrounding prose
#   3. Visual breaks — whitespace separates sections
#   4. No numbering — prevents pattern-matching shortcuts
# =============================================================================

set -euo pipefail

WORKFLOW_STATE_FILE="${ZCP_STATE_DIR:-${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../..}/state}/workflow.json"
SCRIPT_PATH=".zcp/bootstrap.sh"

# -----------------------------------------------------------------------------
# emit_success <step_name>
# Output format for successful step completion
# -----------------------------------------------------------------------------
emit_success() {
    local step_name="$1"
    local next_step
    next_step=$(get_next_step "$step_name")

    echo ""
    echo "✓ ${step_name} complete"
    echo ""

    if [[ -n "$next_step" ]]; then
        echo "Next → ${SCRIPT_PATH} step ${next_step}"
    else
        emit_complete
    fi
    echo ""
}

# -----------------------------------------------------------------------------
# emit_already_complete <step_name>
# Output when step was already completed (idempotent re-run)
# -----------------------------------------------------------------------------
emit_already_complete() {
    local step_name="$1"
    local next_step
    next_step=$(get_next_step "$step_name")

    echo ""
    echo "✓ ${step_name} already complete"
    echo ""

    if [[ -n "$next_step" ]]; then
        echo "Next → ${SCRIPT_PATH} step ${next_step}"
    else
        emit_complete
    fi
    echo ""
}

# -----------------------------------------------------------------------------
# emit_error <step_name> <error_message> <fix_instruction>
# Output format for step failure
# -----------------------------------------------------------------------------
emit_error() {
    local step_name="$1"
    local error_message="$2"
    local fix_instruction="${3:-}"

    echo ""
    echo "✗ ${step_name} failed: ${error_message}"
    echo ""

    if [[ -n "$fix_instruction" ]]; then
        echo "Fix: ${fix_instruction}"
    fi
    echo "Then → ${SCRIPT_PATH} step ${step_name}"
    echo ""
}

# -----------------------------------------------------------------------------
# emit_complete
# Terminal output — workflow finished, no next action
# -----------------------------------------------------------------------------
emit_complete() {
    echo ""
    echo "═══════════════════════════════════════"
    echo "✓ Workflow complete"
    echo "═══════════════════════════════════════"
    echo ""
}

# -----------------------------------------------------------------------------
# emit_resume
# Recovery output — show state + single next command
# -----------------------------------------------------------------------------
emit_resume() {
    local current_step current_name total_steps

    if [[ ! -f "$WORKFLOW_STATE_FILE" ]]; then
        echo ""
        echo "No active workflow."
        echo ""
        echo "Start → ${SCRIPT_PATH} init"
        echo ""
        return 0
    fi

    current_step=$(jq -r '.current_step // 1' "$WORKFLOW_STATE_FILE")
    current_name=$(jq -r ".steps[$((current_step - 1))].name // \"unknown\"" "$WORKFLOW_STATE_FILE")
    total_steps=$(jq -r '.total_steps // 0' "$WORKFLOW_STATE_FILE")

    # Check if workflow is already complete
    local all_complete
    all_complete=$(jq -r '[.steps[].status] | all(. == "complete")' "$WORKFLOW_STATE_FILE" 2>/dev/null || echo "false")
    if [[ "$all_complete" == "true" ]]; then
        emit_complete
        return 0
    fi

    echo ""
    echo "Resume Point: ${current_name}"
    echo ""
    echo "Completed:"

    # Show completed steps
    jq -r '.steps[] | select(.status == "complete") | "  ✓ \(.name)"' "$WORKFLOW_STATE_FILE" 2>/dev/null || true

    echo ""
    echo "Remaining:"

    # Show remaining steps with current marker
    jq -r '.steps[] | select(.status != "complete") |
        if .status == "in_progress" then "  → \(.name) ← CURRENT"
        elif .status == "failed" then "  ✗ \(.name) ← FAILED"
        else "  ○ \(.name)" end' "$WORKFLOW_STATE_FILE" 2>/dev/null || true

    echo ""
    echo "Run → ${SCRIPT_PATH} step ${current_name}"
    echo ""
}

# -----------------------------------------------------------------------------
# emit_spawn_instructions <step_output_json>
# Special output for spawn-subagents step - tells agent to use Task tool
# -----------------------------------------------------------------------------
emit_spawn_instructions() {
    local step_output="$1"
    local count

    count=$(echo "$step_output" | jq -r '.data.subagent_count // 0' 2>/dev/null)

    echo ""
    echo "╔═══════════════════════════════════════════════════════════════════╗"
    echo "║  ⚠️  ACTION REQUIRED: SPAWN ${count} SUBAGENT(S)                   ║"
    echo "╠═══════════════════════════════════════════════════════════════════╣"
    echo "║                                                                   ║"
    echo "║  STOP! Do NOT run any more bootstrap steps.                       ║"
    echo "║                                                                   ║"
    echo "║  You MUST now use the Task tool to spawn ${count} subagent(s).     ║"
    echo "║                                                                   ║"
    echo "║  1. Read the prompts from: /tmp/bootstrap_spawn.json              ║"
    echo "║  2. For EACH entry in .data.instructions[], spawn a Task          ║"
    echo "║  3. Wait for ALL subagents to complete                            ║"
    echo "║  4. THEN run: .zcp/bootstrap.sh step aggregate-results            ║"
    echo "║                                                                   ║"
    echo "╚═══════════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Subagent prompts saved to: /tmp/bootstrap_spawn.json"
    echo ""
    echo "To extract prompt for subagent N (0-indexed):"
    echo "  jq -r '.data.instructions[N].subagent_prompt' /tmp/bootstrap_spawn.json"
    echo ""
    echo "Example Task tool call:"
    echo "  Task(subagent_type='general-purpose', prompt=<extracted_prompt>)"
    echo ""
}

# -----------------------------------------------------------------------------
# emit_needs_action <step_name> <step_output_json>
# Output for steps that complete but require agent action
# -----------------------------------------------------------------------------
emit_needs_action() {
    local step_name="$1"
    local step_output="$2"
    local action message

    action=$(echo "$step_output" | jq -r '.data.action_required // "See output"' 2>/dev/null)
    message=$(echo "$step_output" | jq -r '.message // "Action required"' 2>/dev/null)

    echo ""
    echo "⚠ ${step_name}: ${message}"
    echo ""
    echo "Action: ${action}"
    echo ""
}

# -----------------------------------------------------------------------------
# emit_gate_error <attempted_step> <required_step>
# Output when agent tries to skip steps
# -----------------------------------------------------------------------------
emit_gate_error() {
    local attempted="$1"
    local required="$2"

    echo ""
    echo "✗ Cannot run ${attempted}: ${required} not complete"
    echo ""
    echo "Run → ${SCRIPT_PATH} step ${required}"
    echo ""
}

# -----------------------------------------------------------------------------
# get_next_step <current_step_name>
# Returns the next step name, or empty if workflow complete
# -----------------------------------------------------------------------------
get_next_step() {
    local current_name="$1"
    local current_index next_index total

    [[ ! -f "$WORKFLOW_STATE_FILE" ]] && return

    current_index=$(jq -r --arg name "$current_name" \
        '.steps | to_entries[] | select(.value.name == $name) | .key' "$WORKFLOW_STATE_FILE" 2>/dev/null)
    total=$(jq -r '.total_steps' "$WORKFLOW_STATE_FILE" 2>/dev/null)

    [[ -z "$current_index" ]] && return

    next_index=$((current_index + 1))

    if [[ $next_index -lt $total ]]; then
        jq -r ".steps[$next_index].name" "$WORKFLOW_STATE_FILE" 2>/dev/null
    else
        echo ""
    fi
}

# -----------------------------------------------------------------------------
# get_previous_step <current_step_name>
# Returns the previous step name for gate validation
# -----------------------------------------------------------------------------
get_previous_step() {
    local current_name="$1"
    local current_index prev_index

    [[ ! -f "$WORKFLOW_STATE_FILE" ]] && return

    current_index=$(jq -r --arg name "$current_name" \
        '.steps | to_entries[] | select(.value.name == $name) | .key' "$WORKFLOW_STATE_FILE" 2>/dev/null)

    [[ -z "$current_index" ]] && return

    if [[ $current_index -gt 0 ]]; then
        prev_index=$((current_index - 1))
        jq -r ".steps[$prev_index].name" "$WORKFLOW_STATE_FILE" 2>/dev/null
    else
        echo ""
    fi
}

# -----------------------------------------------------------------------------
# get_step_status <step_name>
# Returns the current status of a step
# -----------------------------------------------------------------------------
get_step_status() {
    local step_name="$1"

    [[ ! -f "$WORKFLOW_STATE_FILE" ]] && echo "pending" && return

    jq -r --arg name "$step_name" \
        '.steps[] | select(.name == $name) | .status // "pending"' "$WORKFLOW_STATE_FILE" 2>/dev/null
}

# =============================================================================
# LEGACY JSON OUTPUT (for backward compatibility with existing steps)
# =============================================================================
# These functions allow existing step implementations to work unchanged.
# They output JSON but the main router converts to text output.
# =============================================================================

# JSON response for successful step completion
json_response() {
    local step="$1"
    local message="$2"
    local data="${3:-"{}"}"
    local next="${4:-null}"
    local checkpoint="${5:-$step}"

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    local next_field="null"
    if [[ "$next" != "null" ]] && [[ -n "$next" ]]; then
        next_field="\"$next\""
    fi

    jq -n \
        --arg status "complete" \
        --arg step "$step" \
        --arg checkpoint "$checkpoint" \
        --argjson data "$data" \
        --argjson next "$next_field" \
        --arg message "$message" \
        '{
            status: $status,
            step: $step,
            checkpoint: $checkpoint,
            data: $data,
            next: $next,
            message: $message
        }'
}

# JSON response for in-progress step
json_progress() {
    local step="$1"
    local message="$2"
    local data="${3:-"{}"}"
    local next="${4:-$step}"

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    jq -n \
        --arg status "in_progress" \
        --arg step "$step" \
        --argjson data "$data" \
        --arg next "$next" \
        --arg message "$message" \
        '{
            status: $status,
            step: $step,
            data: $data,
            next: $next,
            message: $message
        }'
}

# JSON response for step failure
json_error() {
    local step="$1"
    local error_msg="$2"
    local data="${3:-"{}"}"
    local recovery_options="${4:-"[]"}"

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    if ! echo "$recovery_options" | jq -e 'type == "array"' >/dev/null 2>&1; then
        recovery_options='[]'
    fi

    jq -n \
        --arg status "failed" \
        --arg step "$step" \
        --arg error "$error_msg" \
        --argjson data "$data" \
        --argjson recovery "$recovery_options" \
        '{
            status: $status,
            step: $step,
            data: ($data + {error: $error, recovery_options: $recovery}),
            next: null,
            message: $error
        }'
}

# JSON response for step needing user action
json_needs_action() {
    local step="$1"
    local message="$2"
    local action_required="$3"
    local data="${4:-"{}"}"

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    jq -n \
        --arg status "needs_action" \
        --arg step "$step" \
        --arg action "$action_required" \
        --argjson data "$data" \
        --arg message "$message" \
        '{
            status: $status,
            step: $step,
            data: ($data + {action_required: $action}),
            next: null,
            message: $message
        }'
}

# Extract JSON from zcli output (skips log lines before JSON, strips ANSI codes)
extract_zcli_json() {
    sed 's/\x1b\[[0-9;]*m//g' | awk '/^\s*[\{\[]/{found=1} found{print}'
}

# Safe JSON merge
json_merge() {
    local base="$1"
    local overlay="$2"

    if ! echo "$base" | jq -e . >/dev/null 2>&1; then
        base='{}'
    fi
    if ! echo "$overlay" | jq -e . >/dev/null 2>&1; then
        overlay='{}'
    fi

    echo "$base" | jq --argjson overlay "$overlay" '. + $overlay'
}

# Export all functions
export -f emit_success emit_already_complete emit_error emit_complete emit_resume
export -f emit_spawn_instructions emit_needs_action
export -f emit_gate_error get_next_step get_previous_step get_step_status
export -f json_response json_progress json_error json_needs_action extract_zcli_json json_merge
