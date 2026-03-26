# Analysis: Adopting Pre-Existing Services — Iteration 1

**Date**: 2026-03-26
**Scope**: internal/workflow/, internal/tools/, internal/content/, docs/
**Agents**: KB (zerops-knowledge), Primary (architecture+feasibility), Adversarial (challenge)
**Complexity**: Deep (ultrathink, 4 agents)
**Task**: Design how ZCP can work with pre-existing runtime services that weren't bootstrapped via ZCP

---

## Summary

Bootstrap **already supports adoption** through the `IsExisting` flag on `RuntimeTarget` and `EXISTS` resolution on dependencies. The flag is load-bearing in the provision checker (`workflow_checks.go:64`), has 8+ unit tests, and a dedicated E2E scenario. The correct approach is **extending bootstrap with better guidance and minor conditional logic**, NOT creating a separate "adopt" workflow. A new workflow would duplicate 95% of bootstrap's engine, session management, step checkers, and guidance assembly — a textbook DRY violation.

The core gap is **guidance**, not infrastructure: bootstrap's discover step for NON_CONFORMANT projects needs clearer adoption guidance (how to classify existing services, how to handle existing code, how to map env vars). The engine, checkers, and ServiceMeta system need only minor extensions.

---

## Findings by Severity

### Critical

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| C1 | Bootstrap already supports adoption via `IsExisting` — creating a new workflow duplicates validated, tested logic | `workflow_checks.go:64` branches stage validation; 8+ tests in `workflow_checks_test.go:394,424,478,537,573,720`; E2E at `bootstrap_workflow_test.go:299-353` | Adversarial CH1, self-verified |
| C2 | `routeNonConformant()` already routes NON_CONFORMANT projects to bootstrap/deploy — it is NOT a dead end | `router.go:129-158` offers bootstrap (p1) + debug (p2) when no metas; deploy (p1) + bootstrap (p2) when metas exist | KB C1, self-verified |
| C3 | Existing services that were created but never deployed (`READY_TO_DEPLOY` status) would fail provision checker if treated as `IsExisting=true` — checker expects RUNNING/ACTIVE for existing services | `workflow_checks.go:55` calls `checkServiceRunning()` for dev regardless of IsExisting; line 64-68 only differentiates stage expectations | Adversarial MF3, self-verified |

### Major

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| M1 | Guidance gap: bootstrap.md discover section lists 3 options for NON_CONFORMANT but option (c) "work with existing" has NO concrete steps for setting `IsExisting=true` in the plan or mapping existing services | `bootstrap.md:27` — "Options: (a) add new... (b) delete... (c) work with existing" — option (c) is one line, no elaboration | Primary F6 (corrected), self-verified |
| M2 | No mode inference logic exists — when adopting existing services, there's no code to detect whether services follow dev/stage naming convention and suggest appropriate mode | `managed_types.go:66-86` `hasDevStagePattern()` detects patterns for state classification but result isn't available to the agent for per-service mode selection | Primary analysis, self-verified |
| M3 | Generate step has no conditional path for existing code — it always assumes fresh code generation. No mechanism to signal "skip code generation, write zerops.yml only" | `workflow_checks_generate.go:20-74` validates zerops.yml + code exist but doesn't distinguish between generated and pre-existing code | Primary analysis, self-verified |
| M4 | `PreserveExistingCode` does NOT belong in `RuntimeTarget` (plan type) — it's a per-iteration step-level decision, not an immutable plan property. Plan is sealed after discover; code preservation is a generate-step concern | Plan is immutable (`ServicePlan.CreatedAt` set once, no mutation methods); Strategies stored separately in mutable `BootstrapState.Strategies` map — same pattern should apply | Adversarial MF1/MF2, self-verified |
| M5 | `origin` field on ServiceMeta is unnecessary — no behavioral code path would branch on origin. Deploy, strategy, and routing are all origin-agnostic | ServiceMeta consumers: `router.go` (reads mode, strategy), `bootstrap_outputs.go` (writes), `workflow_strategy.go` (reads strategy) — none check origin | Adversarial CH2, self-verified |

### Minor

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| m1 | Primary used "RUNNING" and "ACTIVE" interchangeably — API status for deployed services is `ACTIVE`, not `RUNNING`. `RUNNING` is a Process status | `platform/types.go:29` ServiceStack.Status; `types.go:109` Process status | KB C3 |
| m2 | SSHFS mount (`zerops_mount`) is only available in container mode (zcpx). Local dev must use SSH for file access — adoption on local machine cannot mount | CLAUDE.local.md "NEVER Mount" section | KB S3 |
| m3 | `hasDevStagePattern()` only checks `dev`/`stage` suffixes — "simple" mode services (no suffix) will always be NON_CONFORMANT, which is correct but needs documenting | `managed_types.go:72-84` | KB S2 |

---

## Recommendations (max 7, evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | **Expand bootstrap.md discover section** — add concrete adoption guidance under option (c): how to identify existing services, set `IsExisting=true`, choose mode, handle existing code decision | P0 | `bootstrap.md:27` has 1-line placeholder for "work with existing" | ~150L guidance |
| R2 | **Add adoption-specific provision check** — when `IsExisting=true`, accept `ACTIVE` status for dev service (not just RUNNING); handle `READY_TO_DEPLOY` existing services by prompting user to deploy first or treat as fresh | P0 | `workflow_checks.go:55` doesn't differentiate dev status by IsExisting | ~30L code + 20L tests |
| R3 | **Add generate step conditionality** — store "code preservation" decision in `BootstrapState` (like Strategies map), not in RuntimeTarget. When preserve=true, generate checker requires zerops.yml but skips code endpoint checks | P1 | `workflow_checks_generate.go` has no conditional for existing code; `BootstrapState.Strategies` proves mutable-decision-map pattern | ~50L code + 40L tests |
| R4 | **Expose mode suggestion in discover guidance** — when NON_CONFORMANT, guidance should enumerate existing services with suggested modes based on `hasDevStagePattern()` logic + hostname analysis | P1 | `managed_types.go:66-86` has detection logic but result stays internal | ~40L guidance |
| R5 | **Update spec** — add adoption subsection to discover step (section 3.2) documenting IsExisting semantics, code preservation decision, and NON_CONFORMANT adoption path | P1 | `docs/spec-bootstrap-deploy.md:147-151` lists 3 states but doesn't detail adoption under NON_CONFORMANT | ~80L spec |
| R6 | **Add env var reconciliation guidance** — when adopting services with existing zerops.yml env mappings, guidance should instruct agent to compare discovered env vars against existing zerops.yml references | P2 | Env var references are validated in `workflow_checks_generate.go:96-130`; existing code may use different names than discovery returns | ~30L guidance |
| R7 | **Do NOT add `origin` field to ServiceMeta** — no code path branches on it. If needed later, git history + `BootstrapSession` field already provides audit trail | P2 | Zero consumers would read origin; YAGNI principle | 0L (avoided) |

---

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| C1: IsExisting already load-bearing | VERIFIED | `workflow_checks.go:64`, 8+ tests, E2E test |
| C2: routeNonConformant not a dead end | VERIFIED | `router.go:129-158` |
| C3: READY_TO_DEPLOY existing service fails | VERIFIED | `workflow_checks.go:55` unconditional checkServiceRunning |
| M1: Guidance gap for adoption | VERIFIED | `bootstrap.md:27` — one line |
| M2: No mode inference | VERIFIED | no InferAdoptionMode or equivalent exists |
| M3: Generate has no code-preservation path | VERIFIED | `workflow_checks_generate.go` — no IsExisting branching |
| M4: PreserveExistingCode wrong layer | LOGICAL | Plan immutability pattern + Strategies pattern |
| M5: Origin field unnecessary | LOGICAL | Zero consumers in codebase |

---

## Adversarial Challenges

### Challenged and UPHELD (adversarial was right)

| Challenge | Verdict | Resolution |
|-----------|---------|------------|
| CH1: New workflow is over-engineering | **UPHELD** | Bootstrap engine, session, checkers are all workflow-agnostic. IsExisting is already the adoption signal. Extend bootstrap, don't duplicate. |
| CH2: Origin field is dead weight | **UPHELD** | No consumer code path. YAGNI. |
| CH4: "Same 5-step engine" means it's the same workflow | **UPHELD** | If steps, checkers, session, and guidance structure are identical, it's configuration not a new workflow. |
| MF1: PreserveExistingCode wrong layer | **UPHELD** | Belongs in BootstrapState (mutable), not RuntimeTarget (sealed plan). |
| MF3: Existing READY_TO_DEPLOY services gap | **UPHELD** | Real gap. Provision checker needs IsExisting-aware status validation. |

### Challenged and REJECTED (primary was right)

| Challenge | Verdict | Resolution |
|-----------|---------|------------|
| MF5: "1-2 days, 330L" estimate | **REJECTED** — too aggressive | Guidance changes alone are ~150L; provision checker ~50L; generate conditionality ~90L; spec ~80L; tests ~100L. Realistic: **~500L, 1-1.5 weeks**. |
| MF6: "Bootstrap.md already handles adoption" | **PARTIALLY REJECTED** | It mentions option (c) "work with existing" but has zero concrete guidance. The option exists in text; the implementation guidance does not. |
| MF8: "Guidance routing needs no changes" | **PARTIALLY REJECTED** | `ResolveProgressiveGuidance()` may need adoption-aware sections for generate step (code preservation). Minor, but not zero. |

### Primary findings CONFIRMED

| Finding | Status |
|---------|--------|
| F3: Env var discovery identical for adopted & new | CONFIRMED — API is origin-agnostic |
| F4: Deploy step requires zero changes | CONFIRMED — deploy checker doesn't reference IsExisting |
| RISK1: Silent corruption of existing code | CONFIRMED — real risk, needs guardrails in guidance |
| RISK3: Missing required endpoints | CONFIRMED — adopted code may lack /health, /status |

---

## Recommended Design: Bootstrap Extension (NOT New Workflow)

### Architecture

```
                        ┌─────────────────────────────────┐
                        │  zerops_workflow action="start"  │
                        │     workflow="bootstrap"         │
                        └───────────┬─────────────────────┘
                                    │
                        ┌───────────▼─────────────────────┐
                        │   DISCOVER step                  │
                        │   zerops_discover → classify     │
                        │                                  │
                        │  FRESH → standard bootstrap      │
                        │  CONFORMANT → route to deploy    │
                        │  NON_CONFORMANT → adoption path  │ ← EXPANDED
                        └───────────┬─────────────────────┘
                                    │
            ┌───────────────────────┼──────────────────────┐
            │ (new services)        │ (adoption)           │
            │ IsExisting=false      │ IsExisting=true      │
            ▼                       ▼                      │
    ┌───────────────┐     ┌────────────────┐              │
    │ PROVISION     │     │ PROVISION      │              │
    │ import + env  │     │ env vars ONLY  │ ← CONDITIONAL│
    │ discovery     │     │ (skip import)  │              │
    └───────┬───────┘     └───────┬────────┘              │
            │                     │                        │
            └──────────┬──────────┘                        │
                       ▼                                   │
            ┌──────────────────┐                           │
            │ GENERATE         │                           │
            │ if preserve=true │ ← NEW STATE FIELD         │
            │   zerops.yml only│                           │
            │ else             │                           │
            │   full codegen   │                           │
            └────────┬─────────┘                           │
                     ▼                                     │
            ┌──────────────────┐                           │
            │ DEPLOY (unchanged)│                          │
            └────────┬─────────┘                           │
                     ▼                                     │
            ┌──────────────────┐                           │
            │ CLOSE (unchanged) │                          │
            └──────────────────┘
```

### Data Model Changes

```go
// BootstrapState — ADD one field (like Strategies map pattern):
type BootstrapState struct {
    // ... existing fields ...
    PreserveCode map[string]bool `json:"preserveCode,omitempty"` // hostname -> preserve existing code?
}
```

NO changes to: `RuntimeTarget`, `ServiceMeta`, `WorkflowState`, `ServicePlan`, `Engine`.

### Files to Modify

| File | Change | LOC |
|------|--------|-----|
| `internal/content/workflows/bootstrap.md` | Expand discover § NON_CONFORMANT option (c) with concrete adoption guidance; add generate § code-preservation conditional sections | +150 |
| `internal/tools/workflow_checks.go` | `checkProvision()`: when `IsExisting=true`, accept ACTIVE for dev (not just via checkServiceRunning); handle READY_TO_DEPLOY existing services with guidance message | +30 |
| `internal/tools/workflow_checks_generate.go` | `checkGenerate()`: when `PreserveCode[hostname]=true`, skip app code checks (endpoints), require only zerops.yml | +40 |
| `internal/workflow/bootstrap.go` | Add `PreserveCode` field to BootstrapState; add `StorePreserveCode()` method | +15 |
| `internal/workflow/engine.go` | Add `StorePreserveCode()` delegation method (like `StoreDiscoveredEnvVars`) | +15 |
| `internal/workflow/bootstrap_guidance.go` | Add conditional section extraction for adoption in generate step (code preservation path) | +20 |
| `docs/spec-bootstrap-deploy.md` | Add adoption subsection to Step 1 (discover): IsExisting semantics, code preservation, NON_CONFORMANT adoption | +80 |
| `internal/tools/workflow_checks_test.go` | Adoption-specific provision test cases (ACTIVE existing, READY_TO_DEPLOY existing) | +60 |
| `internal/tools/workflow_checks_generate_test.go` | Code-preservation test cases (skip endpoint checks when preserve=true) | +40 |
| `internal/workflow/bootstrap_test.go` | PreserveCode state management tests | +20 |
| **Total** | | **~470** |

### Implementation Phases

**Phase 1: Guidance + Provision (3-4 days)**
- Expand bootstrap.md discover section with adoption guidance
- Add provision checker IsExisting-aware status validation
- Write tests FIRST (RED), then implement (GREEN)
- No engine changes needed

**Phase 2: Generate Conditionality (2-3 days)**
- Add PreserveCode to BootstrapState
- Add StorePreserveCode() to engine
- Modify generate checker for code preservation
- Add bootstrap_guidance.go adoption sections
- Tests FIRST

**Phase 3: Spec + Polish (1-2 days)**
- Update spec-bootstrap-deploy.md
- E2E test: adopt existing service on live Zerops
- Verify deploy workflow works unchanged post-adoption

**Total: ~1.5 weeks, ~470 LOC, ~120 new tests**

---

## Risk Mitigation

| Risk | Mitigation | Where |
|------|-----------|-------|
| User accidentally overwrites production code | Default to preserve=true in guidance; explicit warning before regenerate | bootstrap.md generate section |
| Adopted code missing /health, /status endpoints | If preserve=true, guidance warns: "verify endpoints exist or deploy may fail verification" | bootstrap.md generate section |
| Mode misclassification | Always ASK user to confirm mode during discover; show detected topology as suggestion only | bootstrap.md discover section |
| Env var name mismatch | Guidance instructs: compare existing zerops.yml envVariables against discovered vars | bootstrap.md generate section |
| READY_TO_DEPLOY existing service | Provision checker detects and returns actionable message: "Service exists but was never deployed. Deploy first or treat as fresh (IsExisting=false)" | workflow_checks.go |

---

## Open Questions (Require Live Verification)

1. **Import with existing hostname + override=false**: Does it silently succeed, error, or partially apply? Affects whether adoption can safely re-import for env var updates. [UNCHECKED]
2. **SSHFS mount on adopted ACTIVE services**: Works in container mode (zcpx). Confirmed blocked in local mode (CLAUDE.local.md). Adoption on local machine requires SSH-only code access. [LOGICAL]
3. **Shared storage discovery**: No API field for mount relationships. Adoption guidance should instruct: "list services, identify shared-storage type, check zerops.yml for run.mount directives." [LOGICAL]
