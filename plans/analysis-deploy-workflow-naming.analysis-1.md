# Analysis: Deploy Workflow Naming and Conceptual Identity — Iteration 1
**Date**: 2026-03-27
**Scope**: All deploy workflow files (15+ source files), Zerops platform terminology, MCP interface
**Agents**: kb (zerops-knowledge), verifier (platform-verifier), primary (Explore), adversarial (Explore)
**Complexity**: Deep (4 agents, ultrathink)
**Task**: Should the "deploy workflow" be renamed? Deep analysis of what it does, how it's used, and whether the name is accurate.

## Summary

The deploy workflow name "deploy" is **correct and should be kept** — it aligns with Zerops platform terminology and user expectations. However, the **deploy step inside the deploy workflow** creates a naming recursion problem (`WorkflowDeploy = "deploy"` + `DeployStepDeploy = "deploy"`) that produces confusing agent-facing output like `"Deploy step 2/3: deploy"`. The fix is surgical: rename the step constant from `"deploy"` to `"execute"`, which **already aligns with the existing content structure** (`deploy.md` sections are already named `deploy-execute-*`).

## Findings by Severity

### Critical

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| C1 | **Naming recursion: workflow, step, and tool all called "deploy"** — Agent sees `"Deploy step 2/3: deploy"` with guidance titled `"## Deploy — mode, strategy"` and tool `zerops_deploy`. Three levels of "deploy" in nested scope. | `deploy.go:9,14,302`; `deploy_guidance.go:79` | Primary [AG1], Adversarial [CH1] |
| C2 | **Content structure ALREADY uses "execute" — code doesn't** — `deploy.md` sections use `deploy-execute-standard`, `deploy-execute-dev`, `deploy-execute-simple` but the constant is `DeployStepDeploy = "deploy"`. Structural misalignment between content naming and code naming. | `deploy.md:53,67,89,100`; `deploy.go:14` | Adversarial [MF1] |

### Major

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| M1 | **Platform API uses `stack.build` for code pushes, NOT `stack.deploy`** — `stack.deploy` is reserved for managed service provisioning. The "platform alignment" argument for keeping the step name "deploy" is undermined (though keeping the workflow name "deploy" is still valid since it's the user-facing term). | `events.go:52-63`; KB live verification | Adversarial [CH3], Verifier |
| M2 | **Workflow name "deploy" doesn't capture iteration** — The workflow is a prepare→deploy→verify→iterate feedback loop, not a single deploy action. Name sets wrong expectation. | `deploy.go:239-261`; `deploy_guidance.go:263-276` | Primary [AG3] |
| M3 | **Asymmetry with other workflows** — "bootstrap" names a process, "cicd" names a domain, "deploy" names a verb/tool. Mixed naming conventions. | `state.go:17,31-32`; `router.go:203-205` | Primary [AG5] |

### Minor

| # | Finding | Evidence | Source |
|---|---------|----------|--------|
| m1 | **"Deploy" confusable with manual strategy's direct deploy** — Manual strategy uses `zerops_deploy` directly without a workflow, but shares the "deploy" word. | `router.go:210-211`; `deploy.md:198-218` | Primary [AG4] |
| m2 | **Primary's cost estimate was inaccurate** — Claimed ~40 lines, ~12 files. Actual: 13 code occurrences across 5 Go files + tests. Content changes are ZERO (already aligned). | Grep for `DeployStepDeploy`: 13 hits | Adversarial [CH2] |

## Recommendations (evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|----------------|----------|----------|--------|
| R1 | **Keep "deploy" as workflow name** — Only platform-aligned option. Zerops docs use "deploy" as the user-facing verb. No alternatives ("ship", "release", "iterate") exist in Zerops vocabulary. | P0 (decision) | KB: Zerops docs survey; `router.go` flow offerings | 0 (no change) |
| R2 | **Rename `DeployStepDeploy` from `"deploy"` to `"execute"`** — Eliminates recursion. Aligns with existing `deploy.md` section naming (`deploy-execute-*`). Agent output changes from `"Deploy step 2/3: deploy"` to `"Deploy step 2/3: execute"`. | P1 | `deploy.go:14`; `deploy.md:53,67,89,100` | ~13 code locations across 5 files |
| R3 | **Update guidance title in `buildDeployGuide()`** — Change `"## Deploy — %s mode, %s"` to `"## Execute — %s mode, %s"` or `"## Build & Deploy — %s mode, %s"` to match new step name. | P1 | `deploy_guidance.go:79` | 1 line |
| R4 | **Update message format in `BuildResponse()`** — Currently `"Deploy step %d/%d: %s"`. Consider `"Workflow step %d/%d: %s"` to avoid "Deploy step" prefix on non-deploy steps too. | P2 | `deploy.go:302` | 1 line |

## Evidence Map

| Finding | Confidence | Basis |
|---------|------------|-------|
| C1 Naming recursion | VERIFIED | `deploy.go:9,14,302`; `deploy_guidance.go:79` — code inspection |
| C2 Content alignment | VERIFIED | `deploy.md:53,67,89,100` — grep confirmed section names |
| M1 API terminology | VERIFIED | Live Zerops API verification (verifier agent, 2026-03-27) |
| M2 Iteration hidden | VERIFIED | `deploy.go:239-261` — ResetForIteration() code |
| M3 Naming asymmetry | VERIFIED | `state.go:17,31-32` — constant definitions |
| R1 Keep workflow name | VERIFIED + LOGICAL | KB docs survey + no alternatives in Zerops lexicon |
| R2 Rename step | VERIFIED | 13 occurrences grepped; content already uses "execute" |

## Adversarial Challenges

### Challenged Findings (resolved)

| Challenge | Target | Resolution | Evidence |
|-----------|--------|------------|----------|
| CH1: Recursion is CRITICAL not MEDIUM | Primary AG1 | **UPGRADED to CRITICAL** — three levels of "deploy" in nested scope, visible in agent output | `deploy.go:302` outputs "Deploy step 2/3: deploy" |
| CH2: Cost estimate inaccurate | Primary RC1 | **CORRECTED** — content changes are ZERO (already aligned), code changes are 13 locations not 40 | Grep: 13 hits for DeployStepDeploy |
| CH3: Platform alignment undermined | Primary FA1 | **PARTIALLY UPHELD** — API uses `stack.build`, but user-facing term is still "deploy". Workflow name alignment holds; step name alignment does NOT. | `events.go` actionNameMap + KB |
| MF1: deploy.md already uses "execute" | Primary missed | **CONFIRMED CRITICAL** — sections `deploy-execute-*` already exist, proving "execute" is the intended step-level term | `deploy.md:53,67,89,100` |

### Confirmed (survived challenge)

| Finding | Why it holds |
|---------|-------------|
| R1: Keep workflow name "deploy" | No platform-native alternative exists; user expectation matches |
| R3: Keep workflow name | Adversarial confirmed this independently |
| AG2-AG5: Step name arguments | All confirmed by adversarial with additional evidence |

## Rename Implementation Scope

If R2 is approved, here are the exact changes:

### Constants (`internal/workflow/deploy.go`)
```
Line 14: DeployStepDeploy = "deploy" → DeployStepExecute = "execute"
```

### Code references (5 files, 12 locations)
| File | Lines | Change |
|------|-------|--------|
| `internal/workflow/deploy.go` | 100, 246, 316 | `DeployStepDeploy` → `DeployStepExecute` |
| `internal/workflow/deploy_test.go` | 62, 67, 88, 105 | `DeployStepDeploy` → `DeployStepExecute` |
| `internal/workflow/engine_test.go` | 1288 | `DeployStepDeploy` → `DeployStepExecute` |
| `internal/tools/workflow_checks_deploy.go` | 22 | `workflow.DeployStepDeploy` → `workflow.DeployStepExecute` |
| `internal/tools/workflow_checks_deploy_workflow_test.go` | 169 | `workflow.DeployStepDeploy` → `workflow.DeployStepExecute` |

### Guidance (`internal/workflow/deploy_guidance.go`)
```
Line 79: "## Deploy — %s mode, %s" → "## Execute — %s mode, %s"
```

### Message format (`internal/workflow/deploy.go`)
```
Line 302: "Deploy step %d/%d: %s" — no change needed (now outputs "Deploy step 2/3: execute" which is clear)
```

### Content (`internal/content/workflows/deploy.md`)
**ZERO changes needed** — sections already use `deploy-execute-*` prefix.

### Plans (non-code)
```
plans/plan-local-dev-flow.md:788 — stale plan reference, update or ignore
```

**Total: 13 code changes across 5 Go files + 1 guidance line. Zero content changes.**
