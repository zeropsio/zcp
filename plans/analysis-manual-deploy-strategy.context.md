# Context: analysis-manual-deploy-strategy

**Last updated**: 2026-03-23
**Iterations**: 1
**Task type**: codebase-analysis

## Decision Log

| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | F1-F2 are CRITICAL: strategy description contradicts behavior | `workflow_strategy.go:134-138` vs `deploy_guidance.go:76-127` — two separate code paths, deploy path ignores strategy | 1 | All 4 agents confirmed the contradiction; adversarial's counter (CH4) rejected with evidence |
| D2 | Adversarial's CH3/CH4 rejected — `buildStrategyGuidance()` is one-shot, not deploy workflow | `workflow_strategy.go:66` (strategy-set) vs `deploy_guidance.go:76` (deploy workflow) | 1 | KB fact-check traced both code paths independently |
| D3 | CI/CD hard gate IS architecturally meaningful (adversarial MF2 accepted) | `workflow_cicd.go:24-28` filters StrategyCICD exclusively | 1 | Correct — ci-cd is the only strategy with unique workflow behavior |
| D4 | R1 (redefine manual as "user-triggered") is the right fix, not R2 (add strategy branching) | Platform has no "manual" concept; all deploys use zcli/zerops_deploy | 1 | Adding code branching would be over-engineering — the label is the problem, not the workflow |

## Rejected Alternatives

| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|------------|-----------------|-----------|--------------|
| A1 | "Manual is intentionally a soft label, no changes needed" (adversarial position) | `workflow_strategy.go:138` explicitly says "ZCP won't manage or guide your deploys" — this is a false promise, not a soft label | 1 | User receives full guided deploy workflow after being told ZCP won't guide — trust issue |
| A2 | "Add strategy branching to buildDeployGuide()" (primary R2) | Would add complexity for a distinction that doesn't exist on the platform | 1 | Over-engineering — the fix is description text, not code branching |
| A3 | "Remove manual strategy entirely" | Manual has a valid use case: "I trigger deploys on my own schedule" vs ci-cd automation | 1 | Useful intent signal even if workflow is identical to push-dev |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| C1 | "Does strategy affect deploy workflow at all?" | `deploy_guidance.go:89-98` branches on mode only; `deploy.go:312-322` passes strategy as data but no branching | 1 | 1 | No — strategy is cosmetic in deploy workflow. Only CI/CD has a hard gate. |
| C2 | "Is buildStrategyGuidance called during deploy workflow?" | Grep confirmed: called ONLY in `handleStrategy()` at `workflow_strategy.go:66` | 1 | 1 | No — one-shot at strategy-set time, not during deploy steps |

## Open Questions (Unverified)

- Does the Zerops dashboard GUI have a deploy button? (KB says no based on docs, but not verified via browser)
- Should manual + standard mode be warned/blocked? (R5 — needs product decision)

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|----------------|
| Strategy description text | VERIFIED | Code: workflow_strategy.go:134-138 |
| Deploy guidance generation | VERIFIED | Code: deploy_guidance.go:76-127, traced both paths |
| Router behavior | VERIFIED | Code: router.go:195-202, test: router_test.go:78 |
| Platform deploy mechanisms | VERIFIED | zerops-docs + KB exhaustive search |
| CI/CD hard gate | VERIFIED | Code: workflow_cicd.go:24-28 |
| deploy.md content | VERIFIED | Content: deploy.md:194-200 |
| Spec consistency | VERIFIED | Spec: spec-bootstrap-deploy.md:578 vs 584-649 |
