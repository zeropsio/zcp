# Bootstrap State Consolidation - Implementation Guide

**Target Agent**: Claude Opus
**Estimated Phases**: 4
**Files to Modify**: 13
**Files to Delete**: 0 (workflow.json removed at runtime)

---

## Pre-Implementation Checklist

Before starting, verify these files exist:

```
zcp/.zcp/bootstrap.sh                         (507 lines)
zcp/.zcp/lib/bootstrap/state.sh               (447 lines)
zcp/.zcp/lib/bootstrap/output.sh              (450 lines)
zcp/.zcp/lib/bootstrap/steps/plan.sh
zcp/.zcp/lib/bootstrap/steps/recipe-search.sh
zcp/.zcp/lib/bootstrap/steps/generate-import.sh
zcp/.zcp/lib/bootstrap/steps/import-services.sh
zcp/.zcp/lib/bootstrap/steps/wait-services.sh
zcp/.zcp/lib/bootstrap/steps/mount-dev.sh
zcp/.zcp/lib/bootstrap/steps/discover-services.sh
zcp/.zcp/lib/bootstrap/steps/finalize.sh
zcp/.zcp/lib/bootstrap/steps/spawn-subagents.sh
zcp/.zcp/lib/bootstrap/steps/aggregate-results.sh
```

---

## Phase 1: Rewrite state.sh (Core State API)

### Objective
Replace dual state system with single unified state file.

### Target File
`zcp/.zcp/lib/bootstrap/state.sh`

### Step 1.1: Define Single State Path

**DELETE** lines 10-18 (old path definitions):
```bash
# State file paths
# Use ZCP_STATE_DIR (exported from bootstrap.sh) as primary, with fallbacks
# This ensures consistent path resolution regardless of sourcing context
_ZCP_BASE_STATE_DIR="${ZCP_STATE_DIR:-${STATE_DIR:-${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../..}/state}}"
BOOTSTRAP_STATE_DIR="${_ZCP_BASE_STATE_DIR}/bootstrap"
BOOTSTRAP_STATE_FILE="${BOOTSTRAP_STATE_DIR}/state.json"

# Workflow state file for Approach E
WORKFLOW_STATE_FILE="${_ZCP_BASE_STATE_DIR}/workflow.json"
```

**REPLACE WITH**:
```bash
# Single state file - temp directory for reliability (always writable, no SSHFS issues)
STATE_FILE="${ZCP_TMP_DIR:-/tmp}/bootstrap_state.json"
```

### Step 1.2: Write New Unified API

**DELETE** everything from line 21 to line 447 (all existing functions).

**REPLACE WITH** the following complete implementation:

```bash
# =============================================================================
# UNIFIED STATE API
# =============================================================================

# Generate UUID with fallbacks
generate_uuid() {
    if command -v uuidgen &>/dev/null; then
        uuidgen | tr '[:upper:]' '[:lower:]'
    elif [[ -f /proc/sys/kernel/random/uuid ]]; then
        cat /proc/sys/kernel/random/uuid
    elif command -v python3 &>/dev/null; then
        python3 -c 'import uuid; print(uuid.uuid4())'
    else
        echo "$(date +%s)-$$-${RANDOM}${RANDOM}"
    fi
}

# Atomic write to file
atomic_write() {
    local content="$1"
    local file="$2"
    local dir
    dir=$(dirname "$file")
    mkdir -p "$dir" 2>/dev/null || return 1
    local tmp_file="${file}.tmp.$$"
    echo "$content" > "$tmp_file" && mv "$tmp_file" "$file"
}

# -----------------------------------------------------------------------------
# BOOTSTRAP LIFECYCLE
# -----------------------------------------------------------------------------

# Initialize bootstrap with plan and step definitions
# Usage: init_bootstrap "$plan_json" "step1,step2,step3,..."
init_bootstrap() {
    local plan_data="$1"
    local steps_csv="${2:-plan,recipe-search,generate-import,import-services,wait-services,mount-dev,discover-services,finalize,spawn-subagents,aggregate-results}"

    local workflow_id session_id timestamp
    workflow_id=$(generate_uuid)
    session_id=$(generate_uuid)
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    # Build steps array
    local steps_json="[]"
    local index=1
    IFS=',' read -ra step_names <<< "$steps_csv"
    for name in "${step_names[@]}"; do
        steps_json=$(echo "$steps_json" | jq \
            --arg name "$name" \
            --argjson idx "$index" \
            '. + [{name: $name, index: $idx, status: "pending", data: null}]')
        ((index++))
    done

    local state
    state=$(jq -n \
        --arg wid "$workflow_id" \
        --arg sid "$session_id" \
        --arg ts "$timestamp" \
        --argjson plan "$plan_data" \
        --argjson steps "$steps_json" \
        '{
            workflow_id: $wid,
            session_id: $sid,
            started_at: $ts,
            current_step: 1,
            plan: $plan,
            steps: $steps,
            services: {}
        }')

    atomic_write "$state" "$STATE_FILE"
}

# Clear all bootstrap state
clear_bootstrap() {
    rm -f "$STATE_FILE" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_plan.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_import.yml" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/recipe_review.json" 2>/dev/null || true
    rm -f "${ZCP_TMP_DIR:-/tmp}/service_discovery.json" 2>/dev/null || true
    # Clean up per-service completion files
    rm -f "${ZCP_TMP_DIR:-/tmp}"/*_complete.json 2>/dev/null || true
}

# -----------------------------------------------------------------------------
# STATE READERS
# -----------------------------------------------------------------------------

# Get entire state object
get_state() {
    if [[ -f "$STATE_FILE" ]]; then
        cat "$STATE_FILE"
    else
        echo '{}'
    fi
}

# Check if bootstrap is active
bootstrap_active() {
    [[ -f "$STATE_FILE" ]]
}

# Get plan from state
get_plan() {
    get_state | jq '.plan // {}'
}

# Get step object by name
get_step() {
    local name="$1"
    get_state | jq --arg n "$name" '.steps[] | select(.name == $n)'
}

# Get step data by name
get_step_data() {
    local name="$1"
    get_state | jq --arg n "$name" '(.steps[] | select(.name == $n) | .data) // {}'
}

# Check if step is complete
is_step_complete() {
    local name="$1"
    local status
    status=$(get_state | jq -r --arg n "$name" '(.steps[] | select(.name == $n) | .status) // "pending"')
    [[ "$status" == "complete" ]]
}

# Get next pending step name
get_next_step() {
    get_state | jq -r '(.steps[] | select(.status != "complete") | .name) // empty' | head -1
}

# Get current step index
get_current_step_index() {
    get_state | jq -r '.current_step // 1'
}

# -----------------------------------------------------------------------------
# STATE WRITERS
# -----------------------------------------------------------------------------

# Set step status (pending, in_progress, complete, failed)
set_step_status() {
    local name="$1"
    local status="$2"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    local state
    state=$(get_state)

    local ts_field
    case "$status" in
        in_progress) ts_field="started_at" ;;
        complete) ts_field="completed_at" ;;
        failed) ts_field="failed_at" ;;
        *) ts_field="" ;;
    esac

    if [[ -n "$ts_field" ]]; then
        state=$(echo "$state" | jq \
            --arg n "$name" \
            --arg s "$status" \
            --arg ts "$timestamp" \
            --arg tf "$ts_field" \
            '.steps |= map(if .name == $n then .status = $s | .[$tf] = $ts else . end)')
    else
        state=$(echo "$state" | jq \
            --arg n "$name" \
            --arg s "$status" \
            '.steps |= map(if .name == $n then .status = $s else . end)')
    fi

    # Update current_step pointer
    local new_index
    new_index=$(echo "$state" | jq --arg n "$name" '.steps | to_entries | map(select(.value.name == $n)) | .[0].key + 1')
    state=$(echo "$state" | jq --argjson idx "$new_index" '.current_step = $idx')

    atomic_write "$state" "$STATE_FILE"
}

# Set step data
set_step_data() {
    local name="$1"
    local data="$2"

    # Validate JSON
    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    local state
    state=$(get_state)
    state=$(echo "$state" | jq \
        --arg n "$name" \
        --argjson d "$data" \
        '.steps |= map(if .name == $n then .data = $d else . end)')

    atomic_write "$state" "$STATE_FILE"
}

# Combined: set status and data atomically
complete_step() {
    local name="$1"
    local data="${2:-"{}"}"
    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    local state
    state=$(get_state)
    state=$(echo "$state" | jq \
        --arg n "$name" \
        --arg ts "$timestamp" \
        --argjson d "$data" \
        '.steps |= map(if .name == $n then .status = "complete" | .completed_at = $ts | .data = $d else . end)')

    atomic_write "$state" "$STATE_FILE"
}

# Update plan
update_plan() {
    local plan_update="$1"
    local state
    state=$(get_state)
    state=$(echo "$state" | jq --argjson u "$plan_update" '.plan = (.plan + $u)')
    atomic_write "$state" "$STATE_FILE"
}

# -----------------------------------------------------------------------------
# SERVICE STATE (for subagents)
# -----------------------------------------------------------------------------

set_service_status() {
    local hostname="$1"
    local status="$2"
    local data="${3:-"{}"}"

    if ! echo "$data" | jq -e . >/dev/null 2>&1; then
        data='{}'
    fi

    local timestamp
    timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

    local state
    state=$(get_state)
    state=$(echo "$state" | jq \
        --arg h "$hostname" \
        --arg s "$status" \
        --arg ts "$timestamp" \
        --argjson d "$data" \
        '.services[$h] = {status: $s, updated_at: $ts, data: $d}')

    atomic_write "$state" "$STATE_FILE"
}

get_service_status() {
    local hostname="$1"
    get_state | jq --arg h "$hostname" '.services[$h] // {status: "unknown"}'
}

get_all_services() {
    get_state | jq '.services // {}'
}

# -----------------------------------------------------------------------------
# STATUS DISPLAY
# -----------------------------------------------------------------------------

generate_status_json() {
    local state
    state=$(get_state)

    if [[ "$state" == '{}' ]]; then
        jq -n '{active: false, message: "No bootstrap in progress"}'
        return
    fi

    echo "$state" | jq '{
        active: true,
        workflow_id: .workflow_id,
        current_step: .current_step,
        total_steps: (.steps | length),
        steps_complete: ([.steps[] | select(.status == "complete")] | length),
        started_at: .started_at,
        plan: .plan,
        services: .services
    }'
}

# -----------------------------------------------------------------------------
# EXPORTS
# -----------------------------------------------------------------------------

export STATE_FILE
export -f generate_uuid atomic_write
export -f init_bootstrap clear_bootstrap
export -f get_state bootstrap_active get_plan get_step get_step_data
export -f is_step_complete get_next_step get_current_step_index
export -f set_step_status set_step_data complete_step update_plan
export -f set_service_status get_service_status get_all_services
export -f generate_status_json
```

### Step 1.3: Verify Phase 1

Run this command to verify syntax:
```bash
bash -n zcp/.zcp/lib/bootstrap/state.sh && echo "PHASE 1 COMPLETE: state.sh syntax valid"
```

---

## Phase 2: Update bootstrap.sh (Orchestrator)

### Target File
`zcp/.zcp/lib/bootstrap/state.sh`

### Step 2.1: Remove init_workflow_state()

**DELETE** lines 97-126 (the entire `init_workflow_state` function):
```bash
# Initialize workflow.json from BOOTSTRAP_STEPS array
init_workflow_state() {
    # ... entire function
}
```

### Step 2.2: Update cmd_step() - Remove Double Recording

**FIND** in `cmd_step()` (around lines 210-226), the section that handles successful step completion:

```bash
        if [[ "$status" == "complete" ]]; then
            update_step_status "$step_name" "complete"
            record_step "$step_name" "complete" "$(echo "$step_output" | jq '.data // {}' 2>/dev/null || echo '{}')"
```

**REPLACE WITH**:
```bash
        if [[ "$status" == "complete" ]]; then
            complete_step "$step_name" "$(echo "$step_output" | jq '.data // {}' 2>/dev/null || echo '{}')"
```

**FIND** the `needs_action` handler (around line 234-238):
```bash
        elif [[ "$status" == "needs_action" ]]; then
            # Step completed but requires agent action (e.g., spawn subagents)
            update_step_status "$step_name" "complete"
            record_step "$step_name" "complete" "$(echo "$step_output" | jq '.data // {}' 2>/dev/null || echo '{}')"
```

**REPLACE WITH**:
```bash
        elif [[ "$status" == "needs_action" ]]; then
            complete_step "$step_name" "$(echo "$step_output" | jq '.data // {}' 2>/dev/null || echo '{}')"
```

### Step 2.3: Update cmd_init()

**FIND** line 339:
```bash
    init_workflow_state
```

**DELETE** that line entirely.

**FIND** lines 358-359:
```bash
            update_step_status "plan" "complete"
            record_step "plan" "complete" "$(echo "$plan_output" | jq '.data // {}' 2>/dev/null || echo '{}')"
```

**REPLACE WITH**:
```bash
            # plan.sh handles its own state via init_bootstrap() + complete_step()
```

### Step 2.4: Update cmd_status()

**FIND** the `cmd_status()` function (around lines 275-293).

**REPLACE** the entire function with:
```bash
cmd_status() {
    if ! bootstrap_active; then
        echo "No active workflow."
        echo ""
        echo "Start with: .zcp/bootstrap.sh init --runtime <type>"
        exit 0
    fi

    echo "=== Workflow Status ==="
    echo ""
    get_state | jq -r '
        "Workflow ID: \(.workflow_id)",
        "Started: \(.started_at)",
        "Progress: \(.current_step)/\(.steps | length)",
        "",
        "Steps:",
        (.steps[] | "  [\(.status | if . == "complete" then "✓" elif . == "in_progress" then "→" elif . == "failed" then "✗" else " " end)] \(.name)")
    '
}
```

### Step 2.5: Update cmd_done()

**FIND** line 403:
```bash
    all_complete=$(jq -r '[.steps[].status] | all(. == "complete")' "$ZCP_STATE_DIR/workflow.json" 2>/dev/null || echo "false")
```

**REPLACE WITH**:
```bash
    all_complete=$(get_state | jq -r '[.steps[].status] | all(. == "complete")')
```

### Step 2.6: Update cmd_reset()

**FIND** the `cmd_reset()` function and ensure it calls `clear_bootstrap`:
```bash
cmd_reset() {
    clear_bootstrap
    echo ""
    echo "✓ Workflow state cleared"
    echo ""
    echo "Start → .zcp/bootstrap.sh init --runtime <type>"
    echo ""
}
```

### Step 2.7: Verify Phase 2

```bash
bash -n zcp/.zcp/bootstrap.sh && echo "PHASE 2 COMPLETE: bootstrap.sh syntax valid"
```

---

## Phase 3: Update output.sh

### Target File
`zcp/.zcp/lib/bootstrap/output.sh`

### Step 3.1: Remove WORKFLOW_STATE_FILE Reference

**DELETE** line 15:
```bash
WORKFLOW_STATE_FILE="${ZCP_STATE_DIR:-${SCRIPT_DIR:-$(dirname "${BASH_SOURCE[0]}")/../..}/state}/workflow.json"
```

### Step 3.2: Update emit_resume()

**FIND** the `emit_resume()` function (lines 96-140).

**REPLACE** entirely with:
```bash
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
```

### Step 3.3: Update get_next_step()

**FIND** the `get_next_step()` function (lines 245-264).

**REPLACE** entirely with:
```bash
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
```

### Step 3.4: Update get_previous_step()

**REPLACE** the `get_previous_step()` function with:
```bash
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
```

### Step 3.5: Update get_step_status()

**REPLACE** the `get_step_status()` function with:
```bash
get_step_status() {
    local step_name="$1"
    get_state | jq -r --arg name "$step_name" \
        '(.steps[] | select(.name == $name) | .status) // "pending"'
}
```

### Step 3.6: Verify Phase 3

```bash
bash -n zcp/.zcp/lib/bootstrap/output.sh && echo "PHASE 3 COMPLETE: output.sh syntax valid"
```

---

## Phase 4: Update Step Scripts

### Pattern for ALL Step Scripts

Each step script should:
1. **NOT** call `record_step()` - orchestrator handles this
2. Return JSON via `json_response()` / `json_error()` only
3. Use `get_plan()` and `get_step_data()` for reading

### Step 4.1: Update plan.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/plan.sh`

**FIND** lines 160-164:
```bash
    # Initialize bootstrap state
    init_state "$plan_data" "$session_id"

    # Record step completion
    record_step "plan" "complete" "$plan_data"
```

**REPLACE WITH**:
```bash
    # Initialize bootstrap with plan
    init_bootstrap "$plan_data"
```

The orchestrator will call `complete_step()` after the step returns.

### Step 4.2: Update recipe-search.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/recipe-search.sh`

**DELETE** all `record_step` calls (lines 211, 228).

Keep only the `json_response` calls - the orchestrator handles state recording.

### Step 4.3: Update generate-import.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/generate-import.sh`

**DELETE** line 72:
```bash
            record_step "generate-import" "complete" "$data"
```

### Step 4.4: Update import-services.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/import-services.sh`

**DELETE** all `record_step` calls (lines 82, 107, 130).

### Step 4.5: Update wait-services.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/wait-services.sh`

**DELETE** line 196:
```bash
            record_step "wait-services" "complete" "$data"
```

### Step 4.6: Update mount-dev.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/mount-dev.sh`

**DELETE** line 109:
```bash
        record_step "mount-dev" "complete" "$mounts_data"
```

### Step 4.7: Update discover-services.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/discover-services.sh`

**DELETE** lines 59 and 121:
```bash
        record_step "discover-services" "complete" "$empty_result"
        ...
    record_step "discover-services" "complete" "$result"
```

### Step 4.8: Update finalize.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/finalize.sh`

**DELETE** line 288:
```bash
    record_step "finalize" "complete" "$data"
```

### Step 4.9: Update spawn-subagents.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/spawn-subagents.sh`

**DELETE** line 641:
```bash
    record_step "spawn-subagents" "complete" "$data"
```

### Step 4.10: Update aggregate-results.sh

**Target**: `zcp/.zcp/lib/bootstrap/steps/aggregate-results.sh`

**DELETE** line 560:
```bash
    record_step "aggregate-results" "complete" "$data"
```

### Step 4.11: Verify Phase 4

```bash
for f in zcp/.zcp/lib/bootstrap/steps/*.sh; do
    bash -n "$f" || echo "SYNTAX ERROR: $f"
done
echo "PHASE 4 COMPLETE: All step scripts verified"
```

---

## Phase 5: Integration Test

### Test Sequence

```bash
# 1. Clean slate
zcp/.zcp/bootstrap.sh reset

# 2. Verify no state
cat /tmp/bootstrap_state.json 2>/dev/null && echo "ERROR: State file should not exist" || echo "OK: Clean state"

# 3. Initialize
zcp/.zcp/bootstrap.sh init --runtime go

# 4. Verify single state file
cat /tmp/bootstrap_state.json | jq .workflow_id && echo "OK: State initialized"

# 5. Check status
zcp/.zcp/bootstrap.sh status

# 6. Resume shows correct next step
zcp/.zcp/bootstrap.sh resume
```

### Success Criteria

1. **Single file**: Only `/tmp/bootstrap_state.json` exists (no `workflow.json`)
2. **No errors**: All commands complete without bash errors
3. **State integrity**: `jq .` parses the state file successfully
4. **Correct flow**: `resume` shows `recipe-search` as next step after init

---

## Rollback Plan

If issues occur, restore from git:
```bash
git checkout HEAD -- zcp/.zcp/lib/bootstrap/state.sh
git checkout HEAD -- zcp/.zcp/bootstrap.sh
git checkout HEAD -- zcp/.zcp/lib/bootstrap/output.sh
git checkout HEAD -- zcp/.zcp/lib/bootstrap/steps/
```

---

## API Quick Reference

### New Unified API

| Function | Purpose | Returns |
|----------|---------|---------|
| `init_bootstrap "$plan"` | Create initial state | - |
| `clear_bootstrap` | Delete all state | - |
| `get_state` | Full state JSON | JSON |
| `bootstrap_active` | Check if active | exit code |
| `get_plan` | Extract plan | JSON |
| `get_step "$name"` | Get step object | JSON |
| `get_step_data "$name"` | Get step data | JSON |
| `is_step_complete "$name"` | Check completion | exit code |
| `get_next_step` | First pending step | string |
| `set_step_status "$name" "$status"` | Update status | - |
| `set_step_data "$name" "$data"` | Update data | - |
| `complete_step "$name" "$data"` | Mark complete + data | - |
| `set_service_status "$host" "$status" "$data"` | Service state | - |
| `get_service_status "$host"` | Read service state | JSON |

### Removed Functions (Do Not Use)

- `init_state` → use `init_bootstrap`
- `record_step` → use `complete_step` (orchestrator only)
- `workflow_exists` → use `bootstrap_active`
- `update_step_status` → use `set_step_status`
- `init_workflow_state` → removed
- `get_checkpoint` / `set_checkpoint` → derived from steps
