#!/bin/bash
# Utility functions for Zerops Workflow Management System

# State files
SESSION_FILE="/tmp/claude_session"
MODE_FILE="/tmp/claude_mode"
PHASE_FILE="/tmp/claude_phase"
DISCOVERY_FILE="/tmp/discovery.json"
DEV_VERIFY_FILE="/tmp/dev_verify.json"
STAGE_VERIFY_FILE="/tmp/stage_verify.json"
DEPLOY_EVIDENCE_FILE="/tmp/deploy_evidence.json"

# Valid phases
PHASES=("INIT" "DISCOVER" "DEVELOP" "DEPLOY" "VERIFY" "DONE")

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
