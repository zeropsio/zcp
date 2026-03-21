# Implementation Plan: Bootstrap Guidance Chain Fixes

**Date**: 2026-03-20
**Source**: Deep review of `~/.claude/plans/whimsical-fluttering-sutherland.md` (Bootstrap Workflow Chain Analysis)
**Branch**: v2
**Status**: Ready to implement

---

## Context

The original analysis identified 8 issues (P0-P7) in the bootstrap guidance chain — how knowledge is assembled and delivered to the LLM at each step. After deep review against the current codebase (post-commit `b17191d` which redesigned guidance assembly), 3 issues are resolved, 3 should be implemented, and 2 are deferred.

## Issue Status Summary

| Issue | Title | Status | Action |
|-------|-------|--------|--------|
| **P0** | Iteration delta erases all context | **IMPLEMENT** | Critical — prepend delta instead of replacing |
| P1 | Agent prompt template unreachable | **RESOLVED** | Consolidated `deploy` section now used directly |
| **P2** | Generate step context size (12-25 KB) | **DEFER** | Not a real problem — modern LLMs handle this; optimize selectively later |
| P3 | Dead `deploy` section | **RESOLVED** | Section is actively used |
| **P4** | Env var discovery dual path | **IMPLEMENT** | Clarify guidance — document auto-storage, remove manual instructions |
| P5 | Content duplication | **DEFER** | Intentional (self-contained sections); trivial size impact on 1M context |
| **P6** | Verification text vs checker mismatch | **IMPLEMENT** | Align 3 mismatched verification strings |
| P7 | deploy-overview/standard redundancy | **RESOLVED** | Consolidated into single deploy section |

### New findings from review

| Finding | Source | Action |
|---------|--------|--------|
| Tier 2 iteration guidance lacks env var checklist | Correctness + Architecture | Fix as part of P0 |
| Agent prompt (185 lines) always included for single-service deploy | KB-research | **DEFER** — prompt serves as deploy model reference, not just agent template |

---

## What to Implement

### 1. P0 — Iteration Delta: Prepend, Don't Replace

**Priority**: CRITICAL
**Evidence**: `guidance.go:28-32` — `return delta` completely replaces base guidance + knowledge layers
**Impact**: On deploy retry, LLM loses ALL runtime knowledge, env var names, schema rules, deploy flow
**Risk**: This is the most likely failure path — iteration exists for hard cases that need MORE context

#### Analysis

**Current flow** (iteration > 0):
```
assembleGuidance() → BuildIterationDelta() returns non-empty → return delta (DONE)
```
LLM receives: 4-10 lines of escalating recovery template. No env vars, no schema, no deploy flow.

**Proposed flow**:
```
assembleGuidance() → Layer 1: static guidance → Layer 2: knowledge → PREPEND iteration delta
```
LLM receives: iteration delta header (focused diagnosis) + full deploy guidance + knowledge.

**Architecture verification** (from architecture analyst):
- Deploy workflow uses `assembleKnowledge()` directly, NOT `assembleGuidance()` — zero cross-workflow impact
- `BuildIterationDelta()` only fires for `step == StepDeploy` — no other step affected
- Layer-based architecture supports this naturally — delta becomes layer 0 (prepended)
- Type-safe iteration counter, no state corruption risk

**Security verification** (from security analyst):
- Env vars preserved are NAMES only (`map[string][]string`) — no secret values
- `lastAttestation` contains step descriptions, not credentials
- Session state expansion is zero — same data, different assembly order

#### Implementation

**File**: `internal/workflow/guidance.go`

**Change**: Lines 28-32 — remove early return, prepend delta to assembled guide.

```go
// Before (current):
if params.Iteration > 0 {
    if delta := BuildIterationDelta(params.Step, params.Iteration, params.Plan, params.LastAttestation); delta != "" {
        return delta
    }
}

// After:
var iterationDelta string
if params.Iteration > 0 {
    iterationDelta = BuildIterationDelta(params.Step, params.Iteration, params.Plan, params.LastAttestation)
}

// Layer 1: Static guidance
guide := resolveStaticGuidance(params.Step, params.Plan, params.FailureCount)

// Layers 2-4: Knowledge injection
if extra := assembleKnowledge(params); extra != "" {
    guide += "\n\n---\n\n" + extra
}

// Layer 0: Iteration delta prepended (focused diagnosis first, full context follows)
if iterationDelta != "" {
    guide = iterationDelta + "\n\n---\n\n" + guide
}

return guide
```

**Also fix**: Tier 2 escalation in `bootstrap_guidance.go:102-104` — add env var hint.

```go
// Before:
case iteration <= 2:
    guidance = `DIAGNOSE: zerops_logs severity="error" since="5m"
FIX the specific error, then redeploy + verify.`

// After:
case iteration <= 2:
    guidance = `DIAGNOSE: zerops_logs severity="error" since="5m"
If error mentions env var, connection, or "undefined":
→ zerops_discover includeEnvs=true — compare var names with zerops.yml envVariables
FIX the specific error, then redeploy + verify.`
```

#### Tests (RED first)

**File**: `internal/workflow/bootstrap_guidance_test.go`

```go
func TestAssembleGuidance_IterationPrependsNotReplaces(t *testing.T) {
    // Verify iteration delta is prepended, not replacing base guidance
    // iteration > 0 should still contain base deploy guidance + knowledge
}

func TestAssembleGuidance_IterationPreservesEnvVars(t *testing.T) {
    // Verify env vars from DiscoveredEnvVars appear even on iteration > 0
    // (for deploy workflow which injects env vars at deploy step)
}

func TestBuildIterationDelta_Tier2_ContainsEnvVarHint(t *testing.T) {
    // Verify tier 2 (iterations 1-2) mentions env var validation
}
```

**Affected layers**: Unit (workflow), Tool (workflow_checks — no change needed), Integration (bootstrap flow)

---

### 2. P4 — Env Var Dual Path: Clarify Guidance

**Priority**: MODERATE
**Evidence**: Provision guidance tells LLM to call `zerops_discover includeEnvs=true`, but `checkProvision()` auto-stores env var names via `engine.StoreDiscoveredEnvVars()` — LLM doesn't know the system handles it.

#### Analysis

**Current behavior**:
- Guidance says: "Call `zerops_discover includeEnvs=true` after importing" (bootstrap.md:119-149)
- Checker silently auto-stores env var names on step completion (workflow_checks.go:110-123)
- If LLM skips discover, env vars are still captured
- If LLM calls discover, it gets VALUES (useful for understanding) but the NAMES are stored independently

**Design assessment** (confirmed by all 4 analysts): The dual path is **intentionally sound** — defense-in-depth. The checker is the authoritative path. The guidance path helps the LLM understand what's available.

**The fix is documentation-only**: Don't remove either path. Just make the automatic storage visible in guidance.

#### Implementation

**File**: `internal/content/workflows/bootstrap.md` — provision section (lines 119-149)

**Change**: Replace "Env var discovery protocol (mandatory before generate)" heading and instructions with clearer text:

```markdown
### Env var discovery

After importing services and confirming they reach RUNNING status, env vars are **automatically captured** when you complete this step. The generate step guide will include the discovered variable names as `${hostname_varName}` references.

**Optional but recommended**: Call `zerops_discover includeEnvs=true` to see the full list of available variables and their actual values. This helps you understand what's available before writing zerops.yml.

Common patterns by service type:
[... keep existing table ...]
```

Key changes:
1. Remove "mandatory" framing — automatic capture handles it
2. Add note that env vars appear in generate step guide automatically
3. Keep the `zerops_discover includeEnvs=true` call as recommended (not required)
4. Keep the env var patterns table (still useful context)

#### Tests

No code changes — content-only update. Existing tests pass unchanged.

Verify content change doesn't break existing test assertions:
- `TestResolveGuidance` cases for "provision" check for "import.yml" and "discovery protocol" — update expected substring if heading changes.

---

### 3. P6 — Verification Text Alignment

**Priority**: MODERATE
**Evidence**: 3 of 6 step verification texts overstate what checkers actually validate.

#### Analysis

| Step | Current Verification Text | What Checker Does | Mismatch |
|------|--------------------------|-------------------|----------|
| **provision** | "...AND dev filesystems mounted AND env vars recorded" | Checks services exist + RUNNING, type match, env vars stored. Does NOT check mounts. | **YES** — remove mount claim |
| **generate** | "...AND app code exposes /health and /status endpoints" | Validates zerops.yml structure (10-point check). Does NOT check app code endpoints. | **YES** — app endpoints checked at verify, not generate |
| **deploy** | "...AND zerops_verify returns healthy for each service" | Checks service RUNNING + subdomain enabled. Does NOT call zerops_verify. | **YES** — zerops_verify happens at verify step |

#### Implementation

**File**: `internal/workflow/bootstrap_steps.go`

```go
// provision — remove mount claim
Verification: "SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND env vars recorded in session state.",

// generate — remove app code endpoint claim
Verification: "SUCCESS WHEN: zerops.yml exists with correct setup entry AND env var references match discovered variables AND deployment config is valid (ports, deployFiles, start command).",

// deploy — remove zerops_verify claim
Verification: "SUCCESS WHEN: all runtime services deployed (RUNNING status) AND subdomains enabled for services with ports.",
```

#### Tests

**File**: `internal/workflow/bootstrap_guidance_test.go` or `bootstrap_steps_test.go`

No existing tests assert on verification text content. Add tests to ensure alignment:

```go
func TestStepDetails_VerificationText_NoMountClaim(t *testing.T) {
    // Provision step should NOT claim mount validation
}

func TestStepDetails_VerificationText_NoAppCodeClaim(t *testing.T) {
    // Generate step should NOT claim app endpoint validation
}

func TestStepDetails_VerificationText_NoVerifyClaim(t *testing.T) {
    // Deploy step should NOT claim zerops_verify
}
```

---

## What to Defer

### P2 — Generate Context Size (12-25 KB)

**Rationale**: All analysts agree this is not a correctness issue. Modern LLMs (Opus 4.6, 1M context) handle 12-25 KB trivially. The information IS needed — each of the 7 pieces serves a distinct purpose:
1. Base guidance: WHAT to do
2. Mode section: HOW for this mode
3. Runtime briefing: HOW for this framework
4. Dependency briefings: WHAT services are available
5. Env vars: WHICH variables to use
6. Schema: SYNTAX reference
7. Rules: PITFALLS to avoid

Removing any piece means the LLM either guesses or makes extra round-trips. The adversarial analyst correctly notes the real improvement would be **selective knowledge injection** (skip runtime briefing if recipe was already loaded) — but that's a new feature, not a fix.

### P5 — Content Duplication

**Rationale**: Duplication is intentional — each bootstrap.md section is designed to be self-contained. The 185-line agent prompt MUST repeat env var rules, deploy flow, and platform constraints because subagents have no prior context. For single-service bootstraps, the prompt serves as a comprehensive deploy model reference. Deduplication would either break agent self-containment or require a cross-reference system that adds complexity without clear benefit.

Quantified: ~2-3 KB of repeated content across 825-line file. On a 1M context window, this is noise.

---

## Implementation Order

```
1. Write failing tests for P0 (iteration prepend, env var hint)     [RED]
2. Implement P0 fix in guidance.go + bootstrap_guidance.go           [GREEN]
3. Run all tests: go test ./internal/workflow/... -count=1           [VERIFY]
4. Update bootstrap_steps.go verification text (P6)                  [TRIVIAL]
5. Update bootstrap.md provision guidance (P4)                       [CONTENT]
6. Run full test suite: go test ./... -count=1 -short                [VERIFY]
7. Run lint: make lint-fast                                          [VERIFY]
```

**Estimated scope**: ~30 lines of code changes + ~20 lines of content changes + ~40 lines of new tests.

---

## Evidence Trail

### Analysts who verified these findings:
- **Correctness**: Traced all 5 data flows end-to-end, confirmed P0 is survivable but fragile on tier 2
- **Architecture**: Verified P0 fix has zero cross-workflow impact (deploy uses different code path), confirmed layer-based architecture supports prepend naturally
- **Security**: Verified P0 fix introduces no new security risks (names-only preservation, no secret leakage)
- **Adversarial**: Challenged all 6 points, confirmed P0 fix is warranted (narrower than stated but real), argued P2/P5 should be deferred (correct)

### Adversarial findings that were INCORRECT (analyst read old plan, not current code):
- "BootstrapTarget type not implemented" — EXISTS at `validate.go:30`
- "service_meta.go missing" — EXISTS at 131 lines
- "StepChecker not implemented" — EXISTS and used in 17 files
- "Hard checkers don't exist" — EXISTS in `workflow_checks.go`, `workflow_checks_generate.go`, `workflow_checks_strategy.go`

### KB-verifier finding that was INCORRECT:
- "No `zerops_deploy` tool exists" — EXISTS at `internal/tools/deploy.go`
