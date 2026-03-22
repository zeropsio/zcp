# Flow Trace: Bootstrap for Managed-Only Services — Iteration 1
**Date**: 2026-03-22
**Entry point**: `internal/tools/workflow.go:55` → `handleWorkflowAction` → `handleStart`
**Agents**: kb (zerops-knowledge), verifier (platform-verifier), primary (correctness+data-flow), adversarial
**Complexity**: Deep (ultrathink)
**Task**: When bootstrap runs for a single managed service (e.g., "add a PostgreSQL database") with NO runtime services, what is the exact flow? Is it correct? Are changes needed?

## Summary

The managed-only bootstrap flow is an **intentional, tested design path** — not a gap. The system uses `plan=[]` (empty targets) as a sentinel for managed-only mode, documented in code comments across 6+ files and validated by 2 integration tests + 8 unit tests. The flow works end-to-end: discover → provision (import managed service) → skip generate/deploy/close. Two cosmetic improvements are warranted (guidance note + transition message guard), but no structural changes are needed.

---

## Trace

### Step 1: Start Bootstrap
**Location**: `tools/workflow.go:159` → `engine.go:117-131`
**Input**: `zerops_workflow action="start" workflow="bootstrap" intent="add a postgresql database"`
**Action**: Creates session via `InitSessionAtomic`, creates `NewBootstrapState()` with 5 steps, sets step[0] (discover) to `in_progress`, saves state, returns `BuildResponse` with discover guidance + stack catalog.
**Output**: LLM receives discover guidance from `<section name="discover">` in bootstrap.md plus available stacks.

### Step 2: Complete Discover with Empty Plan
**Location**: `tools/workflow_bootstrap.go:41` → `engine.go:194-248`
**Input**: `zerops_workflow action="complete" step="discover" plan=[]`
**Action**:
  - `input.Plan != nil` → routes to `BootstrapCompletePlan` (line 41 comment: `empty plan = managed-only`)
  - `ValidateBootstrapTargets([], ...)` at `validate.go:128-131` — returns `nil, nil` (comment: `Empty targets allowed for managed-only projects`)
  - Attestation: `"Planned targets: "` (empty join — no service info recorded)
  - Plan stored: `&ServicePlan{Targets: []}` — non-nil, passes defense-in-depth check at `engine.go:147`
  - Step advances to provision (step 1)
**Output**: LLM receives provision guidance with import.yml schema from knowledge store.

### Step 3: Provision (managed service)
**Location**: `engine.go:137-191` with checker from `tools/workflow_checks.go:25-26`
**Input**: `zerops_workflow action="complete" step="provision" attestation="db imported, RUNNING, envs discovered"`
**LLM actions before completing**:
  1. Generates import.yml for the managed service (e.g., `postgresql@16`, `mode: NON_HA`)
  2. Calls `zerops_import` — blocks until process completes, returns status
  3. Calls `zerops_discover includeEnvs=true` to verify service exists and record env vars
  4. Submits completion with attestation
**Checker**: `checkProvision` at `workflow_checks.go:36-39` — `plan.Targets` is empty → returns `nil, nil`. No automated validation. This is correct: managed services are verified via `zerops_import` error handling (API errors for invalid YAML/unknown types) and the LLM's own discovery calls. [VERIFIED: integration test at `bootstrap_realistic_test.go:416-426` confirms this exact flow]
**Output**: Step advances to generate (step 2).

### Step 4: Skip Generate
**Location**: `engine.go:250-269` via `bootstrap.go:175-207`
**Input**: `zerops_workflow action="skip" step="generate" reason="no runtime services"`
**Action**: `validateConditionalSkip(plan, "generate")` at `bootstrap.go:315-327` — `len(plan.Targets) == 0` → skip allowed (step is `Skippable: true` at `bootstrap_steps.go:34`).
**Output**: Step marked skipped, advances to deploy (step 3).

### Step 5: Skip Deploy
**Input**: `zerops_workflow action="skip" step="deploy" reason="managed-only project"`
**Action**: Same `validateConditionalSkip` logic. Skip allowed.
**Output**: Step marked skipped, advances to close (step 4).

### Step 6: Skip Close
**Input**: `zerops_workflow action="skip" step="close" reason="managed-only project"`
**Action**: Skip allowed. `state.Bootstrap.Active = false`.
**Post-completion**: `writeBootstrapOutputs` at `bootstrap_outputs.go:22` iterates `plan.Targets` — empty, writes nothing (no metas, no reflog). Session cleaned up.
**Transition message**: `BuildTransitionMessage` at `bootstrap_guide_assembly.go:62-129` produces section headers ("Services", "Deploy Strategy", "What's Next?") with no content entries. Offers deploy/cicd workflows that are irrelevant.
**Output**: Bootstrap complete. 2 completed (discover, provision) + 3 skipped (generate, deploy, close) = 5/5.

## Data Shape Evolution

| Step | Plan.Targets | DiscoveredEnvVars | Checker | Guidance Relevance |
|------|-------------|-------------------|---------|--------------------|
| Start | n/a | nil | n/a | Adequate (stack catalog) |
| Discover | n/a → [] | nil | n/a | Adequate (plan schema shown) |
| Provision | [] | nil → populated | nil,nil (no-op) | Adequate (import.yml schema injected) |
| Generate | [] | populated | n/a (skipped) | n/a |
| Deploy | [] | populated | n/a (skipped) | n/a |
| Close | [] | populated | n/a (skipped) | Transition message: cosmetically poor |

## Failure Paths

| # | At Step | Condition | Result | Handled? | Evidence |
|---|---------|-----------|--------|----------|----------|
| FP1 | Discover | LLM tries to put managed service as RuntimeTarget | Validation error: type not found as runtime or hostname invalid | YES — `validate.go:174` type check | VERIFIED |
| FP2 | Discover | LLM sends plan=null (omits plan field) | Falls to attestation path, no plan stored. Step 2 fails: `"step provision requires plan from discover step"` | YES — `engine.go:147` defense-in-depth | VERIFIED |
| FP3 | Provision | LLM generates invalid import.yml | `zerops_import` returns API error. LLM sees error, retries. | YES — standard MCP error handling | LOGICAL |
| FP4 | Provision | LLM skips env var discovery | Not caught by checker (nil), but provision guidance explicitly calls for `zerops_discover includeEnvs=true`. No downstream impact for managed-only. | PARTIAL — guidance only | VERIFIED |
| FP5 | Generate | LLM tries to complete instead of skip | `checkGenerate` iterates empty targets → vacuous pass. Wastes a step but doesn't break. | YES — degrades gracefully | VERIFIED |

## Findings by Severity

### Major
| # | Finding | Evidence | Source | Adversarial |
|---|---------|----------|--------|-------------|
| F1 | Discover guidance doesn't mention empty plan for managed-only. LLM must independently deduce `plan=[]` from the schema. | bootstrap.md:76-77 shows only `plan=[{runtime:...}]` | Primary (F3) | Downgraded from MAJOR: "one-line note to discover section" is trivial fix. Tests prove LLMs navigate it. |

### Minor
| # | Finding | Evidence | Source | Adversarial |
|---|---------|----------|--------|-------------|
| F2 | Transition message has empty Services/Deploy Strategy sections + irrelevant deploy offerings for managed-only | `bootstrap_guide_assembly.go:62-129` | Primary (F5) + Adversarial (MF3) | Confirmed. Guard with `if len(plan.Targets) > 0`. |
| F3 | Attestation for discover step is semantically empty: `"Planned targets: "` | `engine.go:215-231` | Adversarial (MF1) | Audit trail issue, not functional. |
| F4 | No service meta or reflog entry for managed-only bootstrap | `bootstrap_outputs.go:22-41` | Primary (F6) | Correct by design: managed services are API-authoritative (`bootstrap_outputs.go:21` comment). |
| F5 | `DetectProjectState` classifies managed-only projects as FRESH | `managed_types.go:53` | Primary (F7) | Technically correct: "no runtime services = fresh from bootstrap's perspective". LLM self-corrects via discover. |

### Rejected (by adversarial, confirmed by evidence)
| # | Finding | Original Severity | Rejection Reason | Evidence |
|---|---------|-------------------|------------------|----------|
| R1 | Plan model cannot represent managed-only intent | CRITICAL | **Intentional design.** Comments: `"Empty targets allowed for managed-only projects"` (validate.go:128), `"empty plan = managed-only"` (workflow_bootstrap.go:40), `"managed deps are API-authoritative"` (bootstrap_outputs.go:21). Two integration tests validate this exact flow. | validate.go:128, workflow_bootstrap.go:40, bootstrap_conductor_test.go:146-198, bootstrap_realistic_test.go:353-458 |
| R2 | Provision checker no-op for empty targets | MAJOR | **Correct behavior.** No runtime targets = nothing to check. `zerops_import` provides its own error handling. Managed services verified via discover. | workflow_checks.go:36-39, bootstrap_realistic_test.go:416-426 |
| R3 | Generate/deploy not auto-skipped | MINOR | **Over-engineering.** `Skippable` flag + `validateConditionalSkip` already handle this. Auto-skip would remove LLM agency. | bootstrap.go:314-327, bootstrap_steps.go:13 |

## Recommendations (max 7, evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | Add one-line note to bootstrap.md discover section: "For managed-only projects (no runtime services), submit an empty plan: `plan=[]`" | P2 | bootstrap.md:76-77 lacks managed-only example | Trivial (~1 line) |
| R2 | Guard transition message sections with `if len(plan.Targets) > 0` in `BuildTransitionMessage` to suppress empty Services/Deploy Strategy/CI-CD Gate sections for managed-only | P3 | bootstrap_guide_assembly.go:67-108 | Small (~5 lines) |
| R3 | Add managed-only summary to transition: "Managed services provisioned: {list from import attestation or discover}" when targets are empty | P3 | bootstrap_guide_assembly.go:62-65 | Small (~10 lines) |

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| F1: No managed-only guidance note | VERIFIED | Read bootstrap.md, no empty-plan mention in discover section |
| F2: Empty transition message | VERIFIED | Read bootstrap_guide_assembly.go:62-129, traced empty iteration |
| F3: Empty attestation | VERIFIED | Read engine.go:215-231, traced empty join |
| F4: No outputs for managed-only | VERIFIED | Read bootstrap_outputs.go:22-41, confirmed by design comment |
| F5: State detection classifies as FRESH | VERIFIED | Read managed_types.go:43-55, filters managed services |
| Intentional design (rejected findings) | VERIFIED | Code comments + 2 integration tests + 8 unit tests |

## Adversarial Challenges

### Challenged: F1 "Plan model gap" (originally CRITICAL → REJECTED)
The adversarial correctly identified that empty plan is an **intentional sentinel**, not a gap. Evidence: `validate.go:128` comment, `workflow_bootstrap.go:40` comment, `bootstrap_outputs.go:21` ("managed deps are API-authoritative"), `bootstrap_steps.go:13` comment ("managed-only fast path"). Two passing integration tests validate the complete managed-only flow end-to-end.

**Resolution**: The plan model is designed for runtime service orchestration. Managed services are API-authoritative and don't need plan-level tracking. R1 from primary (add managed services to plan model) is **rejected** as scope creep violating the API-authoritative design principle.

### Challenged: F2 "Provision checker no-op" (originally MAJOR → REJECTED)
The adversarial correctly noted that `zerops_import` has its own error handling (API errors, process tracking). The LLM verifies via `zerops_discover` before completing the step. The provision checker's nil return for empty targets is correct — there are no targets to check.

**Resolution**: No provision checker change needed.

### Challenged: F3 "No managed-only guidance" (originally MAJOR → kept MAJOR as F1)
Both analysts agree a one-line guidance note is warranted. The adversarial accepts R3 (guidance note) as trivial and helpful. Kept as MAJOR because it's the most actionable improvement — not because the system is broken, but because explicit guidance > LLM inference.

### Confirmed by adversarial
- F4 (generate/deploy skip semantics) — system structurally supports it
- F5 (empty transition) — cosmetically poor, trivial fix
- F6 (no outputs) — correct by design
- F7 (state detection) — technically correct, LLM self-corrects
