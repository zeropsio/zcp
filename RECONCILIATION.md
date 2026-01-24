# ZCP Reconciliation Guide

> **Verified**: 2026-01-24 - All claims triple-verified against codebase. Corrections applied.

> **Goal**: Transform ZCP from an evolved, layered system into a clean, unified, world-class workflow orchestration framework.

---

## Executive Summary

ZCP has accumulated technical debt through evolutionary development across 4-5 design iterations. This guide provides a systematic path to reconcile all inconsistencies, remove dead code, complete missing implementations, and establish a single coherent architecture.

**Estimated Effort**: 2-3 focused sessions
**Risk Level**: Low (mostly additive fixes and removals, core flow is solid)
**Result**: A system that is self-consistent, fully documented, and operates exactly as described

---

## Table of Contents

1. [Phase 1: Remove Dead Code](#phase-1-remove-dead-code)
2. [Phase 2: Complete Missing Implementations](#phase-2-complete-missing-implementations)
3. [Phase 3: Unify State Management](#phase-3-unify-state-management)
4. [Phase 4: Fix Gate System](#phase-4-fix-gate-system)
5. [Phase 5: Reconcile Workflows](#phase-5-reconcile-workflows)
6. [Phase 6: Security & Integration](#phase-6-security--integration)
7. [Phase 7: Documentation Alignment](#phase-7-documentation-alignment)
8. [Architecture Decisions](#architecture-decisions)
9. [Success Criteria](#success-criteria)

---

## Phase 1: Remove Dead Code

### 1.1 Delete Orphaned Functions

**File**: `.zcp/lib/env.sh`

Remove these functions that are defined but never called:

```bash
# DELETE: with_env() - lines 41-65
# DELETE: env_exists() - lines 24-36
```

**Action**: Either delete the entire `env.sh` file if nothing else is used, or remove just these functions.

---

### 1.2 Delete or Integrate Orphaned Planning Commands

**File**: `.zcp/lib/commands/planning.sh`

The following commands create evidence files that no gate checks:

| Command | Evidence File | Gate That Should Check It |
|---------|---------------|---------------------------|
| `cmd_plan_services` | `/tmp/service_plan.json` | None exists |
| `cmd_snapshot_dev` | `/tmp/dev_snapshot.json` | "Gate 5" (doesn't exist) |

**Decision Required**:

- **Option A (Recommended)**: Delete `planning.sh` entirely. The synthesis flow (`compose`) supersedes `plan_services`, and dev verification (`verify.sh`) supersedes `snapshot_dev`.

- **Option B**: Integrate into gate system. Create Gate 3 (config validation) and Gate 5 (dev snapshot) that actually use these.

**If choosing Option A**, also:
1. Remove from `workflow.sh` router (lines 92-96)
2. Remove from `commands.sh` source line
3. Remove help text references
4. Remove variable definitions in `utils.sh`: `SERVICE_PLAN_FILE`, `DEV_SNAPSHOT_FILE`

---

### 1.3 Clean Up Unused Evidence File Definitions

**File**: `.zcp/lib/utils.sh`

If deleting planning.sh, remove:

```bash
# Line 28 - DELETE
SERVICE_PLAN_FILE="${ZCP_TMP_DIR}/service_plan.json"

# Line 31 - DELETE
DEV_SNAPSHOT_FILE="${ZCP_TMP_DIR}/dev_snapshot.json"
```

**Note**: `CONFIG_VALIDATED_FILE` (line 30) IS used by `validate-config.sh:12` - keep it if keeping that validation tool.

---

### 1.4 Security Hook - Verify It Works

**File**: `.zcp/lib/security-hook.sh`

**Current State**: ✅ Security hook IS properly configured in `.claude/settings.json`:
```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Bash",
      "hooks": [{
        "type": "command",
        "command": "/var/www/.zcp/lib/security-hook.sh"
      }]
    }]
  }
}
```

**Path**: Hardcoded to `/var/www/.zcp/lib/security-hook.sh` - this is correct since ZCP always deploys to `/var/www/`.

**Action Required**: Test that the hook actually blocks dangerous patterns (see Phase 6.2). No configuration changes needed.

---

## Phase 2: Complete Missing Implementations

### 2.1 ~~Implement `cmd_intent` and `cmd_note`~~ ALREADY EXISTS

**Status**: ✅ **NO ACTION NEEDED**

These functions already exist in `.zcp/lib/commands/context.sh`:
- `cmd_intent()` - lines 9-37
- `cmd_note()` - lines 43-56
- `get_intent()` - lines 62-78
- `set_intent()` - lines 80-100
- `add_note()` - lines 102-138
- `show_notes()` - lines 140-172

The router correctly dispatches to these via `commands.sh` which sources `context.sh`.

---

### 2.2 Implement or Remove Gate 3 (Config Validation)

**Decision**: Should config validation be a gate?

**If YES** (recommended), add to `gates.sh`:

```bash
# ============================================================================
# Gate 3: CONFIG_VALIDATION (DEVELOP → DEPLOY)
# ============================================================================
# Validates zerops.yml before allowing deployment

check_gate_config_validation() {
    local config_file="${1:-/var/www/$(get_dev_service)/zerops.yml}"

    gate_start "Gate 3: Config Validation"

    # Check 1: zerops.yml exists
    gate_check_file "$config_file" "zerops.yml" \
        "Create zerops.yml in service directory"

    # Check 2: Has zerops: wrapper
    if [ -f "$config_file" ]; then
        if yq e '.zerops' "$config_file" > /dev/null 2>&1; then
            gate_pass "zerops.yml has 'zerops:' wrapper"
        else
            gate_fail "zerops.yml missing 'zerops:' wrapper" \
                "Add 'zerops:' as top-level key"
        fi

        # Check 3: Has at least one setup
        local setup_count=$(yq e '.zerops | length' "$config_file" 2>/dev/null || echo "0")
        if [ "$setup_count" -gt 0 ]; then
            gate_pass "Has $setup_count setup configuration(s)"
        else
            gate_fail "No setup configurations found" \
                "Add setup block under zerops:"
        fi

        # Check 4: deployFiles includes built artifacts
        # This is a warning, not a blocker
        if yq e '.zerops[].build.deployFiles' "$config_file" 2>/dev/null | grep -q "\."; then
            gate_pass "deployFiles configured"
        else
            gate_warn "deployFiles may need review"
        fi
    fi

    gate_finish "$CONFIG_VALIDATED_FILE" 24
}
```

**Then integrate** into `check_gate_develop_to_deploy()`:

```bash
check_gate_develop_to_deploy() {
    gate_start "Gate: DEVELOP → DEPLOY"

    # Check 1: dev_verify.json exists
    gate_check_file "$DEV_VERIFY_FILE" "dev_verify.json" \
        "Run: .zcp/verify.sh {dev} {port} / /status /api/..."

    # Check 2: session_id matches
    gate_check_session "$DEV_VERIFY_FILE"

    # Check 3: failures == 0
    gate_check_no_failures "$DEV_VERIFY_FILE" "verification"

    # Check 4: Config validation (Gate 3)
    # This is integrated rather than separate
    local dev_service=$(jq -r '.dev.name' "$DISCOVERY_FILE" 2>/dev/null)
    if [ -n "$dev_service" ] && [ -f "/var/www/$dev_service/zerops.yml" ]; then
        if yq e '.zerops' "/var/www/$dev_service/zerops.yml" > /dev/null 2>&1; then
            gate_pass "zerops.yml structure valid"
        else
            gate_fail "zerops.yml invalid" "Run: .zcp/validate-config.sh"
        fi
    fi

    gate_finish "$DEV_VERIFY_FILE" 24
}
```

**If NO** (simpler approach):
1. Remove "Gate 3" references from help text
2. Remove `CONFIG_VALIDATED_FILE` from utils.sh
3. Keep `validate-config.sh` as optional validation tool, not a gate

---

### 2.3 Fix Iteration Completion Tracking

**File**: `.zcp/lib/commands/iterate.sh`

The `completed` field is always null. Add completion tracking:

```bash
# In cmd_complete or when transitioning to DONE:
mark_iteration_complete() {
    local iteration=$(get_iteration)
    local history_file="$WORKFLOW_STATE_DIR/iteration_history.json"
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    if [ -f "$history_file" ]; then
        local updated=$(jq --arg n "$iteration" --arg ts "$timestamp" \
            '(.iterations[] | select(.n == ($n | tonumber)) | .completed) = $ts' \
            "$history_file")
        echo "$updated" > "$history_file"
    fi
}
```

Call this from `cmd_complete`.

---

## Phase 3: Unify State Management

### 3.1 Decide on Single Source of Truth

**Current State**: Two systems coexist
- `utils.sh`: Individual files (`claude_session`, `claude_phase`, etc.)
- `state.sh` (WIGGUM): Single JSON file with all state

**Recommendation**: Keep both, but with clear hierarchy:

```
Individual files (utils.sh) = PRIMARY source of truth
  ↓ (derived from)
WIGGUM JSON (state.sh) = COMPUTED view for agent consumption
```

**Enforce this by**:
1. All state WRITES go through utils.sh functions (`set_phase`, `set_mode`, etc.)
2. WIGGUM JSON is regenerated on every read, never written directly as source
3. Document this clearly in state.sh header

---

### 3.2 Ensure Consistent State Updates

Add `sync_state()` to be called after any state change:

```bash
# In utils.sh
sync_state() {
    # Sync to persistent storage
    sync_to_persistent

    # Update WIGGUM JSON
    if type update_workflow_state &>/dev/null; then
        update_workflow_state 2>/dev/null
    fi
}
```

Then update all state-changing functions to call it:

```bash
set_phase() {
    echo "$1" > "$PHASE_FILE"
    sync_state
}

set_mode() {
    echo "$1" > "$MODE_FILE"
    sync_state
}
```

---

### 3.3 Remove Duplicate Phase Definitions

**File**: `utils.sh` line 47 has `PHASES` array
**File**: `state.sh` lines 32-41 has `PHASES_FULL_SYNTHESIS`, etc.

**Action**: Keep only `state.sh` definitions (they're more complete), remove from `utils.sh`.

Update `validate_phase()` in utils.sh to use state.sh's arrays:

```bash
validate_phase() {
    local phase="$1"
    local all_phases="INIT COMPOSE EXTEND SYNTHESIZE DISCOVER DEVELOP DEPLOY VERIFY DONE"

    for p in $all_phases; do
        if [ "$p" = "$phase" ]; then
            return 0
        fi
    done
    return 1
}
```

---

## Phase 4: Fix Gate System

### 4.1 Renumber Gates Consistently

Current confusion:
- Gate 0, 0.5, 1, 2 exist
- Gate 3, 4, 5 are referenced but don't exist
- Gate 6, 7, S exist

**New numbering scheme** (simpler):

| Gate | Transition | What It Checks |
|------|------------|----------------|
| **G0** | INIT → DISCOVER/COMPOSE | Recipe review |
| **G0.5** | Before extend | Import.yml validation |
| **G1** | DISCOVER → DEVELOP | Discovery evidence |
| **G2** | EXTEND → SYNTHESIZE | Services imported |
| **GS** | SYNTHESIZE → DEVELOP | Synthesis complete |
| **G3** | DEVELOP → DEPLOY | Dev verification + config |
| **G4** | DEPLOY → VERIFY | Deploy evidence |
| **G5** | VERIFY → DONE | Stage verification |

Update all gate functions, help text, and comments to use this scheme.

---

### 4.2 Make Evidence Freshness Actually Matter (Or Remove It)

**File**: `utils.sh` - `check_evidence_freshness()`

Current: Always returns 0, only emits warning.

**Option A (Enforce It)**:
```bash
check_evidence_freshness() {
    local file="$1"
    local max_age_hours="${2:-24}"
    local mode=$(get_mode)

    # ... age calculation ...

    if [ "$age_hours" -gt "$max_age_hours" ]; then
        if [ "$mode" = "hotfix" ]; then
            echo "⚠️  STALE EVIDENCE WARNING (hotfix mode - proceeding)"
            return 0  # Hotfix allows stale
        else
            echo "❌ STALE EVIDENCE: $file is ${age_hours}h old (max: ${max_age_hours}h)"
            echo "   Re-run the command to generate fresh evidence"
            return 1  # Block in normal mode
        fi
    fi
    return 0
}
```

**Option B (Remove It)**:
Delete `check_evidence_freshness()` entirely and remove all calls to it.

**Recommendation**: Option A - it's a good safeguard, just needs teeth.

---

### 4.3 Add Gate Bypass for Quick Mode Consistently

Ensure all gate functions check for quick mode:

```bash
# Add at start of every gate function:
check_gate_*() {
    local mode=$(get_mode)
    if [ "$mode" = "quick" ]; then
        echo "⚠️  QUICK MODE: Gate bypassed"
        return 0
    fi

    # ... rest of gate logic
}
```

---

## Phase 5: Reconcile Workflows

### 5.1 Fix Bootstrap Guidance

**File**: `.zcp/lib/commands/transition.sh` - `output_discover_guidance()`

When no services exist, it currently describes an old flow. Update to:

```bash
output_discover_guidance() {
    # ... existing service detection code ...

    if [ "$has_services" = true ]; then
        # STANDARD FLOW - keep existing
    else
        # BOOTSTRAP FLOW - point to synthesis
        cat <<'EOF'
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🚀 BOOTSTRAP FLOW: No runtime services found
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

You need to CREATE services. Use the synthesis flow:

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 1: Review recipes (Gate 0)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   .zcp/recipe-search.sh quick {runtime} [managed-service]

   Example: .zcp/recipe-search.sh quick go postgresql

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 2: Transition to COMPOSE
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   .zcp/workflow.sh transition_to COMPOSE

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 STEP 3: Generate infrastructure
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   .zcp/workflow.sh compose --runtime go --services postgresql

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📋 Full synthesis flow:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

   INIT → COMPOSE → EXTEND → SYNTHESIZE → DEVELOP → DEPLOY → VERIFY → DONE

EOF
    fi
}
```

---

### 5.2 Enforce Mutual Exclusion: DISCOVER vs COMPOSE

**File**: `.zcp/lib/commands/transition.sh`

Add check to prevent mixing flows:

```bash
# In transition to DISCOVER:
DISCOVER)
    if [ -f "${ZCP_TMP_DIR:-/tmp}/synthesis_plan.json" ]; then
        echo "❌ Cannot use DISCOVER after running compose"
        echo "   You're in synthesis mode. Use: transition_to COMPOSE"
        echo "   To reset: .zcp/workflow.sh reset"
        return 2
    fi
    # ... existing logic
    ;;

# In transition to COMPOSE:
COMPOSE)
    if [ -f "$DISCOVERY_FILE" ]; then
        echo "❌ Cannot use COMPOSE after creating discovery"
        echo "   You're in standard mode. Use: transition_to DISCOVER"
        echo "   To reset: .zcp/workflow.sh reset"
        return 2
    fi
    # ... existing logic
    ;;
```

---

### 5.3 Add Backward Transitions for Synthesis Flow

**File**: `.zcp/lib/commands/transition.sh`

Currently only allows:
- VERIFY → DEVELOP
- DEPLOY → DEVELOP
- DONE → VERIFY

Add synthesis-specific backward transitions:

```bash
# In backward transition handler:
SYNTHESIZE→EXTEND)
    rm -f "$SYNTHESIS_COMPLETE_FILE"
    echo "⚠️  Backward transition: Synthesis evidence invalidated"
    set_phase "$target_phase"
    output_phase_guidance "$target_phase"
    return 0
    ;;
EXTEND→COMPOSE)
    rm -f "$SERVICES_IMPORTED_FILE"
    rm -f "$SYNTHESIZED_IMPORT_FILE"
    echo "⚠️  Backward transition: Import evidence invalidated"
    set_phase "$target_phase"
    output_phase_guidance "$target_phase"
    return 0
    ;;
```

---

### 5.4 Clarify Dev-Only → Full Upgrade Path

**File**: `.zcp/lib/commands/extend.sh` - `cmd_upgrade_to_full()`

Document and verify the upgrade path:

```bash
cmd_upgrade_to_full() {
    local mode=$(get_mode)

    if [ "$mode" != "dev-only" ]; then
        echo "❌ Already in $mode mode"
        return 1
    fi

    # Check dev work is complete
    if [ "$(get_phase)" != "DONE" ]; then
        echo "❌ Complete dev-only workflow first (reach DONE phase)"
        return 1
    fi

    # Check dev verification exists
    if [ ! -f "$DEV_VERIFY_FILE" ]; then
        echo "❌ Dev verification required before upgrading"
        echo "   Run: .zcp/verify.sh {dev} {port} /"
        return 1
    fi

    echo "Upgrading to full deployment mode..."

    # Change mode
    set_mode "full"

    # Reset to DEVELOP (not DONE)
    set_phase "DEVELOP"

    echo "✅ Upgraded to full mode"
    echo ""
    echo "Next steps:"
    echo "  1. .zcp/workflow.sh transition_to DEPLOY"
    echo "  2. Deploy to stage"
    echo "  3. .zcp/workflow.sh transition_to VERIFY"
    echo "  4. Verify stage"
    echo "  5. .zcp/workflow.sh transition_to DONE"

    output_phase_guidance "DEVELOP"
}
```

---

## Phase 6: Security & Integration

### 6.1 Security Hook Configuration - ✅ Already Done

**Current State**: The security hook IS properly configured in `.claude/settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Bash",
      "hooks": [{
        "type": "command",
        "command": "/var/www/.zcp/lib/security-hook.sh",
        "statusMessage": "Security check"
      }]
    }]
  }
}
```

**No configuration changes needed.** The hook is correctly set up to intercept Bash commands before execution.

---

### 6.2 Test Security Hook

Create test script `.zcp/test/test-security-hook.sh`:

```bash
#!/bin/bash
# Test security hook blocks dangerous patterns

HOOK="../lib/security-hook.sh"

# Test cases that SHOULD be blocked
SHOULD_BLOCK=(
    'ssh dev "env"'
    'ssh dev "printenv"'
    'ssh dev "env | grep SECRET"'
    'psql "postgresql://user:hardcoded@host/db"'
)

# Test cases that SHOULD be allowed
SHOULD_ALLOW=(
    'ssh dev "ls -la"'
    'ssh dev "echo \$DB_HOST"'
    'psql "\$db_connectionString"'
)

echo "Testing security hook..."

for cmd in "${SHOULD_BLOCK[@]}"; do
    result=$($HOOK "$cmd" 2>&1)
    if echo "$result" | grep -q '"decision": "block"'; then
        echo "✓ Correctly blocked: $cmd"
    else
        echo "✗ FAILED TO BLOCK: $cmd"
    fi
done

for cmd in "${SHOULD_ALLOW[@]}"; do
    result=$($HOOK "$cmd" 2>&1)
    if echo "$result" | grep -q '"decision": "block"'; then
        echo "✗ INCORRECTLY BLOCKED: $cmd"
    else
        echo "✓ Correctly allowed: $cmd"
    fi
done
```

---

## Phase 7: Documentation Alignment

### 7.1 Update Help Text to Match Reality

**Files to audit**:
- `.zcp/lib/help/full.sh`
- `.zcp/lib/help/topics.sh`
- `workflow.sh` help output
- `CLAUDE.md`

**Checklist**:
- [ ] Remove references to Gate 3, 4, 5 (old numbering)
- [ ] Add correct gate numbering
- [ ] Remove `plan_services` and `snapshot_dev` if deleted
- [ ] Verify `intent` and `note` commands are documented (they exist in context.sh)
- [ ] Document synthesis vs standard flow decision tree
- [ ] Document dev-only → full upgrade path

---

### 7.2 Create Architecture Decision Records

Add `.zcp/docs/adr/` directory with:

**ADR-001-state-management.md**:
```markdown
# ADR 001: State Management Architecture

## Status
Accepted

## Context
ZCP needs to track workflow state across sessions and tool calls.

## Decision
- Individual files (claude_session, claude_phase, etc.) are the PRIMARY source of truth
- WIGGUM JSON state is a COMPUTED view derived from these files
- All writes go through utils.sh functions
- WIGGUM JSON is regenerated on every read

## Consequences
- Clear hierarchy prevents state conflicts
- Easy to debug by inspecting individual files
- WIGGUM provides rich state for agent consumption
```

**ADR-002-gate-system.md**:
```markdown
# ADR 002: Gate System Design

## Status
Accepted

## Context
Gates prevent agents from skipping critical verification steps.

## Decision
- Gates are numbered G0 through G5
- Each gate corresponds to a specific transition
- Gates check for evidence files with matching session IDs
- Quick mode bypasses all gates
- Hotfix mode bypasses only G0 (recipe) and G3 (dev verify)

## Gate Definitions
| Gate | Transition | Evidence Required |
|------|------------|-------------------|
| G0 | INIT → DISCOVER/COMPOSE | recipe_review.json |
| G0.5 | Before extend | Valid import.yml |
| G1 | DISCOVER → DEVELOP | discovery.json |
| G2 | EXTEND → SYNTHESIZE | services_imported.json |
| GS | SYNTHESIZE → DEVELOP | synthesis_complete.json |
| G3 | DEVELOP → DEPLOY | dev_verify.json |
| G4 | DEPLOY → VERIFY | deploy_evidence.json |
| G5 | VERIFY → DONE | stage_verify.json |
```

---

### 7.3 Update CLAUDE.md

Add clear decision tree at the top:

```markdown
# ZCP Quick Start

## Which Flow?

```
Do services already exist in this project?
├── YES → Standard Flow
│         INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE
│
│         Start: .zcp/workflow.sh init
│                .zcp/recipe-search.sh quick {runtime}
│                .zcp/workflow.sh transition_to DISCOVER
│
└── NO  → Synthesis Flow (Bootstrap)
          INIT → COMPOSE → EXTEND → SYNTHESIZE → DEVELOP → DEPLOY → VERIFY → DONE

          Start: .zcp/workflow.sh init
                 .zcp/recipe-search.sh quick {runtime}
                 .zcp/workflow.sh transition_to COMPOSE
```

## Lost Context?

```bash
.zcp/workflow.sh show      # Current state
.zcp/workflow.sh recover   # Full context recovery
```
```

---

## Architecture Decisions

### Decision 1: Remove Planning Commands

**Rationale**: The synthesis flow (`compose`) supersedes `plan_services`, and dev verification supersedes `snapshot_dev`. These commands create evidence that nothing checks.

**Action**: Delete `planning.sh` and all references.

---

### Decision 2: Integrate Config Validation into DEVELOP → DEPLOY Gate

**Rationale**: Rather than a separate Gate 3, config validation is a natural part of the DEVELOP → DEPLOY transition.

**Action**: Add config checks to `check_gate_develop_to_deploy()`.

---

### Decision 3: Enforce Evidence Freshness

**Rationale**: The 24-hour concept exists in documentation but isn't enforced. Either enforce it or remove it.

**Action**: Make `check_evidence_freshness()` return non-zero for stale evidence (except in hotfix mode).

---

### Decision 4: Verify Security Hook Works

**Rationale**: Security hook is configured in `.claude/settings.json` and provides protection against env dumping and credential exposure. Need to verify it actually blocks dangerous patterns.

**Action**: Create and run test script (Phase 6.2) to verify hook blocks expected patterns and allows safe commands.

---

### Decision 5: Single Gate Numbering Scheme

**Rationale**: Current numbering (0, 0.5, 1, 2, 3?, 4?, 5?, 6, 7, S) is confusing with gaps.

**Action**: Renumber to G0-G5 + GS, update all references.

---

## Success Criteria

### Functional Completeness
- [x] All commands in router have implementations (intent, note already exist in context.sh)
- [ ] All evidence files are checked by gates
- [ ] All gates are called at appropriate transitions
- [ ] All help text matches actual functionality

### Code Quality
- [ ] No dead code (unused functions, orphaned files)
- [ ] No duplicate definitions (phase arrays, file paths)
- [ ] Single source of truth for state
- [ ] Consistent patterns throughout

### Documentation
- [ ] CLAUDE.md accurately describes system
- [ ] Help text matches implementations
- [ ] Gate numbering is consistent everywhere
- [ ] ADRs document key decisions

### Security
- [x] Security hook configuration in settings.json (already done)
- [ ] Security hook tested with test script
- [ ] All blocked patterns are documented
- [ ] Permissions in settings.json are minimal and correct

### Testing
- [ ] Each gate can be triggered and passes/fails correctly
- [ ] Each command runs without errors
- [ ] Forward and backward transitions work
- [ ] Quick mode bypasses gates
- [ ] Hotfix mode bypasses appropriate gates

---

## Implementation Order

1. **Phase 1** (Remove Dead Code) - Cleans the slate
2. **Phase 2** (Complete Implementations) - Fills the gaps
3. **Phase 3** (Unify State) - Single source of truth
4. **Phase 4** (Fix Gates) - Consistent enforcement
5. **Phase 5** (Reconcile Workflows) - Clear paths
6. **Phase 6** (Security) - Verify hook works
7. **Phase 7** (Documentation) - Accurate docs

Each phase can be done independently, but they build on each other. Phase 1 is quickest win (deletion is easy). Phase 7 should be done last (document what exists, not what was planned).

---

## Estimated Effort

| Phase | Effort | Risk | Notes |
|-------|--------|------|-------|
| Phase 1 | 30 min | Very Low | |
| Phase 2 | 30-60 min | Low | 2.1 already done (context.sh exists) |
| Phase 3 | 30 min | Low | |
| Phase 4 | 1 hour | Medium | |
| Phase 5 | 1 hour | Medium | |
| Phase 6 | 15 min | Very Low | Hook configured, just needs testing |
| Phase 7 | 1-2 hours | Very Low | |

**Total**: 3.5-5.5 hours of focused work

---

## Post-Reconciliation

After completing all phases:

1. **Run full workflow test**: INIT → DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE
2. **Run synthesis workflow test**: INIT → COMPOSE → EXTEND → SYNTHESIZE → DEVELOP → DEPLOY → VERIFY → DONE
3. **Test edge cases**: quick mode, hotfix mode, dev-only mode, backward transitions
4. **Update version**: Consider this v2.0 of ZCP
5. **Archive this document**: Move to `.zcp/docs/completed/reconciliation-v2.md`
