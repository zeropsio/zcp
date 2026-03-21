# Deep Review Report: Workflow Trimming & Stability — Review 1

**Date**: 2026-03-21
**Reviewed version**: `docs/spec-bootstrap-deploy.md` + full workflow implementation
**Knowledge team**: kb-research (zerops-knowledge), kb-verifier (platform-verifier)
**Analysis team**: correctness, architecture, security, adversarial
**Focus**: Trim ballast, strengthen stability and flow cleanliness for LLM consumption
**Resolution method**: Evidence-based (no voting)

---

## Stage 1: Knowledge Base

### Factual Brief (kb-research)

**Structural duplications confirmed:**
1. `extractDeploySection()` in `tools/workflow_strategy.go:105-118` — exact copy of `workflow.ExtractSection()` in `bootstrap_guidance.go:129-142`
2. `isManagedNonStorage()` prefix list in `tools/workflow_checks.go:327-332` — copy-paste from `workflow/managed_types.go:14-19`
3. `strategySectionMap` alias in `tools/workflow_strategy.go:22` — unnecessary indirection

**Dead code confirmed:**
- `DeployState.UpdateTarget()` (deploy.go:239-251) — zero prod callers
- `DeployState.DevFailed()` (deploy.go:275-282) — zero prod callers
- `DeployTarget.Error` + `LastAttestation` fields — only written by dead UpdateTarget
- `Environment` type (environment.go) — threaded through all BuildResponse but `_ ignored` everywhere
- `Engine.Environment()` method — never called
- `RefreshRegistry()` — zero prod callers
- `StepCategory` type — defined, serialized to JSON, but never branched on anywhere

**Triplicated patterns:**
- `CompleteStep()` x3 (bootstrap, deploy, cicd) — near-identical logic
- `SkipStep()` x2 (bootstrap, deploy) — near-identical logic
- `BuildResponse()` x3 — similar progress-summary patterns with separate types

**Guidance chain:** 5 files participate in one bootstrap guidance string (bootstrap.go → bootstrap_guide_assembly.go → guidance.go → bootstrap_guidance.go → content)

**Response noise:** `intent` (echo), `index` (redundant), `category` (never branched), `planMode` (echo), `availableStacks` at generate step (noise after types chosen)

### Platform Verification Results (kb-verifier)

| Claim | Status | Evidence |
|-------|--------|----------|
| discover returns unmasked secrets | **CONFIRMED** | Raw passwords/keys visible |
| 6 checks for dynamic runtime | **REFUTED** → max 5, class-dependent | zcpx got 2 as worker-like |
| error_logs returns "info" not "fail" | **PARTIAL** | Returns "pass" when no errors |
| API uses "ACTIVE" not "RUNNING" | Observed | Code accepts both via checkServiceStatusAny |
| Project NON_CONFORMANT | **CONFIRMED** | System prompt + logical consistency |

---

## Stage 2: Analysis Reports

### Correctness Analysis (8 findings, all VERIFIED)
- [F1] `extractDeploySection()` duplicated — MAJOR
- [F2] `isManagedNonStorage()` prefix list duplicated — MAJOR
- [F3] `strategySectionMap` unnecessary alias — MINOR
- [F4] `UpdateTarget()` + `DevFailed()` dead code — MAJOR
- [F5] `DeployTarget.Error` + `LastAttestation` unused — MAJOR
- [F6] `StepCategory` never branched on — MAJOR
- [F7] `getCoreSection()` called 3x per step — MINOR (cached by provider)
- [F8] Response echo fields (intent, index, category, planMode) — MAJOR (conditional)

### Architecture Analysis (20 findings, all VERIFIED)
- Assessment: SOUND — no structural changes required
- [F1] 3 dead functions confirmed (Environment, DevFailed, RefreshRegistry)
- [F2] StepCategory: data-pass-through, never branched on
- [F3] Dead Environment type (20 lines) — delete entirely
- [F4] 5-file guidance chain: COMPLEX but SOUND — justified by separation of concerns, 350L limit
- [F5] cicd_guidance.go (26L): minimal but justified by symmetry
- [F6] bootstrap_checks.go (24L): type-only file, candidate for merge but acceptable
- [F7] Response type redundancy: 3x Progress/StepInfo/StepOutSum — keep separate (coupling cost > duplication)
- [F8] Router: appropriate complexity for 5 project states

### Security Analysis (8 findings, 7 VERIFIED)
- [F1] **CRITICAL**: SSH error leakage in `classifySSHError()` — credentials exposed to MCP response
- [F2] Dead code surface (UpdateTarget, DevFailed) — security surface area
- [F3] Duplicate managed prefix lists — divergence risk
- [F4] Session ownership: PID liveness, no auth on resume — acceptable risk
- [F5] Env var exposure in discovery — intentional design, mitigated
- [F6] Step checker closures — API access during bootstrap, code review required
- [F7] Environment dead code — hygiene
- [F8] Shell injection — VERIFIED SAFE

### Adversarial Analysis (10 challenges)
- [C1] CompleteStep triplication: REAL but unification NOT obviously beneficial — interface complexity defeats purpose
- [C2] 5-file guidance chain: JUSTIFIED by separation of concerns
- [C3] Dependencies field removal: SOUND (API is authoritative)
- [G1] **Genuine gap**: CompleteStep checker integration is asymmetric — state.CompleteStep has no validation, only engine.BootstrapComplete runs checkers
- [G2] **Genuine gap**: PlanModeDev naming ambiguous — "dev" suggests development, actually means "runtime-only, no stage"
- [G3] Doc error: spec says "6 checks" but max is 5
- Counter-arguments: availableStacks at generate is useful for LLM context recovery (not pure noise); removing Decisions map from ServiceMeta loses future extensibility

---

## Stage 3: Evidence-Based Resolution

### Findings by Evidence Strength

#### VERIFIED (confirmed by KB or independent check)

| # | Finding | Severity | Evidence | Source |
|---|---------|----------|----------|--------|
| V1 | `extractDeploySection()` duplicates `ExtractSection()` | MAJOR | Byte-for-byte identical, tools/workflow_strategy.go:105-118 vs workflow/bootstrap_guidance.go:129-142 | Correctness F1, Architecture confirmed |
| V2 | `isManagedNonStorage()` duplicates prefix list | MAJOR | Same 12 prefixes in tools/workflow_checks.go:327 and workflow/managed_types.go:14 | Correctness F2, Security F3 |
| V3 | `strategySectionMap` is unnecessary alias | MINOR | `var strategySectionMap = workflow.StrategyToSection` at line 22, single caller at line 91 | Correctness F3 |
| V4 | `UpdateTarget()` + `DevFailed()` are dead | MAJOR | Zero prod callers confirmed by grep | Correctness F4, Security F2, Architecture F1 |
| V5 | `DeployTarget.Error` + `LastAttestation` unused | MAJOR | Only written by dead UpdateTarget | Correctness F5 |
| V6 | `Environment` type + `Engine.Environment()` dead | MINOR | Threaded but `_ ignored` in all BuildResponse/buildGuide | Architecture F1/F3 |
| V7 | `RefreshRegistry()` dead | MINOR | Zero prod callers | Architecture F1 |
| V8 | `StepCategory` never branched on | MAJOR | 3 constants defined, serialized to JSON, zero if/switch usage | Correctness F6, Architecture F2 |
| V9 | Spec says "6 checks" but max is 5 | MINOR | Code has 5 checks for dynamic, class-dependent 1-5 | Platform verification + Adversarial G3 |
| V10 | Discover returns unmasked secrets | Accepted | Raw passwords confirmed in live API response | Platform verification |

#### LOGICAL (follows from verified facts)

| # | Finding | Severity | Reasoning Chain | Source |
|---|---------|----------|----------------|--------|
| L1 | Response echo fields are low-value | MAJOR | `intent` echoes input agent already has; `index` redundant with array position; `planMode` echoes plan state | Correctness F8 |
| L2 | `availableStacks` at generate step is partially noise | MINOR | Agent already chose types at discover; BUT useful for LLM context recovery | Correctness F8, Adversarial counter |
| L3 | CompleteStep triplication acceptable | INFO | 80L total; unification adds interface complexity exceeding savings | Adversarial C1 |
| L4 | 5-file guidance chain is justified | INFO | Each layer has single responsibility; collapsing forces files past 350L | Architecture F4, Adversarial C2 |

#### UNVERIFIED (flagged but not confirmed)

| # | Finding | Severity | Why Unverified | Source |
|---|---------|----------|---------------|--------|
| U1 | SSH error leakage exposes credentials | CRITICAL | Code path confirmed (classifySSHError → PlatformError → MCP JSON), but actual credential content in SSH stderr not tested end-to-end | Security F1 |
| U2 | PlanModeDev naming causes LLM confusion | MINOR | Adversarial claim, no evidence of actual agent confusion | Adversarial G2 |

### Disputed Findings

| # | Finding | Position A | Evidence A | Position B | Evidence B | Resolution |
|---|---------|-----------|-----------|-----------|-----------|------------|
| D1 | Unify CompleteStep? | Correctness: "triplicated = bad" | 80L of duplicate code | Adversarial: "unification adds interface complexity" | Go has no generics for this pattern without interface overhead | **KEEP SEPARATE** — 80L is acceptable; interface coupling > line savings |
| D2 | Remove availableStacks at generate? | Correctness: "noise after types chosen" | Agent already has types | Adversarial: "useful for LLM context recovery" | Session interruption needs catalog access | **KEEP BUT OPTIMIZE** — only inject at discover, not generate |
| D3 | Remove response echo fields? | Correctness: "intent/index/category/planMode are noise" | Never branched on by code | Adversarial: implied context recovery value | No evidence agents read these for recovery | **REMOVE category; KEEP intent** — category proven useless; intent has marginal recovery value at near-zero cost |

### Key Insights from Knowledge Base

1. **The "6 checks" claim in the spec is wrong** — max 5 checks for dynamic runtime, class-dependent 1-5. The spec should document this variability.
2. **CI/CD workflow has no knowledge injection** — it bypasses `assembleGuidance()` entirely, unlike bootstrap and deploy. This is either intentional simplicity or a gap.
3. **The deploy workflow's per-target tracking (UpdateTarget/DevFailed/Error/LastAttestation) was scaffolding that was never wired into prod paths** — the iteration model via ResetForIteration() superseded it entirely.
4. **CompleteStep checker integration is asymmetric** (Adversarial G1) — `BootstrapState.CompleteStep()` has no validation; only `Engine.BootstrapComplete()` runs checkers. This is by design (checkers are optional engine-level gates) but could be documented more clearly.

---

## Action Items

### Must Address (VERIFIED Critical + Major)

| # | Item | Evidence | Effort |
|---|------|----------|--------|
| A1 | Delete `extractDeploySection()` from tools/workflow_strategy.go, use `workflow.ExtractSection()` | V1: byte-identical duplicate | 1 deletion + 1 import change |
| A2 | Consolidate managed prefix list: delete local copy in workflow_checks.go, call `workflow.isManagedService()` + storage filter | V2: divergence risk | 10L deleted, 5L wrapper |
| A3 | Delete `strategySectionMap` alias, use `workflow.StrategyToSection` directly | V3: unnecessary indirection | 2 line changes |
| A4 | Delete `DeployState.UpdateTarget()` method | V4: zero prod callers | 13L deleted + test update |
| A5 | Delete `DeployState.DevFailed()` method | V4: zero prod callers | 8L deleted + test update |
| A6 | Delete `DeployTarget.Error` + `LastAttestation` fields | V5: unused | 2 fields + writes cleanup |
| A7 | Delete `Environment` type, `environment.go`, `Engine.Environment()`, remove `env` parameter from all BuildResponse calls | V6: dead weight threaded everywhere | ~40L + test cleanup |
| A8 | Delete `RefreshRegistry()` | V7: zero prod callers | ~10L + test update |
| A9 | Remove `StepCategory` type from JSON response (keep in internal StepDetail metadata) | V8: never branched, false API signal | Remove from BootstrapStepInfo |

### Should Address (LOGICAL Critical + Major, VERIFIED Minor)

| # | Item | Evidence | Effort |
|---|------|----------|--------|
| B1 | Fix spec "6 checks" → document class-dependent check counts (1-5) | V9: verified max 5 | Spec edit |
| B2 | Stop injecting `availableStacks` at generate step (keep at discover only) | L2 + D2 resolution | Change `stackSteps` map |
| B3 | Remove `BootstrapStepInfo.Index` field from response | L1: redundant with array position | 1 field removal |

### Investigate (UNVERIFIED but plausible)

| # | Item | Why investigate | Source |
|---|------|----------------|--------|
| I1 | SSH error leakage in `classifySSHError()` | Code path shows raw SSH stderr → MCP JSON; may contain credentials | Security F1 (U1) |
| I2 | PlanModeDev naming ambiguity | "dev" could confuse LLMs into thinking it means "development environment" vs "runtime-only mode" | Adversarial G2 (U2) |
| I3 | CI/CD knowledge injection gap | CI/CD bypasses `assembleGuidance()` — intentional or missing? | KB research finding |
| I4 | CompleteStep checker contract test | State.CompleteStep() has no validation; Engine.BootstrapComplete() is the only gated path | Adversarial G1 |

---

## Change Log (for Revised Spec)

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | Spec §4 Verification | Fix "6 checks" → "up to 5 checks, class-dependent" | KB-PLATFORM: live API returns 2 for worker, 5 max for dynamic | Platform verifier |
| 2 | Spec §5.2 ServiceMeta | Remove Dependencies, Type, Status, Decisions from struct definition | V4-V6: already removed in code | Recent commits 998dd33, 4e358aa |
| 3 | Spec §2 System Model | Remove Environment from Engine description | V6: dead type, never used | Architecture F3 |
| 4 | Spec §3.2 Step 5 | Document class-dependent verification checks table | V9: code classifies by runtime class | Platform verifier + code inspection |
