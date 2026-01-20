#!/bin/bash
# Utility functions for Zerops Workflow Management System

# ============================================================================
# STATE FILE PATHS
# ============================================================================

# Cache files (fast, ephemeral - /tmp/)
SESSION_FILE="/tmp/claude_session"
MODE_FILE="/tmp/claude_mode"
PHASE_FILE="/tmp/claude_phase"
ITERATION_FILE="/tmp/claude_iteration"
DISCOVERY_FILE="/tmp/discovery.json"
DEV_VERIFY_FILE="/tmp/dev_verify.json"
STAGE_VERIFY_FILE="/tmp/stage_verify.json"
DEPLOY_EVIDENCE_FILE="/tmp/deploy_evidence.json"
CONTEXT_FILE="/tmp/claude_context.json"

# Persistent storage (survives container restart)
# Default to .zcp/state (inside .zcp dir), allow override via env
STATE_DIR="${ZCP_STATE_DIR:-${SCRIPT_DIR}/state}"
WORKFLOW_STATE_DIR="$STATE_DIR/workflow"
WORKFLOW_ITERATIONS_DIR="$WORKFLOW_STATE_DIR/iterations"
PERSISTENT_ENABLED=false

# Valid phases
PHASES=("INIT" "DISCOVER" "DEVELOP" "DEPLOY" "VERIFY" "DONE")

# ============================================================================
# PERSISTENT STORAGE INITIALIZATION
# ============================================================================

init_persistent_storage() {
    if ! mkdir -p "$STATE_DIR/workflow/iterations" 2>/dev/null; then
        echo "⚠️  Warning: Cannot create persistent storage at $STATE_DIR" >&2
        echo "   Falling back to /tmp/ only (state will not survive restart)" >&2
        PERSISTENT_ENABLED=false
        return 1
    fi

    mkdir -p "$STATE_DIR/archive" 2>/dev/null

    PERSISTENT_ENABLED=true

    # Restore from persistent storage if /tmp/ is empty (e.g., after container restart)
    if [ ! -f "$SESSION_FILE" ] && [ -f "$WORKFLOW_STATE_DIR/session" ]; then
        restore_from_persistent
    fi

    return 0
}

# ============================================================================
# FILE LOCKING (BSD-Compatible)
# ============================================================================

with_lock() {
    local lockfile="${1:-.lock}"
    shift

    if command -v flock &>/dev/null; then
        # Linux: use flock
        (
            flock -x 200
            "$@"
        ) 200>"$STATE_DIR/$lockfile"
    else
        # BSD/macOS fallback: atomic mkdir
        local lock_dir="$STATE_DIR/${lockfile}.d"
        local max_wait=30
        local waited=0

        while ! mkdir "$lock_dir" 2>/dev/null; do
            sleep 0.1
            waited=$((waited + 1))
            if [ $waited -gt $((max_wait * 10)) ]; then
                echo "Lock timeout: $lockfile" >&2
                return 1
            fi
        done

        # Ensure cleanup on exit
        trap "rmdir '$lock_dir' 2>/dev/null" EXIT
        "$@"
        local rc=$?
        rmdir "$lock_dir" 2>/dev/null
        trap - EXIT
        return $rc
    fi
}

# ============================================================================
# SAFE WRITE OPERATIONS
# ============================================================================

safe_write_json() {
    local file="$1"
    local content="$2"

    local dir
    dir=$(dirname "$file")

    if ! mkdir -p "$dir" 2>/dev/null; then
        echo "ERROR: Cannot create directory $dir" >&2
        return 1
    fi

    if ! echo "$content" > "${file}.tmp" 2>/dev/null; then
        echo "ERROR: Cannot write to $file (disk full?)" >&2
        rm -f "${file}.tmp"
        return 1
    fi

    if ! mv "${file}.tmp" "$file" 2>/dev/null; then
        echo "ERROR: Cannot finalize $file" >&2
        rm -f "${file}.tmp"
        return 1
    fi

    return 0
}

# Write to both /tmp/ and persistent storage (write-through)
write_evidence() {
    local name="$1"
    local content="$2"

    # Always write to /tmp/ (fast cache)
    safe_write_json "/tmp/${name}.json" "$content" || return 1

    # Also write to persistent if available
    if [ "$PERSISTENT_ENABLED" = true ] && [ -d "$WORKFLOW_STATE_DIR" ]; then
        local persistent="$WORKFLOW_STATE_DIR/evidence/${name}.json"
        mkdir -p "$(dirname "$persistent")" 2>/dev/null
        safe_write_json "$persistent" "$content"
    fi
}

# ============================================================================
# CONTEXT CAPTURE (Automatic Error Recording)
# ============================================================================

auto_capture_context() {
    local type="$1"
    local endpoint="$2"
    local status="$3"
    local response="$4"

    local timestamp
    timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    # Truncate response to first 500 chars
    local truncated="${response:0:500}"

    local context
    context=$(jq -n \
        --arg type "$type" \
        --arg endpoint "$endpoint" \
        --arg status "$status" \
        --arg response "$truncated" \
        --arg ts "$timestamp" \
        '{
            last_error: {
                type: $type,
                endpoint: $endpoint,
                status: ($status | tonumber? // $status),
                response: $response,
                timestamp: $ts
            }
        }')

    safe_write_json "$CONTEXT_FILE" "$context"

    # Also persist if enabled
    if [ "$PERSISTENT_ENABLED" = true ] && [ -d "$WORKFLOW_STATE_DIR" ]; then
        safe_write_json "$WORKFLOW_STATE_DIR/context.json" "$context"
    fi
}

get_last_error() {
    local context_file="$CONTEXT_FILE"

    # Check /tmp/ first, then persistent
    if [ ! -f "$context_file" ] && [ -f "$WORKFLOW_STATE_DIR/context.json" ]; then
        context_file="$WORKFLOW_STATE_DIR/context.json"
    fi

    if [ -f "$context_file" ]; then
        local error_json
        error_json=$(jq -r '.last_error // empty' "$context_file" 2>/dev/null)
        if [ -n "$error_json" ] && [ "$error_json" != "null" ]; then
            local endpoint status response
            endpoint=$(echo "$error_json" | jq -r '.endpoint // ""')
            status=$(echo "$error_json" | jq -r '.status // ""')
            response=$(echo "$error_json" | jq -r '.response // ""')

            if [ -n "$endpoint" ]; then
                # Truncate response for display
                local display_response="${response:0:100}"
                [ ${#response} -gt 100 ] && display_response="${display_response}..."
                echo "$endpoint returned $status: $display_response"
            fi
        fi
    fi
}

clear_last_error() {
    rm -f "$CONTEXT_FILE"
    [ -f "$WORKFLOW_STATE_DIR/context.json" ] && rm -f "$WORKFLOW_STATE_DIR/context.json"
}

get_session() {
    if [ -f "$SESSION_FILE" ]; then
        cat "$SESSION_FILE"
    fi
}

get_mode() {
    if [ -f "$MODE_FILE" ]; then
        cat "$MODE_FILE"
    fi
}

get_phase() {
    if [ -f "$PHASE_FILE" ]; then
        cat "$PHASE_FILE"
    else
        echo "NONE"
    fi
}

set_phase() {
    echo "$1" > "$PHASE_FILE"
}

validate_phase() {
    local phase="$1"
    for p in "${PHASES[@]}"; do
        if [ "$p" = "$phase" ]; then
            return 0
        fi
    done
    return 1
}

check_evidence_session() {
    local file="$1"
    local current_session
    local evidence_session

    current_session=$(get_session)
    if [ -z "$current_session" ]; then
        return 1
    fi

    if [ ! -f "$file" ]; then
        return 1
    fi

    if ! command -v jq &>/dev/null; then
        echo "⚠️  Warning: jq not found, cannot validate evidence"
        return 0
    fi

    evidence_session=$(jq -r '.session_id // empty' "$file" 2>/dev/null)
    if [ "$evidence_session" = "$current_session" ]; then
        return 0
    fi
    return 1
}

check_evidence_freshness() {
    local file="$1"
    local max_age_hours="${2:-24}"

    if [ ! -f "$file" ]; then
        return 0  # No file = no staleness check
    fi

    local timestamp
    timestamp=$(jq -r '.timestamp // empty' "$file" 2>/dev/null)
    if [ -z "$timestamp" ]; then
        return 0  # No timestamp = can't check
    fi

    # Parse timestamp and calculate age
    local evidence_epoch now_epoch age_hours

    # Try GNU date first, then BSD date
    if evidence_epoch=$(date -d "$timestamp" +%s 2>/dev/null); then
        : # GNU date worked
    elif evidence_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$timestamp" +%s 2>/dev/null); then
        : # BSD date worked
    else
        return 0  # Can't parse = skip check
    fi

    now_epoch=$(date +%s)
    age_hours=$(( (now_epoch - evidence_epoch) / 3600 ))

    if [ "$age_hours" -gt "$max_age_hours" ]; then
        echo ""
        echo "⚠️  STALE EVIDENCE WARNING"
        echo "   File: $file"
        echo "   Age: ${age_hours} hours (threshold: ${max_age_hours}h)"
        echo "   Created: $timestamp"
        echo ""
        echo "   Consider re-verifying to ensure current system state"
        echo "   (Proceeding anyway - this is a warning, not a blocker)"
        echo ""
    fi
    return 0
}

# ============================================================================
# ITERATION MANAGEMENT
# ============================================================================

get_iteration() {
    # Check /tmp/ first
    if [ -f "$ITERATION_FILE" ]; then
        cat "$ITERATION_FILE"
        return 0
    fi

    # Check persistent storage
    if [ -f "$WORKFLOW_STATE_DIR/iteration" ]; then
        cat "$WORKFLOW_STATE_DIR/iteration"
        return 0
    fi

    # Default to 1
    echo "1"
}

set_iteration() {
    local n="$1"

    # Write to /tmp/
    echo "$n" > "$ITERATION_FILE"

    # Write to persistent storage if available
    if [ "$PERSISTENT_ENABLED" = true ] && [ -d "$WORKFLOW_STATE_DIR" ]; then
        echo "$n" > "$WORKFLOW_STATE_DIR/iteration"
    fi
}

# ============================================================================
# PERSISTENT STORAGE RESTORATION
# ============================================================================

restore_from_persistent() {
    # Restore state from persistent storage to /tmp/ cache
    # Called on startup if persistent storage has state but /tmp/ is empty

    if [ ! -d "$WORKFLOW_STATE_DIR" ]; then
        return 1
    fi

    local restored=false

    # Restore session
    if [ ! -f "$SESSION_FILE" ] && [ -f "$WORKFLOW_STATE_DIR/session" ]; then
        cp "$WORKFLOW_STATE_DIR/session" "$SESSION_FILE"
        restored=true
    fi

    # Restore mode
    if [ ! -f "$MODE_FILE" ] && [ -f "$WORKFLOW_STATE_DIR/mode" ]; then
        cp "$WORKFLOW_STATE_DIR/mode" "$MODE_FILE"
        restored=true
    fi

    # Restore phase
    if [ ! -f "$PHASE_FILE" ] && [ -f "$WORKFLOW_STATE_DIR/phase" ]; then
        cp "$WORKFLOW_STATE_DIR/phase" "$PHASE_FILE"
        restored=true
    fi

    # Restore iteration
    if [ ! -f "$ITERATION_FILE" ] && [ -f "$WORKFLOW_STATE_DIR/iteration" ]; then
        cp "$WORKFLOW_STATE_DIR/iteration" "$ITERATION_FILE"
        restored=true
    fi

    # Restore evidence files
    local evidence_dir="$WORKFLOW_STATE_DIR/evidence"
    if [ -d "$evidence_dir" ]; then
        for f in discovery dev_verify stage_verify deploy_evidence; do
            if [ ! -f "/tmp/${f}.json" ] && [ -f "$evidence_dir/${f}.json" ]; then
                cp "$evidence_dir/${f}.json" "/tmp/${f}.json"
                restored=true
            fi
        done
    fi

    # Restore context
    if [ ! -f "$CONTEXT_FILE" ] && [ -f "$WORKFLOW_STATE_DIR/context.json" ]; then
        cp "$WORKFLOW_STATE_DIR/context.json" "$CONTEXT_FILE"
        restored=true
    fi

    # Restore intent
    if [ ! -f "/tmp/claude_intent.txt" ] && [ -f "$WORKFLOW_STATE_DIR/intent.txt" ]; then
        cp "$WORKFLOW_STATE_DIR/intent.txt" "/tmp/claude_intent.txt"
        restored=true
    fi

    if [ "$restored" = true ]; then
        echo "📦 State restored from persistent storage" >&2
    fi

    return 0
}

# Sync current /tmp/ state to persistent storage
sync_to_persistent() {
    if [ "$PERSISTENT_ENABLED" != true ]; then
        return 0
    fi

    mkdir -p "$WORKFLOW_STATE_DIR/evidence" 2>/dev/null

    # Sync session, mode, phase, iteration
    [ -f "$SESSION_FILE" ] && cp "$SESSION_FILE" "$WORKFLOW_STATE_DIR/session"
    [ -f "$MODE_FILE" ] && cp "$MODE_FILE" "$WORKFLOW_STATE_DIR/mode"
    [ -f "$PHASE_FILE" ] && cp "$PHASE_FILE" "$WORKFLOW_STATE_DIR/phase"
    [ -f "$ITERATION_FILE" ] && cp "$ITERATION_FILE" "$WORKFLOW_STATE_DIR/iteration"

    # Sync evidence
    for f in discovery dev_verify stage_verify deploy_evidence; do
        [ -f "/tmp/${f}.json" ] && cp "/tmp/${f}.json" "$WORKFLOW_STATE_DIR/evidence/${f}.json"
    done

    # Sync context and intent
    [ -f "$CONTEXT_FILE" ] && cp "$CONTEXT_FILE" "$WORKFLOW_STATE_DIR/context.json"
    [ -f "/tmp/claude_intent.txt" ] && cp "/tmp/claude_intent.txt" "$WORKFLOW_STATE_DIR/intent.txt"
}
