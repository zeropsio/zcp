#!/bin/bash
# shellcheck shell=bash
# shellcheck disable=SC2034  # Variables used by sourced scripts

# Utility functions for Zerops Workflow Management System

# ============================================================================
# ZCLI HELPERS
# ============================================================================

# Strip ANSI color codes from output (zcli outputs colors that break JSON parsing)
strip_ansi() {
    sed 's/\x1b\[[0-9;]*m//g'
}

# Run zcli service list and return clean JSON
zcli_service_list_json() {
    local project_id="$1"
    zcli service list -P "$project_id" --format json 2>&1 | strip_ansi
}

# ============================================================================
# STATE FILE PATHS
# ============================================================================

# Configurable temp directory (defaults to /tmp)
ZCP_TMP_DIR="${ZCP_TMP_DIR:-/tmp}"

# Cache files (fast, ephemeral - uses ZCP_TMP_DIR)
SESSION_FILE="${ZCP_TMP_DIR}/zcp_session"
MODE_FILE="${ZCP_TMP_DIR}/zcp_mode"
PHASE_FILE="${ZCP_TMP_DIR}/zcp_phase"
ITERATION_FILE="${ZCP_TMP_DIR}/zcp_iteration"
DISCOVERY_FILE="${ZCP_TMP_DIR}/discovery.json"
DEV_VERIFY_FILE="${ZCP_TMP_DIR}/dev_verify.json"
STAGE_VERIFY_FILE="${ZCP_TMP_DIR}/stage_verify.json"
DEPLOY_EVIDENCE_FILE="${ZCP_TMP_DIR}/deploy_evidence.json"
CONTEXT_FILE="${ZCP_TMP_DIR}/zcp_context.json"

# New gate evidence files (Gates 0-3)
RECIPE_REVIEW_FILE="${ZCP_TMP_DIR}/recipe_review.json"
IMPORT_VALIDATED_FILE="${ZCP_TMP_DIR}/import_validated.json"  # Gate 0.5: Import validation
SERVICES_IMPORTED_FILE="${ZCP_TMP_DIR}/services_imported.json"
CONFIG_VALIDATED_FILE="${ZCP_TMP_DIR}/config_validated.json"

# WIGGUM state files
WORKFLOW_STATE_FILE="${ZCP_TMP_DIR}/workflow_state.json"

# Bootstrap evidence files (agent-orchestrated architecture)
BOOTSTRAP_PLAN_FILE="${ZCP_TMP_DIR}/bootstrap_plan.json"
BOOTSTRAP_IMPORT_FILE="${ZCP_TMP_DIR}/bootstrap_import.yml"
BOOTSTRAP_STATE_FILE="${ZCP_TMP_DIR}/bootstrap_state.json"
BOOTSTRAP_HANDOFF_FILE="${ZCP_TMP_DIR}/bootstrap_handoff.json"
BOOTSTRAP_COMPLETE_FILE="${ZCP_TMP_DIR}/bootstrap_complete.json"


# Persistent storage (survives container restart)
# Default to .zcp/state (inside .zcp dir), allow override via env
STATE_DIR="${ZCP_STATE_DIR:-${SCRIPT_DIR}/state}"
WORKFLOW_STATE_DIR="$STATE_DIR/workflow"
WORKFLOW_ITERATIONS_DIR="$WORKFLOW_STATE_DIR/iterations"
PERSISTENT_ENABLED=false

# Note: Phase definitions are in state.sh (PHASES_FULL_STANDARD, PHASES_DEV_ONLY)
# validate_phase() uses inline phase list for validation

# ============================================================================
# TERMINAL COLORS (shared across scripts)
# ============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
BOLD='\033[1m'
NC='\033[0m'  # No Color

# WIGGUM persistent state
WORKFLOW_STATE_PERSISTENT="$WORKFLOW_STATE_DIR/workflow_state.json"

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

        # M-19: Use subshell for better cleanup guarantee
        # This ensures lock is released even if the command is interrupted
        (
            # Save and restore original trap handlers
            _cleanup_lock() { rmdir "$lock_dir" 2>/dev/null || true; }
            trap _cleanup_lock EXIT INT TERM

            "$@"
        )
        local rc=$?
        # Extra cleanup in case subshell didn't run trap
        rmdir "$lock_dir" 2>/dev/null || true
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

    # CRITICAL-4: Use PID-unique temp file to prevent race conditions
    local tmp_file="${file}.tmp.$$"

    if ! echo "$content" > "$tmp_file" 2>/dev/null; then
        echo "ERROR: Cannot write to $file (disk full?)" >&2
        rm -f "$tmp_file"
        return 1
    fi

    if ! mv "$tmp_file" "$file" 2>/dev/null; then
        echo "ERROR: Cannot finalize $file" >&2
        rm -f "$tmp_file"
        return 1
    fi

    return 0
}

# Write to both /tmp/ and persistent storage (write-through)
write_evidence() {
    local name="$1"
    local content="$2"

    # Always write to temp cache (fast)
    safe_write_json "${ZCP_TMP_DIR}/${name}.json" "$content" || return 1

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

validate_phase() {
    local phase="$1"
    # Valid phases (standard workflow only - synthesis phases deprecated)
    case "$phase" in
        INIT|DISCOVER|DEVELOP|DEPLOY|VERIFY|DONE|QUICK)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
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

    # HIGH-17: Return failure when jq is not available (validation cannot proceed)
    if ! command -v jq &>/dev/null; then
        echo "⚠️  Warning: jq not found, cannot validate evidence - treating as invalid" >&2
        return 1
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
    local mode
    mode=$(get_mode 2>/dev/null || echo "full")

    # HIGH-18: Return failure when file doesn't exist (caller should handle)
    if [ ! -f "$file" ]; then
        echo "⚠️  Evidence file not found: $file" >&2
        return 1
    fi

    local timestamp
    timestamp=$(jq -r '.timestamp // empty' "$file" 2>/dev/null)
    # HIGH-18: Return failure when timestamp is missing
    if [ -z "$timestamp" ]; then
        echo "⚠️  Evidence file missing timestamp: $file" >&2
        return 1
    fi

    # Parse timestamp and calculate age
    local evidence_epoch now_epoch age_hours

    # Try GNU date first, then BSD date
    if evidence_epoch=$(date -d "$timestamp" +%s 2>/dev/null); then
        : # GNU date worked
    elif evidence_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$timestamp" +%s 2>/dev/null); then
        : # BSD date worked
    else
        # HIGH-18: Return failure when timestamp can't be parsed
        echo "⚠️  Cannot parse timestamp in evidence: $file" >&2
        return 1
    fi

    now_epoch=$(date +%s)
    age_hours=$(( (now_epoch - evidence_epoch) / 3600 ))

    if [ "$age_hours" -gt "$max_age_hours" ]; then
        if [ "$mode" = "hotfix" ]; then
            echo ""
            echo "⚠️  STALE EVIDENCE WARNING (hotfix mode - proceeding)"
            echo "   File: $file"
            echo "   Age: ${age_hours} hours (threshold: ${max_age_hours}h)"
            echo ""
            return 0  # Hotfix allows stale evidence
        else
            echo ""
            echo "❌ STALE EVIDENCE: $file is ${age_hours}h old (max: ${max_age_hours}h)"
            echo "   Created: $timestamp"
            echo ""
            echo "   Re-run the command to generate fresh evidence"
            echo ""
            return 1  # Block in normal mode
        fi
    fi
    return 0
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
            if [ ! -f "${ZCP_TMP_DIR}/${f}.json" ] && [ -f "$evidence_dir/${f}.json" ]; then
                cp "$evidence_dir/${f}.json" "${ZCP_TMP_DIR}/${f}.json"
                restored=true
            fi
        done
    fi

    # Restore WIGGUM workflow state
    if [ ! -f "$WORKFLOW_STATE_FILE" ] && [ -f "$WORKFLOW_STATE_PERSISTENT" ]; then
        cp "$WORKFLOW_STATE_PERSISTENT" "$WORKFLOW_STATE_FILE"
        restored=true
    fi

    # Restore context
    if [ ! -f "$CONTEXT_FILE" ] && [ -f "$WORKFLOW_STATE_DIR/context.json" ]; then
        cp "$WORKFLOW_STATE_DIR/context.json" "$CONTEXT_FILE"
        restored=true
    fi

    # Restore intent
    if [ ! -f "${ZCP_TMP_DIR}/zcp_intent.txt" ] && [ -f "$WORKFLOW_STATE_DIR/intent.txt" ]; then
        cp "$WORKFLOW_STATE_DIR/intent.txt" "${ZCP_TMP_DIR}/zcp_intent.txt"
        restored=true
    fi

    if [ "$restored" = true ]; then
        echo "📦 State restored from persistent storage" >&2
    fi

    return 0
}

# ============================================================================
# SERVICE DETECTION (Bootstrap vs Standard Flow)
# ============================================================================

# Check if runtime services exist in the project
# Returns: 0 if runtime services exist, 1 if no runtime services (need bootstrap)
# Sets global: DETECTED_SERVICES_JSON with service list
check_runtime_services_exist() {
    local pid
    pid=$(cat "${ZCP_TMP_DIR}/projectId" 2>/dev/null || echo "${projectId:-}")

    # If no project ID, can't check - assume need bootstrap
    if [ -z "$pid" ]; then
        echo "⚠️  No project ID found - cannot detect services" >&2
        return 1
    fi

    # HIGH-19: Validate project ID format to prevent injection
    if [[ ! "$pid" =~ ^[a-zA-Z0-9_-]+$ ]]; then
        echo "⚠️  Invalid project ID format: $pid" >&2
        return 1
    fi

    # Check if zcli is available
    if ! command -v zcli &>/dev/null; then
        echo "⚠️  zcli not found - cannot detect services" >&2
        return 1
    fi

    # Get service list with proper error handling
    # Strip ANSI codes that zcli outputs (breaks JSON parsing)
    local services_json
    local zcli_exit_code
    services_json=$(zcli service list -P "$pid" --format json 2>&1 | strip_ansi)
    zcli_exit_code=${PIPESTATUS[0]}

    if [ $zcli_exit_code -ne 0 ]; then
        # Check for specific error types
        if echo "$services_json" | grep -qi "unauthorized\|auth\|login\|token"; then
            echo "⚠️  zcli authentication error - run zcli login first" >&2
        elif echo "$services_json" | grep -qi "not found\|404"; then
            echo "⚠️  Project not found: $pid" >&2
        else
            echo "⚠️  zcli error: ${services_json:0:100}" >&2
        fi
        DETECTED_SERVICES_JSON="[]"
        return 1
    fi

    # Validate JSON response
    if ! echo "$services_json" | jq -e . >/dev/null 2>&1; then
        echo "⚠️  zcli returned invalid JSON" >&2
        DETECTED_SERVICES_JSON="[]"
        return 1
    fi

    # Export for use by callers
    DETECTED_SERVICES_JSON="$services_json"

    # Check if we have any runtime services (not ZCP, not managed)
    # Runtime types: go, nodejs, php, python, rust, bun, dotnet, java, nginx, static
    local runtime_count
    runtime_count=$(echo "$services_json" | jq '[(.services // [])[] | select(
        .type != null and (
            (.type | startswith("go@")) or
            (.type | startswith("nodejs@")) or
            (.type | startswith("php@")) or
            (.type | startswith("python@")) or
            (.type | startswith("rust@")) or
            (.type | startswith("bun@")) or
            (.type | startswith("dotnet@")) or
            (.type | startswith("java@")) or
            (.type | startswith("nginx@")) or
            (.type | startswith("static@")) or
            (.type | startswith("alpine@"))
        )
    )] | length' 2>/dev/null || echo "0")

    if [ "$runtime_count" -gt 0 ]; then
        return 0  # Runtime services exist - standard flow
    else
        return 1  # No runtime services - need bootstrap
    fi
}

# Get detected services summary (call after check_runtime_services_exist)
get_services_summary() {
    if [ -z "$DETECTED_SERVICES_JSON" ] || [ "$DETECTED_SERVICES_JSON" = "[]" ]; then
        echo "No services detected"
        return
    fi

    echo "$DETECTED_SERVICES_JSON" | jq -r '(.services // [])[] | "  • \(.name) (\(.type // "unknown")) - \(.status // "unknown")"' 2>/dev/null
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
        [ -f "${ZCP_TMP_DIR}/${f}.json" ] && cp "${ZCP_TMP_DIR}/${f}.json" "$WORKFLOW_STATE_DIR/evidence/${f}.json"
    done

    # Sync WIGGUM workflow state
    [ -f "$WORKFLOW_STATE_FILE" ] && cp "$WORKFLOW_STATE_FILE" "$WORKFLOW_STATE_PERSISTENT"

    # Sync context and intent
    [ -f "$CONTEXT_FILE" ] && cp "$CONTEXT_FILE" "$WORKFLOW_STATE_DIR/context.json"
    [ -f "${ZCP_TMP_DIR}/zcp_intent.txt" ] && cp "${ZCP_TMP_DIR}/zcp_intent.txt" "$WORKFLOW_STATE_DIR/intent.txt"
}
