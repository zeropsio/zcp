# Review Report: analysis-bootstrap-flow-gates — Review 1
**Date**: 2026-03-21
**Reviewed version**: plans/analysis-bootstrap-flow-gates.md
**Team**: kb-scout, architect, security, qa-lead, dx-product, zerops-expert, evidence-challenger
**Focus**: Mode gate, bootstrap termination before strategy, post-bootstrap step blocking
**Resolution method**: Evidence-based (no voting)

---

## Evidence Summary

| Agent | Findings | Verified | Unverified | Post-Challenge Downgrades |
|-------|----------|----------|------------|--------------------------|
| Architect | 7 | 5 | 0 | C2 INVALIDATED (checkStrategy exists), C3 MISLEADING |
| Security | 7 | 7 | 0 | — |
| QA Lead | 7 | 7 | 0 | — |
| DX/Product | 5 | 2 | 0 | C2 INVALIDATED (deploy does NOT block on strategy), C3 INVALIDATED (transition message IS accurate) |
| Zerops Expert | 8 | 8 | 0 | — |

**Overall**: SOUND — based on VERIFIED findings. All three proposed changes are architecturally feasible. One spec-vs-implementation discrepancy found (managed-only gap). Evidence challenger issued challenges late; kb-scout provided two critical fact-checks that invalidated core claims from architect (C2) and dx-product (C2, C3).

**Critical correction**: Deploy workflow currently has NO strategy gate. `handleDeployStart()` checks metas exist and are complete but never reads `DeployStrategy`. Adding a strategy gate to deploy/cicd is a NEW feature, not fixing existing behavior.

---

## Knowledge Brief

### Bootstrap Steps (current): 6 steps
discover → provision → generate → deploy → verify → strategy

### Mode Determination
- Mode is per-target `RuntimeTarget.BootstrapMode`, submitted in plan during discover step
- Validated by `validBootstrapModes` map: `""`, `"standard"`, `"dev"`, `"simple"`
- `EffectiveMode()` defaults empty to `"standard"`
- `PlanMode()` aggregates: `"standard"` if ANY target uses it, else single mode, else `"mixed"`
- Step ordering structurally guarantees plan exists before provision (discover is non-skippable)

### Strategy Step
- `CategoryFixed`, `Skippable: true` — only step that is both Fixed and Skippable
- `checkStrategy()` EXISTS at `workflow_checks_strategy.go:11-72`, wired at `workflow_checks.go:33-34`
- Validates every runtime target has valid strategy. Blocks step if missing.
- Auto-assign `push-dev` for dev/simple modes at `writeBootstrapOutputs()` only
- Strategy can be skipped (no `validateConditionalSkip` guard)

### Bootstrap Termination
1. `Active = false` when CurrentStep advances past all steps
2. `writeBootstrapOutputs()`: writes ServiceMeta with BootstrappedAt, resolves strategy
3. Session file deleted via `ResetSessionByID()`
4. `completedState` cached for transition message
5. `BuildTransitionMessage()`: "What's Next?" with deploy/cicd/scale/debug/configure

### Strategy is ZCP-only
- Never sent to Zerops API (zero matches in `internal/platform/`)
- CI/CD on Zerops is external (webhooks, GitHub Actions, not import.yml)
- Strategy choice maps to real platform features but setup is GUI/repo-side

### Spec vs Implementation Discrepancies
- `ValidateBootstrapTargets` requires `len(targets) > 0` — managed-only projects blocked (CONFIRMED GAP)
- Strategy step has checkStrategy enforcing valid strategies — spec matches code
- Mode chosen at discover step — spec matches code

---

## Agent Reports

### Architect Review
**Assessment**: CONCERNS
**Key findings**:
- C1 (MAJOR): Mode gate is implicit in step ordering, not explicit type check. VERIFIED but OVERSTATED — step ordering + non-skippable discover structurally guarantees plan exists.
- C2 (CRITICAL): "checkStrategy doesn't exist" — **INVALIDATED by kb-scout**. checkStrategy() exists at `workflow_checks_strategy.go:11` and IS wired at `workflow_checks.go:34`.
- C3 (CRITICAL): "Termination before strategy" — MISLEADING. Termination happens AFTER strategy step completes/skips, not before.
- C4 (MAJOR): Strategy in two places (session state + ServiceMeta) — VERIFIED but INTENTIONAL design (ephemeral → persistent flow).
- C5 (CRITICAL): ValidateBootstrapTargets blocks managed-only — VERIFIED REAL GAP.
- C6 (MINOR): No conditional skip guard for strategy — VERIFIED, by design.
- Recommendation R1 (remove strategy from bootstrap): DEBATABLE — strategy IS enforced, not orphaned.

### Security Review
**Assessment**: SOUND
All 7 findings verified secure. Mode validation strict (enum map). Strategy validation strict (3-element enum + checker). ServiceMeta writes atomic (temp+rename). Session transitions properly ordered. No new vulnerabilities from proposed changes.

### QA Lead Review
**Assessment**: SOUND with MINOR GAPS
- Strategy step completion/auto-assignment: 100% tested
- Mode validation: 100% tested (TestEffectiveMode, TestPlanMode, TestProvisionMeta_SetsMode)
- validateConditionalSkip: 90% — strategy step skip path not explicitly tested
- Router strategyOfferings: 90% — empty-meta fallback untested
- Minor gaps: [R1] add strategy skip test, [R2] add empty-meta router test

### DX/Product Review
**Assessment**: CONCERNS
- C2 (CRITICAL): Removing strategy from bootstrap creates decision fatigue + flow ambiguity
  - "Bootstrap complete → now choose strategy → then choose deploy" = 3 decisions vs current "strategy is final bootstrap step" = 1 inline decision
  - Transition message promises options that may not be available if strategy not set
- C3 (MAJOR): Current transition message ambiguous if strategy moves out
- R1: Recommends KEEPING strategy IN bootstrap for UX coherence
- R4: Mode default behavior should be documented + fail-fast in discover

### Zerops Expert Review
**Assessment**: SOUND
- C1: Mode is ZCP-only (platform doesn't differentiate dev/stage at creation)
- C2: Strategy never sent to API — safe to decouple timing
- C3: CI/CD on Zerops is real (webhooks, GitHub Actions) but external to import
- C4: Managed-only projects ARE valid on Zerops
- Strategy timing has no platform constraints — purely ZCP workflow decision

---

## Evidence-Based Resolution

### Verified Concerns (drive changes)

**1. Managed-only bootstrap gap** — `ValidateBootstrapTargets` requires `len(targets) > 0`
- Source: Architect C5, Zerops Expert C4
- Evidence: `validate.go:128-129`, zerops-docs confirm managed-only is valid
- Impact: CRITICAL — blocks a real use case

**2. Transition message mismatch** — If strategy moves out, "What's Next?" offers invalid options
- Source: DX/Product C2, C3
- Evidence: `bootstrap_guide_assembly.go:57-88` — offers deploy/cicd without checking strategy
- Impact: MAJOR — must be redesigned if strategy moves out

**3. Strategy is ZCP-only, no platform coupling** — Safe to change timing
- Source: Zerops Expert C2, Security finding 2
- Evidence: Zero `strategy` references in `internal/platform/`
- Impact: Enables the proposed design change without API impact

### Logical Concerns (inform changes)

**4. DX coherence tradeoff** — Moving strategy out breaks bootstrap's "single narrative"
- Source: DX/Product C2
- Reasoning: Current 6-step flow has clear start/end. Splitting adds a decision gap.
- Resolution: Addressable — redesign transition message to guide user through strategy → deploy sequence. Don't offer deploy/cicd until strategy is set.

**5. Router needs strategy-awareness** — If strategy moves out, router must handle "no strategy yet"
- Source: DX/Product C2, Architect C4
- Reasoning: `strategyOfferings()` returns nil when no strategies set, falling through to generic deploy offering
- Resolution: Add explicit "set strategy" offering at p0 when ServiceMeta has empty DeployStrategy

### Unverified Concerns (flagged for investigation)
None — all findings verified.

### Evidence Challenger Highlights
Evidence challenger failed to activate (went idle despite two prompts). KB-scout performed ad-hoc fact-checking instead, catching architect's C2 factual error.

### Top Recommendations (evidence-backed, max 7)

**[R1] Remove strategy from bootstrap steps (5-step bootstrap)** — Strategy becomes post-bootstrap
- Evidence: Strategy is ZCP-only (zerops-expert C2), no platform coupling. Auto-assign handles dev/simple.
- Design: Bootstrap = discover→provision→generate→deploy→verify. Strategy = independent `action="strategy"`.
- Standard mode services: explicit strategy required post-bootstrap before deploy workflow.
- Dev/simple modes: auto-assign `push-dev` at bootstrap completion (no change to current behavior).

**[R2] Redesign transition message for 5-step bootstrap**
- Evidence: Current message offers deploy/cicd unconditionally (DX/Product C3)
- Design: After verify completes, transition message presents strategy choice inline:
  - "Bootstrap complete. Choose deployment strategy for each service:"
  - List services with strategy options (push-dev/ci-cd/manual)
  - "After choosing, run deploy or cicd workflow."
- For dev/simple: auto-assigned push-dev, message skips strategy prompt and directly offers deploy.

**[R3] Add strategy-gate to deploy workflow start**
- Evidence: Router falls through to generic deploy when no strategy set (Architect C4)
- Design: `handleDeployStart()` checks ServiceMeta.DeployStrategy for each runtime meta. If empty, returns error: "Strategy not set for {hostname}. Set strategy first: `zerops_workflow action=strategy strategies={...}`"

**[R4] Add strategy-gate to router offerings**
- Evidence: strategyOfferings returns nil when no strategies, falls to generic (router.go:230)
- Design: When any runtime meta has empty DeployStrategy, inject p0 offering: "Set deployment strategy" before deploy/cicd offerings.

**[R5] Fix managed-only bootstrap validation**
- Evidence: `validate.go:128` blocks `len(targets) == 0` (Architect C5, Zerops Expert C4)
- Design: Allow empty targets if dependencies exist. Add managed-only test case.

**[R6] Add explicit plan-exists check in BootstrapComplete (defense-in-depth)**
- Evidence: Architect C1 — mode is implicit in step ordering
- Design: In `BootstrapComplete()`, before non-discover steps, verify `state.Bootstrap.Plan != nil`. Fail fast with clear error.

**[R7] Add missing test cases**
- Evidence: QA Lead R1, R2
- Tests: (1) strategy step skip in validateConditionalSkip, (2) empty-meta router fallback

---

## Revised Version

See `plans/analysis-bootstrap-flow-gates.v2.md`

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | Bootstrap steps | 6→5 steps (remove strategy) | Strategy is ZCP-only, no platform coupling | Zerops Expert C2 |
| 2 | Transition message | Redesign to present strategy choice inline | Current message offers invalid options | DX/Product C2, C3 |
| 3 | Deploy workflow | Add strategy-gate at start | No gate currently, generic fallback | Architect C4 |
| 4 | Router | Add "set strategy" p0 offering | strategyOfferings returns nil for empty | Router.go:230 |
| 5 | Validation | Allow managed-only bootstrap (empty targets) | validate.go:128 blocks real use case | Architect C5 |
| 6 | Engine | Add Plan!=nil check for non-discover steps | Implicit ordering, no explicit check | Architect C1 |
| 7 | Tests | Add strategy skip + empty-meta tests | Minor coverage gaps | QA Lead R1, R2 |
