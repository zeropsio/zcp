#!/bin/bash
# Context commands for Zerops Workflow Continuity
# intent, note commands for rich context capture

# ============================================================================
# INTENT COMMAND
# ============================================================================

cmd_intent() {
    local intent_text="$*"

    if [ -z "$intent_text" ]; then
        # Show current intent
        local current_intent
        current_intent=$(get_intent 2>/dev/null)

        if [ -n "$current_intent" ]; then
            echo "Current intent: \"$current_intent\""
        else
            echo "No intent set."
            echo ""
            echo "Usage: .zcp/workflow.sh intent \"Description of what you're trying to accomplish\""
            echo ""
            echo "Example:"
            echo "  .zcp/workflow.sh intent \"Add user authentication with JWT tokens\""
        fi
        return 0
    fi

    # Set intent
    set_intent "$intent_text"

    echo "✅ Intent set: \"$intent_text\""
    echo ""
    echo "💡 This will be displayed when you resume the workflow."
    echo "   Run: .zcp/workflow.sh show --full"
}

# ============================================================================
# NOTE COMMAND
# ============================================================================

cmd_note() {
    local note_text="$*"

    if [ -z "$note_text" ]; then
        # Show recent notes
        show_notes
        return 0
    fi

    # Add note
    add_note "$note_text"

    echo "✅ Note added: \"$note_text\""
}

# ============================================================================
# HELPER FUNCTIONS
# ============================================================================

get_intent() {
    local intent_file="$WORKFLOW_STATE_DIR/intent.txt"

    # Check /tmp/ first for backward compat
    if [ -f "/tmp/claude_intent.txt" ]; then
        cat "/tmp/claude_intent.txt"
        return 0
    fi

    # Check persistent storage
    if [ -f "$intent_file" ]; then
        cat "$intent_file"
        return 0
    fi

    return 1
}

set_intent() {
    local intent="$1"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Write to /tmp/
    echo "$intent" > "/tmp/claude_intent.txt"

    # Write to persistent storage if available
    if [ "$PERSISTENT_ENABLED" = true ] || mkdir -p "$WORKFLOW_STATE_DIR" 2>/dev/null; then
        echo "$intent" > "$WORKFLOW_STATE_DIR/intent.txt"

        # Also update manifest if it exists
        if [ -f "$WORKFLOW_STATE_DIR/manifest.json" ]; then
            jq --arg intent "$intent" --arg ts "$timestamp" \
                '.intent = $intent | .updated = $ts' \
                "$WORKFLOW_STATE_DIR/manifest.json" > "$WORKFLOW_STATE_DIR/manifest.json.tmp" && \
                mv "$WORKFLOW_STATE_DIR/manifest.json.tmp" "$WORKFLOW_STATE_DIR/manifest.json"
        fi
    fi
}

add_note() {
    local note_text="$1"
    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Build note entry
    local note_entry
    note_entry=$(jq -n --arg at "$timestamp" --arg text "$note_text" '{at: $at, text: $text}')

    # Update context file
    if [ -f "$CONTEXT_FILE" ]; then
        # Add to existing notes array
        jq --argjson note "$note_entry" \
            'if .notes then .notes += [$note] else .notes = [$note] end' \
            "$CONTEXT_FILE" > "${CONTEXT_FILE}.tmp" && \
            mv "${CONTEXT_FILE}.tmp" "$CONTEXT_FILE"
    else
        # Create new context file with note
        jq -n --argjson note "$note_entry" '{notes: [$note]}' > "$CONTEXT_FILE"
    fi

    # Also persist if enabled
    if [ "$PERSISTENT_ENABLED" = true ] && [ -d "$WORKFLOW_STATE_DIR" ]; then
        local persistent_context="$WORKFLOW_STATE_DIR/context.json"
        if [ -f "$persistent_context" ]; then
            jq --argjson note "$note_entry" \
                'if .notes then .notes += [$note] else .notes = [$note] end' \
                "$persistent_context" > "${persistent_context}.tmp" && \
                mv "${persistent_context}.tmp" "$persistent_context"
        else
            jq -n --argjson note "$note_entry" '{notes: [$note]}' > "$persistent_context"
        fi
    fi

    # Keep only last 20 notes to prevent file bloat
    cleanup_old_notes
}

show_notes() {
    local context_file="$CONTEXT_FILE"

    # Check persistent if /tmp/ doesn't have notes
    if [ ! -f "$context_file" ] && [ -f "$WORKFLOW_STATE_DIR/context.json" ]; then
        context_file="$WORKFLOW_STATE_DIR/context.json"
    fi

    if [ ! -f "$context_file" ]; then
        echo "No notes recorded."
        echo ""
        echo "Usage: .zcp/workflow.sh note \"Your note here\""
        echo ""
        echo "Example:"
        echo "  .zcp/workflow.sh note \"Token encoding might be wrong - check jwt.go:42\""
        return 0
    fi

    local notes_count
    notes_count=$(jq -r '.notes | length' "$context_file" 2>/dev/null || echo "0")

    if [ "$notes_count" -eq 0 ]; then
        echo "No notes recorded."
        return 0
    fi

    echo "📝 NOTES ($notes_count total)"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Show last 10 notes
    jq -r '.notes[-10:][] | "[\(.at | split("T")[0]) \(.at | split("T")[1] | split(".")[0] | split("Z")[0])] \(.text)"' "$context_file" 2>/dev/null
}

cleanup_old_notes() {
    # Keep only last 20 notes in context file
    if [ -f "$CONTEXT_FILE" ]; then
        local count
        count=$(jq -r '.notes | length' "$CONTEXT_FILE" 2>/dev/null || echo "0")

        if [ "$count" -gt 20 ]; then
            jq '.notes = .notes[-20:]' "$CONTEXT_FILE" > "${CONTEXT_FILE}.tmp" && \
                mv "${CONTEXT_FILE}.tmp" "$CONTEXT_FILE"
        fi
    fi

    # Same for persistent
    if [ -f "$WORKFLOW_STATE_DIR/context.json" ]; then
        local count
        count=$(jq -r '.notes | length' "$WORKFLOW_STATE_DIR/context.json" 2>/dev/null || echo "0")

        if [ "$count" -gt 20 ]; then
            jq '.notes = .notes[-20:]' "$WORKFLOW_STATE_DIR/context.json" > "$WORKFLOW_STATE_DIR/context.json.tmp" && \
                mv "$WORKFLOW_STATE_DIR/context.json.tmp" "$WORKFLOW_STATE_DIR/context.json"
        fi
    fi
}
