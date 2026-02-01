# Implementation Guide: Subagent Context Gap Fixes

**Status**: Ready for implementation
**Scope**: 2 targeted fixes (1 critical, 1 optional)
**Estimated Changes**: ~15 lines across 2 files

---

## Executive Summary

Analysis verified that `spawn-subagents.sh` already contains most required context. Two gaps remain:

| Priority | Fix | File | Impact |
|----------|-----|------|--------|
| **P0** | Add manual-start warning to status.sh | `zcp/.zcp/status.sh` | Prevents false "done" state |
| **P1** | Add `critical_notes` to handoff JSON | `zcp/.zcp/lib/bootstrap/steps/finalize.sh` | Machine-readable context |

---

## Fix 1: status.sh Manual Start Warning (P0 - CRITICAL)

### Problem

When `status.sh --wait` reports "Deployment complete!", agents assume the application is running. But dev services use `zsc noop --silent` - the container is ACTIVE but no process serves HTTP.

### Current Behavior

```
⏳ Waiting for godev deployment to complete (timeout: 300s)...
  [0/300s] 🔨 Building... (service: godev)
  [45/300s] ✅ Deployment complete!      ← Agent thinks "done", moves to verify
                                          ← verify.sh fails with HTTP 000
```

### Target Behavior

```
⏳ Waiting for godev deployment to complete (timeout: 300s)...
  [0/300s] 🔨 Building... (service: godev)
  [45/300s] ✅ Deployment complete!
  ⚠️  Dev service deployed. Start your application before verifying:
      ssh godev "cd /var/www && nohup <start-cmd> > /tmp/app.log 2>&1 &"
```

### Implementation

**File**: `zcp/.zcp/status.sh`

**Location**: Inside `wait_for_deployment()` function, SUCCESS case (around line 558)

**Find this block** (lines 556-564):
```bash
            SUCCESS)
                echo "  [${elapsed}/${timeout}s] ✅ Deployment complete!"
                echo ""
                record_deployment_evidence "$service" "SUCCESS"
                show_status
                WAIT_MODE_ACTIVE=false
                return 0
                ;;
```

**Replace with**:
```bash
            SUCCESS)
                echo "  [${elapsed}/${timeout}s] ✅ Deployment complete!"
                echo ""
                # Check if this is a dev service (heuristic: name contains 'dev')
                if [[ "$service" == *"dev"* ]]; then
                    echo "  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo "  ⚠️  Dev services use manual start (zsc noop --silent)"
                    echo ""
                    echo "  Before verifying, start your application:"
                    echo "    ssh $service \"cd /var/www && nohup <your-start-cmd> > /tmp/app.log 2>&1 &\""
                    echo ""
                    echo "  Then verify:"
                    echo "    .zcp/verify.sh $service 8080 / /health /status"
                    echo "  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
                    echo ""
                fi
                record_deployment_evidence "$service" "SUCCESS"
                show_status
                WAIT_MODE_ACTIVE=false
                return 0
                ;;
```

### Verification

```bash
# Test the warning appears for dev services
.zcp/status.sh --wait godev --timeout 10 2>&1 | grep -A5 "manual start"

# Test no warning for stage services
.zcp/status.sh --wait gostage --timeout 10 2>&1 | grep -c "manual start"
# Expected: 0
```

---

## Fix 2: Handoff JSON Critical Notes (P1 - OPTIONAL)

### Problem

While `spawn-subagents.sh` generates comprehensive prompts, the `bootstrap_handoff.json` lacks machine-readable critical notes. Tools parsing the handoff file directly miss this context.

### Current Structure

```json
{
  "session_id": "...",
  "timestamp": "...",
  "status": "ready_for_code_generation",
  "service_handoffs": [
    {
      "dev_hostname": "godev",
      "stage_hostname": "gostage",
      "dev_id": "...",
      "mount_path": "/var/www/godev",
      "runtime": "go",
      "managed_services": [...],
      "recipe_patterns": {...}
    }
  ]
}
```

### Target Structure

```json
{
  "session_id": "...",
  "timestamp": "...",
  "status": "ready_for_code_generation",
  "critical_notes": [
    "Dev services use 'zsc noop --silent' - application won't auto-start",
    "Start app via SSH before running verify.sh",
    "jq/yq/psql/redis-cli are ZCP-only - pipe SSH output to them",
    "Use: ssh {dev} 'curl ...' | jq .  (NOT inside SSH quotes)"
  ],
  "service_handoffs": [...]
}
```

### Implementation

**File**: `zcp/.zcp/lib/bootstrap/steps/finalize.sh`

**Location**: Inside `step_finalize()` function, where `handoff_data` is built (around line 258)

**Find this block** (lines 256-268):
```bash
    # Write handoff file
    local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"
    local handoff_data
    handoff_data=$(jq -n \
        --arg session "$(cat "${ZCP_TMP_DIR:-/tmp}/zcp_session" 2>/dev/null || echo "unknown")" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        --argjson handoffs "$handoffs" \
        '{
            session_id: $session,
            timestamp: $ts,
            status: "ready_for_code_generation",
            service_handoffs: $handoffs
        }')
```

**Replace with**:
```bash
    # Write handoff file
    local handoff_file="${ZCP_TMP_DIR:-/tmp}/bootstrap_handoff.json"

    # Critical notes for subagent context (machine-readable)
    local critical_notes
    critical_notes=$(jq -n '[
        "Dev services use zsc noop --silent - application will NOT auto-start after deploy",
        "You MUST start the app via SSH before verifying: ssh {dev} \"cd /var/www && nohup <cmd> > /tmp/app.log 2>&1 &\"",
        "Tool availability: jq, yq, psql, redis-cli are on ZCP only - NOT inside containers",
        "Correct pattern: ssh {dev} \"curl ...\" | jq .  (pipe OUTSIDE SSH, not inside)",
        "HTTP 000 from verify.sh means server not running - start it first",
        "Stage services auto-start (start: ./app) - no manual start needed for stage"
    ]')

    local handoff_data
    handoff_data=$(jq -n \
        --arg session "$(cat "${ZCP_TMP_DIR:-/tmp}/zcp_session" 2>/dev/null || echo "unknown")" \
        --arg ts "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
        --argjson notes "$critical_notes" \
        --argjson handoffs "$handoffs" \
        '{
            session_id: $session,
            timestamp: $ts,
            status: "ready_for_code_generation",
            critical_notes: $notes,
            service_handoffs: $handoffs
        }')
```

### Verification

```bash
# Run finalize step and check output
.zcp/bootstrap.sh step finalize 2>/dev/null | jq '.data.service_handoffs'

# Check handoff file has critical_notes
jq '.critical_notes' /tmp/bootstrap_handoff.json
# Expected: Array of 6 strings
```

---

## Implementation Checklist

```
[ ] Fix 1: status.sh manual start warning
    [ ] Edit zcp/.zcp/status.sh
    [ ] Find SUCCESS case in wait_for_deployment() (~line 556)
    [ ] Add dev service detection and warning block
    [ ] Test with mock dev service name
    [ ] Test with mock stage service name (no warning)

[ ] Fix 2: finalize.sh critical_notes (optional)
    [ ] Edit zcp/.zcp/lib/bootstrap/steps/finalize.sh
    [ ] Find handoff_data construction (~line 256)
    [ ] Add critical_notes array
    [ ] Include in jq output
    [ ] Test bootstrap step finalize
    [ ] Verify JSON structure
```

---

## Testing Protocol

### Unit Tests

```bash
# 1. Test status.sh warning for dev services
SERVICE="testdev"
# Mock: Manually trigger the SUCCESS branch output
bash -c '
service="testdev"
if [[ "$service" == *"dev"* ]]; then
    echo "WARNING TRIGGERED FOR DEV"
else
    echo "NO WARNING (stage)"
fi
'
# Expected: WARNING TRIGGERED FOR DEV

# 2. Test no warning for stage
bash -c '
service="teststage"
if [[ "$service" == *"dev"* ]]; then
    echo "WARNING TRIGGERED FOR DEV"
else
    echo "NO WARNING (stage)"
fi
'
# Expected: NO WARNING (stage)
```

### Integration Test

```bash
# Full bootstrap flow (requires active Zerops project)
.zcp/bootstrap.sh step finalize
.zcp/bootstrap.sh step spawn-subagents

# Verify handoff contains critical_notes
jq 'has("critical_notes")' /tmp/bootstrap_handoff.json
# Expected: true

# Verify subagent prompt still contains embedded context
jq -r '.data.instructions[0].subagent_prompt' /tmp/bootstrap_spawn.json | grep -c "zsc noop"
# Expected: >= 1
```

---

## Rollback

If issues occur:

### Fix 1 Rollback
```bash
cd zcp/.zcp
git checkout status.sh
```

### Fix 2 Rollback
```bash
cd zcp/.zcp/lib/bootstrap/steps
git checkout finalize.sh
```

---

## Architecture Context

```
┌─────────────────────────────────────────────────────────────────────┐
│                        BOOTSTRAP FLOW                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  plan → recipe-search → import → mount → wait-services               │
│                                    │                                 │
│                                    ▼                                 │
│                              ┌──────────┐                            │
│                              │ finalize │ ◄── Fix 2: Add critical_notes
│                              └────┬─────┘                            │
│                                   │                                  │
│                                   ▼                                  │
│                         bootstrap_handoff.json                       │
│                                   │                                  │
│                                   ▼                                  │
│                          spawn-subagents                             │
│                          (builds prompts)                            │
│                                   │                                  │
│                    ┌──────────────┼──────────────┐                   │
│                    ▼              ▼              ▼                   │
│               Subagent 1    Subagent 2    Subagent N                 │
│                    │              │              │                   │
│                    ▼              ▼              ▼                   │
│               ┌─────────┐   ┌─────────┐   ┌─────────┐                │
│               │ deploy  │   │ deploy  │   │ deploy  │                │
│               └────┬────┘   └────┬────┘   └────┬────┘                │
│                    │              │              │                   │
│                    ▼              ▼              ▼                   │
│               ┌─────────┐   ┌─────────┐   ┌─────────┐                │
│               │status.sh│   │status.sh│   │status.sh│                │
│               │ --wait  │   │ --wait  │   │ --wait  │                │
│               └────┬────┘   └────┬────┘   └────┬────┘                │
│                    │              │              │                   │
│                    ▼              ▼              ▼                   │
│           ◄── Fix 1: Show manual start warning for dev ──►          │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Success Criteria

| Metric | Before | After |
|--------|--------|-------|
| Subagent knows dev needs manual start | Via prompt only | Prompt + status.sh reminder |
| HTTP 000 errors after deploy | Common | Prevented by warning |
| Handoff has machine-readable notes | No | Yes (Fix 2) |
| Agent recovery after context loss | Manual lookup | Critical notes in handoff |

---

## Files Modified

| File | Lines Changed | Purpose |
|------|---------------|---------|
| `zcp/.zcp/status.sh` | +12 | Add dev service manual start warning |
| `zcp/.zcp/lib/bootstrap/steps/finalize.sh` | +10 | Add critical_notes to handoff JSON |

**Total**: ~22 lines of targeted changes

---

## Notes for Implementer

1. **Fix 1 is defense-in-depth** - spawn-subagents.sh already documents manual start, but agents may skip reading the full prompt. The status.sh warning catches this at runtime.

2. **Fix 2 is forward-compatible** - Adding critical_notes doesn't break existing consumers; they simply ignore the new field.

3. **Heuristic for dev detection** (`*dev*`) is intentional - matches convention used throughout ZCP (godev, nodedev, bundev, etc.). If naming convention changes, update the pattern.

4. **No breaking changes** - Both fixes add information without removing or modifying existing behavior.
