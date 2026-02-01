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
# Special handling for spawn-subagents: always show instructions from cache
# -----------------------------------------------------------------------------
emit_already_complete() {
    local step_name="$1"
    local next_step
    next_step=$(get_next_step "$step_name")

    echo ""
    echo "✓ ${step_name} already complete"
    echo ""

    # Special handling: spawn-subagents must ALWAYS show instructions
    # Agent may have missed them on first run or lost context
    if [[ "$step_name" == "spawn-subagents" ]]; then
        local spawn_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_spawn.json"
        if [[ -f "$spawn_file" ]]; then
            emit_spawn_instructions "$(cat "$spawn_file")"
            return 0
        else
            echo "⚠️  Spawn instructions file not found: $spawn_file"
            echo "   Re-run finalize then spawn-subagents:"
            echo "   .zcp/bootstrap.sh step finalize"
            echo "   .zcp/bootstrap.sh step spawn-subagents"
            echo ""
            return 0
        fi
    fi

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
# Terminal output — workflow finished, show summary and next steps
# Always shows discovery context as failsafe (survives context loss)
# -----------------------------------------------------------------------------
emit_complete() {
    local discovery_file="${ZCP_TMP_DIR:-/tmp}/discovery.json"

    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo "✓ Bootstrap complete"
    echo "═══════════════════════════════════════════════════════════════"

    # Always try to show discovery summary
    if [[ -f "$discovery_file" ]]; then
        local service_count
        service_count=$(jq -r '.service_count // 0' "$discovery_file" 2>/dev/null)

        if [[ "$service_count" -gt 0 ]]; then
            echo ""
            echo "Services ($service_count pair(s)):"
            jq -r '.services[] | "  \(.dev.name) → \(.stage.name) (\(.runtime // "?"))"' "$discovery_file" 2>/dev/null

            # Show URLs if available
            local has_urls
            has_urls=$(jq -r '[.services[].stage.url // empty] | length' "$discovery_file" 2>/dev/null)
            if [[ "$has_urls" -gt 0 ]]; then
                echo ""
                echo "Stage URLs:"
                jq -r '.services[] | select(.stage.url != "" and .stage.url != null) | "  \(.stage.name): \(.stage.url)"' "$discovery_file" 2>/dev/null
            fi
        else
            echo ""
            echo "⚠️  discovery.json exists but has no services"
            echo "   Bootstrap may not have completed properly."
        fi
    else
        echo ""
        echo "⚠️  No discovery.json found"
        echo "   Bootstrap may not have completed successfully."
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "NEXT: Start your first task"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "  .zcp/workflow.sh iterate \"description of what to build\""
    echo ""
    echo "Or check status:"
    echo "  .zcp/workflow.sh show"
    echo ""
}

# -----------------------------------------------------------------------------
# emit_resume
# Recovery output — show state + single next command
# -----------------------------------------------------------------------------
emit_resume() {
    if ! bootstrap_active; then
        echo ""
        echo "No active workflow."
        echo ""
        echo "Start → ${SCRIPT_PATH} init"
        echo ""
        return 0
    fi

    local state current_step current_name
    state=$(get_state)
    current_step=$(echo "$state" | jq -r '.current_step // 1')
    current_name=$(echo "$state" | jq -r ".steps[$((current_step - 1))].name // \"unknown\"")

    # Check if workflow is already complete
    local all_complete
    all_complete=$(echo "$state" | jq -r '[.steps[].status] | all(. == "complete")')
    if [[ "$all_complete" == "true" ]]; then
        emit_complete
        return 0
    fi

    echo ""
    echo "Resume Point: ${current_name}"
    echo ""
    echo "Completed:"
    echo "$state" | jq -r '.steps[] | select(.status == "complete") | "  ✓ \(.name)"'

    echo ""
    echo "Remaining:"
    echo "$state" | jq -r '.steps[] | select(.status != "complete") |
        if .status == "in_progress" then "  → \(.name) ← CURRENT"
        elif .status == "failed" then "  ✗ \(.name) ← FAILED"
        else "  ○ \(.name)" end'

    echo ""
    echo "Run → ${SCRIPT_PATH} step ${current_name}"
    echo ""
}

# -----------------------------------------------------------------------------
# emit_spawn_instructions <step_output_json>
# Special output for spawn-subagents step - tells agent to use Task tool
# Agent-optimized: clear keywords, structured format, no decorative noise
# -----------------------------------------------------------------------------
emit_spawn_instructions() {
    local step_output="$1"
    local count instructions

    count=$(echo "$step_output" | jq -r '.data.subagent_count // 0' 2>/dev/null)
    instructions=$(echo "$step_output" | jq -c '.data.instructions // []' 2>/dev/null)

    echo ""
    echo "ACTION_REQUIRED: SPAWN_SUBAGENTS"
    echo ""
    echo "You must now use the Task tool to spawn ${count} subagent(s)."
    echo "Do NOT run any more .zcp/bootstrap.sh commands until subagents complete."
    echo ""

    # Output specific instructions for each subagent
    local i=0
    while [[ $i -lt $count ]]; do
        local hostname runtime dev_id stage_id
        hostname=$(echo "$instructions" | jq -r ".[$i].hostname" 2>/dev/null)
        runtime=$(echo "$instructions" | jq -r ".[$i].runtime" 2>/dev/null)
        dev_id=$(echo "$instructions" | jq -r ".[$i].dev_id" 2>/dev/null)
        stage_id=$(echo "$instructions" | jq -r ".[$i].stage_id" 2>/dev/null)

        echo "SUBAGENT_$((i+1))_OF_${count}:"
        echo "  hostname: ${hostname}"
        echo "  runtime: ${runtime}"
        echo "  dev_id: ${dev_id}"
        echo "  stage_id: ${stage_id}"
        echo "  prompt_file: /tmp/subagent_prompt_${i}.txt"
        echo ""
        echo "  Task tool parameters:"
        echo "    subagent_type: general-purpose"
        echo "    description: Bootstrap ${hostname} ${runtime} service"
        echo "    prompt: <read contents of /tmp/subagent_prompt_${i}.txt>"
        echo ""

        ((i++))
    done

    echo "SEQUENCE:"
    echo "  1. Read /tmp/subagent_prompt_0.txt"
    echo "  2. Call Task tool with that prompt (subagent_type=general-purpose)"
    if [[ $count -gt 1 ]]; then
        echo "  3. Read /tmp/subagent_prompt_1.txt"
        echo "  4. Call Task tool with that prompt (subagent_type=general-purpose)"
    fi
    if [[ $count -gt 2 ]]; then
        local j=2
        local step=5
        while [[ $j -lt $count ]]; do
            echo "  ${step}. Read /tmp/subagent_prompt_${j}.txt and spawn Task"
            ((j++))
            ((step++))
        done
    fi
    echo "  $(( count * 2 + 1 )). Wait for all Task tools to return"
    echo "  $(( count * 2 + 2 )). Run: .zcp/bootstrap.sh step aggregate-results"
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
    local state current_index next_index total

    state=$(get_state)
    [[ "$state" == '{}' ]] && return

    current_index=$(echo "$state" | jq -r --arg name "$current_name" \
        '.steps | to_entries[] | select(.value.name == $name) | .key')
    total=$(echo "$state" | jq -r '.steps | length')

    [[ -z "$current_index" ]] && return

    next_index=$((current_index + 1))

    if [[ $next_index -lt $total ]]; then
        echo "$state" | jq -r ".steps[$next_index].name"
    fi
}

# -----------------------------------------------------------------------------
# get_previous_step <current_step_name>
# Returns the previous step name for gate validation
# -----------------------------------------------------------------------------
get_previous_step() {
    local current_name="$1"
    local state current_index

    state=$(get_state)
    [[ "$state" == '{}' ]] && return

    current_index=$(echo "$state" | jq -r --arg name "$current_name" \
        '.steps | to_entries[] | select(.value.name == $name) | .key')

    [[ -z "$current_index" ]] && return

    if [[ $current_index -gt 0 ]]; then
        echo "$state" | jq -r ".steps[$((current_index - 1))].name"
    fi
}

# -----------------------------------------------------------------------------
# get_step_status <step_name>
# Returns the current status of a step
# -----------------------------------------------------------------------------
get_step_status() {
    local step_name="$1"
    get_state | jq -r --arg name "$step_name" \
        '(.steps[] | select(.name == $name) | .status) // "pending"'
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
