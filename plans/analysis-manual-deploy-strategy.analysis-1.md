# Analysis: Manual Deploy Strategy — Iteration 1

**Date**: 2026-03-23
**Scope**: workflow_strategy.go, deploy_guidance.go, deploy.md, router.go, workflow_deploy.go, bootstrap_guide_assembly.go, bootstrap_outputs.go, spec-bootstrap-deploy.md, workflow_cicd.go, service_meta.go
**Agents**: kb (zerops-knowledge), verifier (platform-verifier), primary (Explore), adversarial (Explore)
**Complexity**: Deep (4 agents, ultrathink)
**Task**: What does ZCP currently say about the "manual" deploy strategy, and how is it expected to work?

---

## Summary

The "manual" deploy strategy is **defined** as "user manages deployments with own tools, ZCP won't guide deploys" but **implemented** identically to push-dev — the deploy workflow runs the same 3 steps with the same `zerops_deploy` commands and same mode-based guidance. The strategy label is stored in ServiceMeta and displayed in text, but has **zero effect on workflow behavior**. On the Zerops platform itself, there is no concept of "manual deployment" — all deploys go through `zcli push` or webhooks regardless.

The only place strategy matters architecturally is the **CI/CD hard gate** — `handleCICDStart()` filters exclusively for `ci-cd` strategy services. For push-dev and manual, the deploy workflow is identical.

---

## Findings by Severity

### Critical

| # | Finding | Evidence | Confidence |
|---|---------|----------|------------|
| F1 | **Strategy description contradicts behavior**: `buildStrategySelectionResponse()` says "ZCP won't manage or guide your deploys" for manual, but deploy workflow provides full guided 3-step process with `zerops_deploy` commands — identical to push-dev | `workflow_strategy.go:136-138` (description) vs `deploy_guidance.go:76-127` (guidance generation branches on mode, not strategy) | VERIFIED |
| F2 | **Two separate code paths create false impression**: `buildStrategyGuidance()` (called at strategy-set time, `workflow_strategy.go:66`) extracts the 3-line deploy-manual section as a one-shot response. But `buildDeployGuide()` (called during deploy workflow, `deploy_guidance.go:76`) generates full mode-based workflow ignoring strategy. User gets "you manage yourself" at selection, then full orchestrated guidance at deploy time. | `handleStrategy()` → `buildStrategyGuidance()` (one-shot) vs `handleDeployStart()` → `BuildResponse()` → `buildDeployGuide()` (deploy workflow) — two completely separate paths | VERIFIED |

### Major

| # | Finding | Evidence | Confidence |
|---|---------|----------|------------|
| F3 | **Router produces identical output for manual and push-dev**: `strategyOfferings()` returns `{Workflow: "deploy", Priority: 1}` for both — no differentiation in workflow routing | `router.go:195-202` — both cases return same FlowOffering | VERIFIED |
| F4 | **deploy-manual section is a 3-line stub** that still references `zerops_deploy` — not "own tools": `"Direct deploy: zerops_deploy targetService=\"{hostname}\""` | `deploy.md:194-200` | VERIFIED |
| F5 | **deploy.md claims "without dev+stage pattern" but this isn't enforced**: nothing prevents manual + standard mode (which creates dev+stage pairs) | `workflow_deploy.go:53-62` checks strategy exists but not mode compatibility; `deploy.go:122-162` uses mode from ServiceMeta regardless of strategy | VERIFIED |
| F6 | **Spec claims "ZCP won't guide deploys" for manual** in the strategy table but spec section 4.4-4.6 describes deploy workflow guidance that's identical for all strategies | `spec-bootstrap-deploy.md:578` (description) vs `spec-bootstrap-deploy.md:619-649` (deploy steps — no strategy branching documented) | VERIFIED |
| F7 | **No platform concept of "manual deployment"**: Zerops has exactly 3 deploy triggers (zcli push, zcli service deploy, GitHub/GitLab webhook) — all go through the same build/deploy pipeline. "Manual" maps to no distinct platform mechanism. | Verified via zerops-docs `trigger-pipeline.mdx` + KB search | VERIFIED |

### Minor

| # | Finding | Evidence | Confidence |
|---|---------|----------|------------|
| F8 | **`writeStrategyNote()` is the only strategy-aware element in deploy guidance** — displays current strategy as text label + alternatives, no behavioral effect | `deploy_guidance.go:188-201` | VERIFIED |
| F9 | **Test coverage for manual strategy is thin**: router_test.go has one "manual strategy" case (line 78), deploy_guidance_test.go has "simple_manual" test (line 57). No test verifies that manual guidance differs from push-dev, because it doesn't. | `router_test.go:78-87`, `deploy_guidance_test.go:57-69` | VERIFIED |

---

## Adversarial Challenges — Resolution

| Challenge | Verdict | Evidence |
|-----------|---------|----------|
| CH1: Router identity is correct by design | **ACCEPTED** — router operates before strategy detail matters | Valid architectural observation |
| CH2: Manual is a "label for user intent" | **PARTIALLY ACCEPTED** — it IS a label, but the label promises behavior that doesn't follow | `workflow_strategy.go:138` says "ZCP won't guide" but `buildDeployGuide()` fully guides |
| CH3: "Strategy DOES matter via section extraction" | **REJECTED** — `buildStrategyGuidance()` runs only at strategy-set time, not during deploy workflow. `buildDeployGuide()` never reads strategy sections | `workflow_strategy.go:66` (one-shot) vs `deploy_guidance.go:76-127` (deploy workflow) — two separate paths |
| CH4: "No contradiction exists" | **REJECTED** — adversarial conflated strategy-set path with deploy workflow path. Contradiction is real. | KB fact-check confirmed: `buildDeployGuide()` branches on mode only (line 89-98) |
| MF1: Soft strategy gate is intentional | **ACCEPTED** — valid UX pattern | `workflow_deploy.go:53-62` |
| MF2: CI/CD has meaningful hard gate | **ACCEPTED** — ci-cd IS architecturally different (exclusive workflow filter) | `workflow_cicd.go:24-28` filters `DeployStrategy == StrategyCICD` |
| MF3: Strategy persistence creates workflow branching | **REJECTED** — persistence stores the string, `buildDeployGuide()` never reads it for branching | `deploy_guidance.go:89-98` switches on `state.Mode` only |
| "R1-R4 unnecessary/already done" | **REJECTED** — R2 is NOT done (`buildDeployGuide` doesn't branch on strategy); R3 is partially done (mixed strategies checked, but not mode+strategy compatibility) | Code evidence above |

---

## Recommendations (max 7, evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | **Redefine "manual" as "user-triggered deploy via ZCP"** — not "opt-out of guidance". Clarify: manual = you trigger `zerops_deploy` yourself, no CI/CD automation, but ZCP still provides the deploy workflow. Update `buildStrategySelectionResponse()` text. | P0 | `workflow_strategy.go:134-138` contradicts actual behavior | Small (text change) |
| R2 | **Update spec strategy table** to match new definition — "You trigger deploys yourself (no CI/CD automation)" instead of "ZCP won't guide deploys" | P0 | `spec-bootstrap-deploy.md:578` | Small (text change) |
| R3 | **Expand deploy-manual section in deploy.md** from 3 lines to meaningful guidance — explain: direct `zerops_deploy` calls, no webhook, user decides when to deploy, ZCP provides prepare/verify workflow | P1 | `deploy.md:194-200` is a stub | Small |
| R4 | **Consider whether manual needs to exist at all** — if manual = push-dev minus nothing, the distinction is: (a) push-dev = dev+stage SSH workflow, (b) manual = direct deploy without dev+stage assumption. If this is the intent, enforce it by warning on manual+standard mode. | P2 | `router.go:195-202` identical output; `deploy_guidance.go:89-98` no branching | Medium |
| R5 | **Add mode+strategy compatibility guidance** — manual + standard mode is architecturally confused (standard assumes orchestrated dev→stage push). Either warn or adapt guidance. | P2 | `workflow_deploy.go` has mixed-strategy check but no mode+strategy check | Small |
| R6 | **Update `closeGuidance` and `BuildTransitionMessage()`** to describe manual accurately, matching R1 redefinition | P1 | `guidance.go:46-51`, `bootstrap_guide_assembly.go:106-111` | Small |

---

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| F1 (description vs behavior) | VERIFIED | Code: `workflow_strategy.go:134-138` + `deploy_guidance.go:76-127` |
| F2 (two code paths) | VERIFIED | Code: `handleStrategy()` path vs `BuildResponse()` path |
| F3 (router identical) | VERIFIED | Code: `router.go:195-202` |
| F4 (stub section) | VERIFIED | Content: `deploy.md:194-200` |
| F5 (mode+strategy not enforced) | VERIFIED | Code: `workflow_deploy.go:53-62` |
| F6 (spec inconsistency) | VERIFIED | Spec: `spec-bootstrap-deploy.md:578` vs `584-649` |
| F7 (no platform concept) | VERIFIED | Docs: `zerops-docs/trigger-pipeline.mdx` + KB exhaustive search |
| F8 (writeStrategyNote cosmetic) | VERIFIED | Code: `deploy_guidance.go:188-201` |
| F9 (thin test coverage) | VERIFIED | Tests: `router_test.go:78`, `deploy_guidance_test.go:57` |

---

## Self-Challenge Results (Orchestrator)

All 9 findings verified against code with file:line citations. Adversarial raised one valid structural point (CI/CD hard gate makes strategy meaningful for ci-cd) and one valid UX point (soft gate is intentional). Both accepted and incorporated. Adversarial's core claim ("no contradiction exists") rejected with evidence from two independent agents (primary + KB).

---

## Key Insight

The "manual" strategy exists as a **naming fiction** — it's a third label in a system where only two behaviors exist:
1. **ci-cd**: exclusive workflow gate, routes to cicd workflow (`handleCICDStart` filters)
2. **everything else** (push-dev AND manual): identical deploy workflow, identical guidance, identical commands

The fix is not to add code complexity (strategy branching in guidance) but to **update the descriptions to match reality**: manual means "you trigger deploys on your own schedule via ZCP" — not "you opt out of ZCP guidance."
