# ZCP Gate 0 Analysis: Recipe Review Enforcement Issue

## Executive Summary

Gate 0 (recipe_review.json) is currently enforced for ALL workflow initiations, but it should only be required for **bootstrap flows** (new project setup). For **existing projects** where services already exist, requiring recipe review on every iteration adds unnecessary friction and contradicts the system's primary use case.

## The Problem

### Current Behavior (Incorrect)

```
User: "Add a login button to my existing app"
Agent: init → transition_to DISCOVER
       ❌ BLOCKED: Gate 0 requires recipe_review.json
       Agent must run: .zcp/recipe-search.sh quick go postgresql
```

This makes no sense for an existing project where zerops.yml is already configured and working.

### Expected Behavior (Correct)

```
User: "Add a login button to my existing app"
Agent: init → (discovery exists, session updated)
       → transition_to DEVELOP (Gate 1 passes, discovery.json exists)
       → Do actual work
       → transition_to DEPLOY (Gate 2 checks dev_verify.json)
       → ... rest of workflow
```

Gate 0 should only apply during bootstrap when services are being created and configured for the first time.

---

## Primary Use Case (Being Blocked)

ZCP's main purpose is **AI agent guardrails for ongoing development on Zerops**:

1. User has existing Zerops project with services running
2. User asks Claude for features, debugging, refactoring
3. Claude does the actual development work
4. When deploying → ZCP gates ensure verification before stage

**Bootstrap is the minority case** (one-time project setup). The daily workflow is:
- "Add this feature"
- "Fix this bug"
- "Refactor this module"

These should NOT require recipe review every time.

---

## Code Analysis

### File: `.zcp/lib/commands/init.sh`

Lines 146-171 handle preserved discovery:

```bash
# Check for preserved discovery and update session_id
if [ -f "$DISCOVERY_FILE" ]; then
    ...
    # Update session_id in existing discovery
    jq --arg sid "$session_id" '.session_id = $sid' "$DISCOVERY_FILE" > tmp
    mv tmp "$DISCOVERY_FILE"

    echo "💡 NEXT: Skip DISCOVER, go directly to DEVELOP"
    echo "   .zcp/workflow.sh transition_to DISCOVER"
    echo "   .zcp/workflow.sh transition_to DEVELOP"
    return 0
fi
```

**Issue**: Says "skip DISCOVER" but still requires calling `transition_to DISCOVER`, which enforces Gate 0.

### File: `.zcp/lib/commands/transition.sh`

Lines 199-209 enforce Gate 0 unconditionally:

```bash
DISCOVER)
    if [ "$current_phase" != "INIT" ]; then
        echo "❌ Cannot transition to DISCOVER from $current_phase"
        return 2
    fi
    # Gate 0: Recipe Discovery
    if ! check_gate_init_to_discover; then
        return 2
    fi
```

**Issue**: No check for "if discovery already exists, skip Gate 0".

### File: `.zcp/lib/gates.sh`

Lines 219-320 define `check_gate_init_to_discover()`:

```bash
check_gate_init_to_discover() {
    ...
    # In hotfix mode, warn but don't block
    if [ "$mode" = "hotfix" ]; then
        if [ ! -f "$RECIPE_REVIEW_FILE" ]; then
            echo "  ⚠️  HOTFIX MODE: Recipe review skipped"
            return 0
        fi
    fi

    # In quick mode, skip gate
    if [ "$mode" = "quick" ]; then
        return 0
    fi

    # Check 1: recipe_review.json exists
    if [ -f "$RECIPE_REVIEW_FILE" ]; then
        ...
    else
        echo "  ✗ recipe_review.json missing"
        all_passed=false
    fi
```

**Issue**: Only hotfix and quick modes bypass Gate 0. Full mode always requires it, even for existing projects.

---

## Documentation Mismatch

### FLOW-DIAGRAM.md shows:

```
| Gate | Transition | Evidence Required |
|------|------------|-------------------|
| 0 | INIT → DISCOVER | `recipe_review.json` |
```

### README.md Gate Overview shows:

```
│ 0      │ INIT → DISCOVER         │ recipe_review.json      │
```

Both documents show Gate 0 as part of the main workflow, but this contradicts the system's purpose.

---

## Proposed Fix Options

### Option A: Skip Gate 0 When Discovery Exists

In `check_gate_init_to_discover()`:

```bash
check_gate_init_to_discover() {
    ...
    # EXISTING PROJECT: Skip Gate 0 if valid discovery exists
    if [ -f "$DISCOVERY_FILE" ]; then
        if check_evidence_session "$DISCOVERY_FILE" 2>/dev/null || \
           check_evidence_freshness "$DISCOVERY_FILE" 24 2>/dev/null; then
            echo "  ✓ Existing project with valid discovery - Gate 0 skipped"
            return 0
        fi
    fi

    # New project or stale discovery: require recipe review
    ...
```

### Option B: Direct INIT → DEVELOP Path When Discovery Preserved

In `cmd_init()`, when discovery exists:

```bash
if [ -f "$DISCOVERY_FILE" ]; then
    ...
    # Set phase directly to DEVELOP, skipping DISCOVER entirely
    echo "DEVELOP" > "$PHASE_FILE"
    echo "💡 Discovery preserved. Starting in DEVELOP phase."
    return 0
fi
```

### Option C: New "iterate" Mode That Skips Gate 0

Already exists partially via `iterate` command, but it requires being in DONE phase first.

---

## Recommended Solution

**Option A** is cleanest because:
1. Keeps the phase progression intact (INIT → DISCOVER → DEVELOP)
2. Gate 0 logic decides internally whether to enforce
3. No special paths or modes needed
4. Discovery existence is the natural indicator of "existing project"

The gate check should be:
```
IF discovery.json exists AND (session matches OR <24h old)
THEN skip Gate 0 (existing project iteration)
ELSE require Gate 0 (new project needs recipe patterns)
```

---

## Files to Modify

1. **`.zcp/lib/gates.sh`** - Add discovery check to `check_gate_init_to_discover()`
2. **`README.md`** - Update Gate 0 documentation to clarify it's only for new projects
3. **`FLOW-DIAGRAM.md`** - Add note that Gate 0 is skipped for existing projects

---

## Test Cases

### Test 1: New Project (Gate 0 Required)
```bash
rm -f /tmp/discovery.json /tmp/recipe_review.json
.zcp/workflow.sh init
.zcp/workflow.sh transition_to DISCOVER
# Expected: ❌ Gate 0 blocks, requires recipe-search.sh
```

### Test 2: Existing Project with Fresh Discovery (Gate 0 Skipped)
```bash
# Assume discovery.json exists with current session
.zcp/workflow.sh init
.zcp/workflow.sh transition_to DISCOVER
# Expected: ✓ Gate 0 skipped, proceeds to DISCOVER
```

### Test 3: Existing Project with Stale Discovery (Gate 0 Required?)
```bash
# Assume discovery.json exists but >24h old
.zcp/workflow.sh init
.zcp/workflow.sh transition_to DISCOVER
# Decision needed: Should stale discovery require recipe review?
# Probably not - services still exist, just re-discover them
```

---

## Questions for Implementation

1. Should Gate 0 be skipped for ANY existing discovery, or only fresh (<24h)?
2. Should `iterate` command be the canonical way to continue work (already skips to DEVELOP)?
3. Should there be a `--existing` flag for init that explicitly skips Gate 0?

---

## References

- `zcp/.zcp/lib/gates.sh` - Gate implementations
- `zcp/.zcp/lib/commands/transition.sh` - Phase transition logic
- `zcp/.zcp/lib/commands/init.sh` - Session initialization
- `zcp/.zcp/lib/commands/status.sh` - Show command with phase guidance
- `README.md` - Main documentation
- `FLOW-DIAGRAM.md` - Visual workflow reference
