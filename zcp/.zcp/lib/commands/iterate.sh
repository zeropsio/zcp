#!/bin/bash
# Iterate command for Zerops Workflow Continuity
# Enables post-DONE continuation with iteration history

cmd_iterate() {
    local summary=""
    local target_phase="DEVELOP"

    # Parse arguments
    while [ $# -gt 0 ]; do
        case "$1" in
            --to)
                target_phase="$2"
                shift 2
                ;;
            *)
                summary="$1"
                shift
                ;;
        esac
    done

    # Validate target_phase
    case "$target_phase" in
        DEVELOP|DEPLOY|VERIFY)
            ;;
        *)
            echo "❌ Invalid target phase: $target_phase"
            echo "   Valid: DEVELOP, DEPLOY, VERIFY"
            return 1
            ;;
    esac

    # Validation
    local mode
    mode=$(get_mode)
    if [ "$mode" = "quick" ]; then
        echo "❌ Cannot iterate in quick mode"
        echo ""
        echo "💡 Quick mode has no workflow phases to iterate."
        echo "   If you need workflow iteration, start a full session:"
        echo "   .zcp/workflow.sh reset"
        echo "   .zcp/workflow.sh init"
        return 1
    fi

    local phase
    phase=$(get_phase)
    if [ "$phase" != "DONE" ]; then
        echo "❌ Can only iterate from DONE phase"
        echo "   Current phase: $phase"
        echo ""
        echo "💡 Complete the current workflow first:"
        echo "   .zcp/workflow.sh transition_to DONE"
        echo "   .zcp/workflow.sh complete"
        echo ""
        echo "   Then: .zcp/workflow.sh iterate \"summary\""
        return 1
    fi

    # Get current iteration (default to 1 if not set)
    local current_iteration
    current_iteration=$(get_iteration)

    # Archive current evidence
    archive_iteration_evidence "$current_iteration"

    # Increment iteration
    local next_iteration=$((current_iteration + 1))
    set_iteration "$next_iteration"

    # Record iteration in history
    record_iteration_history "$next_iteration" "$summary"

    # Cleanup old iterations (keep last 10)
    cleanup_old_iterations

    # Set phase to target
    set_phase "$target_phase"

    # Output summary
    echo "╔══════════════════════════════════════════════════════════════════╗"
    echo "║  ITERATION $next_iteration STARTED                                          ║"
    echo "╚══════════════════════════════════════════════════════════════════╝"
    echo ""

    # Show what was archived
    local archived_files=()
    [ -f "$WORKFLOW_ITERATIONS_DIR/$current_iteration/dev_verify.json" ] && archived_files+=("dev_verify.json")
    [ -f "$WORKFLOW_ITERATIONS_DIR/$current_iteration/stage_verify.json" ] && archived_files+=("stage_verify.json")
    [ -f "$WORKFLOW_ITERATIONS_DIR/$current_iteration/deploy_evidence.json" ] && archived_files+=("deploy_evidence.json")

    if [ ${#archived_files[@]} -gt 0 ]; then
        echo "📦 Archived (Iteration $current_iteration):"
        for f in "${archived_files[@]}"; do
            echo "   iterations/$current_iteration/$f"
        done
        echo ""
    fi

    # Show what's preserved
    if [ -f "$DISCOVERY_FILE" ]; then
        local dev_name stage_name
        dev_name=$(jq -r '.dev.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
        stage_name=$(jq -r '.stage.name // "?"' "$DISCOVERY_FILE" 2>/dev/null)
        echo "✅ Preserved:"
        echo "   discovery.json (dev=$dev_name, stage=$stage_name)"
        echo ""
    fi

    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "Phase: $target_phase"

    case "$target_phase" in
        DEVELOP)
            echo "Flow: DEVELOP → DEPLOY → VERIFY → DONE"
            ;;
        DEPLOY)
            echo "Flow: DEPLOY → VERIFY → DONE (skipping DEVELOP)"
            ;;
        VERIFY)
            echo "Flow: VERIFY → DONE (skipping DEVELOP, DEPLOY)"
            ;;
    esac

    if [ -n "$summary" ]; then
        echo ""
        echo "Ready to implement: \"$summary\""
    fi

    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    output_phase_guidance "$target_phase"
}

# Note: get_iteration() and set_iteration() are defined in utils.sh

# Archive evidence from current iteration
archive_iteration_evidence() {
    local n="$1"
    local iter_dir="$WORKFLOW_ITERATIONS_DIR/$n"

    # Create iteration directory
    mkdir -p "$iter_dir"

    # Move evidence files to archive
    for file in dev_verify stage_verify deploy_evidence; do
        if [ -f "/tmp/${file}.json" ]; then
            mv "/tmp/${file}.json" "$iter_dir/${file}.json"
        fi
    done

    # Also save context if available
    if [ -f "$CONTEXT_FILE" ]; then
        cp "$CONTEXT_FILE" "$iter_dir/context.json"
    fi
}

# Record iteration start in history
record_iteration_history() {
    local n="$1"
    local summary="$2"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    local history_file="$WORKFLOW_STATE_DIR/iteration_history.json"

    # Create or update history
    if [ -f "$history_file" ]; then
        # Append to existing history
        local new_entry
        new_entry=$(jq -n \
            --argjson n "$n" \
            --arg ts "$timestamp" \
            --arg sum "$summary" \
            '{n: $n, started: $ts, completed: null, summary: $sum}')

        jq --argjson entry "$new_entry" '.iterations += [$entry]' "$history_file" > "${history_file}.tmp" && \
            mv "${history_file}.tmp" "$history_file"
    else
        # Create new history file
        mkdir -p "$(dirname "$history_file")"
        jq -n \
            --argjson n "$n" \
            --arg ts "$timestamp" \
            --arg sum "$summary" \
            '{
                iterations: [{n: $n, started: $ts, completed: null, summary: $sum}]
            }' > "$history_file"
    fi
}

# Cleanup old iterations (keep last 10)
cleanup_old_iterations() {
    local iter_dir="$WORKFLOW_ITERATIONS_DIR"
    [ ! -d "$iter_dir" ] && return

    local count
    count=$(ls -1 "$iter_dir" 2>/dev/null | wc -l | tr -d ' ')

    if [ "$count" -gt 10 ]; then
        ls -1 "$iter_dir" | sort -n | head -n $((count - 10)) | while read -r old; do
            rm -rf "${iter_dir:?}/$old"
        done
    fi
}

# Mark current iteration as complete
# Called from cmd_complete when workflow reaches DONE
mark_iteration_complete() {
    local iteration
    iteration=$(get_iteration 2>/dev/null || echo "1")
    local history_file="$WORKFLOW_STATE_DIR/iteration_history.json"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    if [ -f "$history_file" ] && command -v jq &>/dev/null; then
        local updated
        updated=$(jq --arg n "$iteration" --arg ts "$timestamp" \
            '(.iterations[] | select(.n == ($n | tonumber)) | .completed) = $ts' \
            "$history_file" 2>/dev/null)
        if [ -n "$updated" ]; then
            echo "$updated" > "$history_file"
        fi
    fi
}
