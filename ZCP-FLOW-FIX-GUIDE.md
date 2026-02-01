# ZCP Flow Fix Implementation Guide

> **For:** Opus Implementator
> **Priority:** CRITICAL
> **Created:** 2026-02-01
> **Scope:** Fix state management split and Gate 0 bypass for existing projects

---

## Executive Summary

The recent refactoring (commit `1df5255`) introduced a unified JSON state system but failed to update `init.sh` to use it. This breaks the main workflow. Additionally, Gate 0 blocks existing projects unnecessarily.

**Three critical fixes required:**
1. Update `init.sh` to use unified state API
2. Add discovery check to Gate 0 bypass
3. Fix misleading guidance in init output

---

## Pre-Implementation Verification

**IMPORTANT:** Before implementing any fixes, verify the bugs exist by running these checks.

### Verify Bug 1: State Management Split

```bash
cd /Users/fxck/www/zcp/zcp

# 1. Clear any existing state
rm -f /tmp/zcp_session /tmp/zcp_mode /tmp/zcp_phase /tmp/zcp_state.json

# 2. Run init
./.zcp/workflow.sh init

# 3. Check what was written
echo "=== Old files ==="
cat /tmp/zcp_session 2>/dev/null && echo " (session exists)" || echo "session: MISSING"
cat /tmp/zcp_mode 2>/dev/null && echo " (mode exists)" || echo "mode: MISSING"
cat /tmp/zcp_phase 2>/dev/null && echo " (phase exists)" || echo "phase: MISSING"

echo "=== New JSON ==="
cat /tmp/zcp_state.json 2>/dev/null || echo "zcp_state.json: MISSING"

# 4. Check what get_session() returns
./.zcp/workflow.sh show
```

**Expected bug behavior:**
- Old files (`/tmp/zcp_session`, etc.) contain data
- New JSON (`/tmp/zcp_state.json`) is MISSING or empty
- `workflow.sh show` reports "Session: none" or empty

### Verify Bug 2: Gate 0 Not Bypassed for Existing Discovery

```bash
cd /Users/fxck/www/zcp/zcp

# 1. Create a fake discovery.json (simulating existing project)
cat > /tmp/discovery.json << 'EOF'
{
  "session_id": "test-session",
  "timestamp": "2026-02-01T12:00:00Z",
  "dev": {"id": "dev123", "name": "appdev"},
  "stage": {"id": "stage123", "name": "appstage"}
}
EOF

# 2. Clear and re-init
rm -f /tmp/zcp_session /tmp/zcp_mode /tmp/zcp_phase /tmp/recipe_review.json

# 3. Run init (should detect preserved discovery)
./.zcp/workflow.sh init

# 4. Try to transition to DISCOVER
./.zcp/workflow.sh transition_to DISCOVER
```

**Expected bug behavior:**
- Gate 0 BLOCKS with "recipe_review.json missing"
- Should have bypassed Gate 0 because discovery.json exists

### Verify Bug 3: Misleading Guidance

```bash
# Check the guidance output when discovery exists
grep -A5 "Skip DISCOVER" /Users/fxck/www/zcp/zcp/.zcp/lib/commands/init.sh
```

**Expected:** Shows contradictory message "Skip DISCOVER" followed by "transition_to DISCOVER"

---

## Implementation Details

### File Locations

All files are under `/Users/fxck/www/zcp/zcp/.zcp/`:

| File | Purpose |
|------|---------|
| `lib/commands/init.sh` | Session initialization - needs unified state |
| `lib/gates.sh` | Gate checks - needs discovery bypass |
| `lib/state.sh` | Unified state API (reference only) |

---

## Fix 1: Update init.sh to Use Unified State API

### File: `lib/commands/init.sh`

#### Problem
Lines 54-56, 98-100, 142-144, 228-230 write directly to old file-based storage instead of using the unified state API.

#### Solution
Replace direct file writes with API calls. The unified state API is defined in `lib/state.sh`.

#### Changes Required

**Change 1a: dev-only mode initialization (lines 54-56)**

Find:
```bash
        echo "$session_id" > "$SESSION_FILE"
        echo "dev-only" > "$MODE_FILE"
        echo "INIT" > "$PHASE_FILE"
```

Replace with:
```bash
        # Initialize unified state
        zcp_init "$session_id"
        set_mode "dev-only"
        set_phase "INIT"
```

**Change 1b: hotfix mode initialization (lines 98-100)**

Find:
```bash
                        echo "$session_id" > "$SESSION_FILE"
                        echo "hotfix" > "$MODE_FILE"
                        echo "DEVELOP" > "$PHASE_FILE"
```

Replace with:
```bash
                        # Initialize unified state
                        zcp_init "$session_id"
                        set_mode "hotfix"
                        set_phase "DEVELOP"
```

**Change 1c: full mode initialization (lines 142-144)**

Find:
```bash
    echo "$session_id" > "$SESSION_FILE"
    echo "full" > "$MODE_FILE"
    echo "INIT" > "$PHASE_FILE"
```

Replace with:
```bash
    # Initialize unified state
    zcp_init "$session_id"
    set_mode "full"
    set_phase "INIT"
```

**Change 1d: quick mode initialization (lines 228-230)**

Find:
```bash
    echo "$session_id" > "$SESSION_FILE"
    echo "quick" > "$MODE_FILE"
    echo "QUICK" > "$PHASE_FILE"
```

Replace with:
```bash
    # Initialize unified state
    zcp_init "$session_id"
    set_mode "quick"
    set_phase "QUICK"
```

#### Verification After Change
```bash
rm -f /tmp/zcp_*
./.zcp/workflow.sh init
cat /tmp/zcp_state.json | jq .
# Should show: session_id, mode: "full", workflow.phase: "INIT"
```

---

## Fix 2: Add Discovery Check to Gate 0

### File: `lib/gates.sh`

#### Problem
`check_gate_init_to_discover()` (lines 219-320) only bypasses Gate 0 for `hotfix` and `quick` modes. It should also bypass when valid `discovery.json` exists (existing project iteration).

#### Solution
Add a check at the start of the function to skip Gate 0 if discovery.json exists with a valid or recent session.

#### Change Required

Find the function `check_gate_init_to_discover()` starting at line 219.

After line 227 (after the header output):
```bash
    echo "Gate: INIT → DISCOVER"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
```

Insert this new block:
```bash
    # EXISTING PROJECT CHECK: Skip Gate 0 if valid discovery exists
    # This enables iteration on existing projects without requiring recipe review
    if [ -f "$DISCOVERY_FILE" ]; then
        # Check if discovery has matching session OR is recent (< 24h)
        if check_evidence_session "$DISCOVERY_FILE" 2>/dev/null; then
            echo "  ✓ Existing project: discovery.json matches current session"
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
            echo "Gate 0 SKIPPED - existing project iteration"
            return 0
        fi

        # Also allow if discovery is fresh (even if session doesn't match)
        # This handles the case where discovery was preserved across sessions
        local disco_timestamp
        disco_timestamp=$(jq -r '.timestamp // empty' "$DISCOVERY_FILE" 2>/dev/null)
        if [ -n "$disco_timestamp" ]; then
            local disco_epoch now_epoch age_hours
            # Try GNU date first, then BSD date
            if disco_epoch=$(date -d "$disco_timestamp" +%s 2>/dev/null) || \
               disco_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$disco_timestamp" +%s 2>/dev/null); then
                now_epoch=$(date +%s)
                age_hours=$(( (now_epoch - disco_epoch) / 3600 ))

                if [ "$age_hours" -lt 24 ]; then
                    echo "  ✓ Existing project: discovery.json is ${age_hours}h old (< 24h)"
                    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo "Gate 0 SKIPPED - existing project iteration"

                    # Update session_id in discovery to current session
                    local current_session
                    current_session=$(get_session)
                    if [ -n "$current_session" ]; then
                        jq --arg sid "$current_session" '.session_id = $sid' "$DISCOVERY_FILE" > "${DISCOVERY_FILE}.tmp" && \
                            mv "${DISCOVERY_FILE}.tmp" "$DISCOVERY_FILE"
                    fi
                    return 0
                fi
            fi
        fi

        # Discovery exists but is stale - show warning but still require Gate 0
        echo "  ⚠ discovery.json exists but is stale (> 24h)"
        echo "    Consider: Is this the same project? If so, update discovery."
    fi
```

#### Verification After Change
```bash
# Create fresh discovery
cat > /tmp/discovery.json << 'EOF'
{
  "session_id": "old-session",
  "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
  "dev": {"id": "dev123", "name": "appdev"},
  "stage": {"id": "stage123", "name": "appstage"}
}
EOF

rm -f /tmp/recipe_review.json
./.zcp/workflow.sh init
./.zcp/workflow.sh transition_to DISCOVER
# Should now PASS without recipe_review.json
```

---

## Fix 3: Fix Misleading Guidance in init.sh

### File: `lib/commands/init.sh`

#### Problem
Lines 166-169 output contradictory guidance when discovery is preserved.

#### Solution
Update the message to accurately reflect what will happen.

#### Change Required

Find (around lines 160-170):
```bash
            echo "✅ Session: $session_id"
            echo ""
            echo "📋 Preserved discovery detected:"
            echo "   Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")"
            echo "   Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")"
            echo ""
            echo "💡 NEXT: Skip DISCOVER, go directly to DEVELOP"
            echo "   .zcp/workflow.sh transition_to DISCOVER"
            echo "   .zcp/workflow.sh transition_to DEVELOP"
            return 0
```

Replace with:
```bash
            echo "✅ Session: $session_id"
            echo ""
            echo "📋 Preserved discovery detected:"
            echo "   Dev:   $(jq -r '.dev.name' "$DISCOVERY_FILE")"
            echo "   Stage: $(jq -r '.stage.name' "$DISCOVERY_FILE")"
            echo ""
            echo "💡 EXISTING PROJECT - Gate 0 will be skipped"
            echo ""
            echo "   Run these commands:"
            echo "   .zcp/workflow.sh transition_to DISCOVER   ← Gate 0 skipped (discovery exists)"
            echo "   .zcp/workflow.sh transition_to DEVELOP    ← Continue to development"
            echo ""
            echo "   Or use iterate if you just completed a workflow:"
            echo "   .zcp/workflow.sh iterate \"description\""
            return 0
```

---

## Fix 4: Ensure State Sync for Legacy File Reads

### File: `lib/state.sh`

#### Problem
Some code paths (like `cmd_context` in status.sh) still read from legacy files. Need to ensure unified state is synced to legacy files for backward compatibility during transition.

#### Solution
Add a sync function to `zcp_init()` that writes to legacy files as well.

#### Change Required

In `lib/state.sh`, modify the `zcp_init()` function (around line 52).

Find the end of `zcp_init()` (before the final `echo "$session_id"`):
```bash
    atomic_write "$state" "$ZCP_STATE_FILE"
    [[ -d "$(dirname "$ZCP_STATE_PERSISTENT")" ]] && atomic_write "$state" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true
    echo "$session_id"
```

Replace with:
```bash
    atomic_write "$state" "$ZCP_STATE_FILE"
    [[ -d "$(dirname "$ZCP_STATE_PERSISTENT")" ]] && atomic_write "$state" "$ZCP_STATE_PERSISTENT" 2>/dev/null || true

    # Backward compatibility: also write to legacy files
    # This ensures code that still reads from old files works correctly
    echo "$session_id" > "${ZCP_TMP_DIR:-/tmp}/zcp_session" 2>/dev/null || true
    echo "full" > "${ZCP_TMP_DIR:-/tmp}/zcp_mode" 2>/dev/null || true
    echo "NONE" > "${ZCP_TMP_DIR:-/tmp}/zcp_phase" 2>/dev/null || true

    echo "$session_id"
```

Also modify `set_mode()` (around line 129) to sync:

Find the end of `set_mode()`:
```bash
    zcp_set '.mode' "\"$mode\""
```

Replace with:
```bash
    zcp_set '.mode' "\"$mode\""
    # Backward compat: sync to legacy file
    echo "$mode" > "${ZCP_TMP_DIR:-/tmp}/zcp_mode" 2>/dev/null || true
```

And modify `set_phase()` (around line 144) to sync:

Find the end of `set_phase()`:
```bash
    zcp_set '.workflow.phase' "\"$phase\""
```

Replace with:
```bash
    zcp_set '.workflow.phase' "\"$phase\""
    # Backward compat: sync to legacy file
    echo "$phase" > "${ZCP_TMP_DIR:-/tmp}/zcp_phase" 2>/dev/null || true
```

---

## Post-Implementation Testing

### Test Suite

Run all tests to verify fixes work:

```bash
cd /Users/fxck/www/zcp/zcp

echo "=== TEST 1: Basic init creates unified state ==="
rm -f /tmp/zcp_* /tmp/discovery.json
./.zcp/workflow.sh init
echo "Session from show command:"
./.zcp/workflow.sh state
echo ""
echo "JSON state:"
jq -r '.session_id, .mode, .workflow.phase' /tmp/zcp_state.json
echo ""

echo "=== TEST 2: Existing project skips Gate 0 ==="
rm -f /tmp/zcp_* /tmp/recipe_review.json
cat > /tmp/discovery.json << EOF
{
  "session_id": "old-session",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "dev": {"id": "dev123", "name": "appdev"},
  "stage": {"id": "stage123", "name": "appstage"}
}
EOF
./.zcp/workflow.sh init
./.zcp/workflow.sh transition_to DISCOVER && echo "PASS: Gate 0 skipped" || echo "FAIL: Gate 0 blocked"
echo ""

echo "=== TEST 3: New project still requires Gate 0 ==="
rm -f /tmp/zcp_* /tmp/discovery.json /tmp/recipe_review.json
./.zcp/workflow.sh init
./.zcp/workflow.sh transition_to DISCOVER 2>&1 | grep -q "recipe_review.json missing" && echo "PASS: Gate 0 enforced" || echo "FAIL: Gate 0 not enforced"
echo ""

echo "=== TEST 4: Quick mode still works ==="
rm -f /tmp/zcp_*
./.zcp/workflow.sh --quick
./.zcp/workflow.sh state | grep -q "QUICK" && echo "PASS: Quick mode works" || echo "FAIL: Quick mode broken"
echo ""

echo "=== TEST 5: Hotfix mode still works ==="
rm -f /tmp/zcp_*
cat > /tmp/discovery.json << EOF
{
  "session_id": "old-session",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "dev": {"id": "dev123", "name": "appdev"},
  "stage": {"id": "stage123", "name": "appstage"}
}
EOF
./.zcp/workflow.sh init --hotfix
./.zcp/workflow.sh state | grep -q "DEVELOP" && echo "PASS: Hotfix mode works" || echo "FAIL: Hotfix mode broken"
```

### Expected Results

All tests should output "PASS".

---

## Summary of Changes

| File | Lines Changed | Change Type |
|------|---------------|-------------|
| `lib/commands/init.sh` | 54-56, 98-100, 142-144, 166-170, 228-230 | Replace file writes with API calls |
| `lib/gates.sh` | After line 227 | Add discovery check block |
| `lib/state.sh` | 82-86, 135, 150 | Add legacy file sync |

---

## Rollback Plan

If issues arise, revert changes with:
```bash
git checkout HEAD~1 -- zcp/.zcp/lib/commands/init.sh zcp/.zcp/lib/gates.sh zcp/.zcp/lib/state.sh
```

---

## Notes for Implementor

1. **Verify before implementing** - Run the pre-implementation verification steps first
2. **Atomic changes** - Each fix can be implemented and tested independently
3. **Order matters** - Implement Fix 1 first (state management) as other fixes depend on it working
4. **Legacy compat** - Fix 4 ensures backward compatibility during transition; can be removed later
5. **Test thoroughly** - The test suite covers critical paths but consider edge cases

---

## Reference: State API Functions

From `lib/state.sh`:

| Function | Purpose |
|----------|---------|
| `zcp_init($session_id)` | Initialize unified state with new session |
| `zcp_state()` | Get full state JSON |
| `zcp_get($path)` | Get value at jq path |
| `zcp_set($path, $value)` | Set value at jq path |
| `get_session()` | Get current session ID |
| `get_mode()` | Get current mode |
| `set_mode($mode)` | Set mode (full/quick/hotfix/dev-only/bootstrap) |
| `get_phase()` | Get current phase |
| `set_phase($phase)` | Set phase (NONE/INIT/DISCOVER/DEVELOP/DEPLOY/VERIFY/DONE/QUICK) |
