# Context: analysis-deploy-workflow-naming
**Last updated**: 2026-03-27
**Iterations**: 1
**Task type**: codebase-analysis

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Keep "deploy" as workflow name | KB: Zerops docs use "deploy" as user-facing verb; no alternatives exist in platform lexicon | 1 | Platform alignment is the only defensible criterion for workflow naming |
| D2 | Rename DeployStepDeploy → DeployStepExecute | `deploy.md` sections already use `deploy-execute-*`; code constant misaligned with content | 1 | Eliminates 3-level naming recursion; aligns code with existing content structure |
| D3 | Upgrade recursion severity from MEDIUM to CRITICAL | Agent output `"Deploy step 2/3: deploy"` + guidance `"## Deploy"` + tool `zerops_deploy` = 3 nested "deploy" | 1 | Adversarial challenge proved real agent confusion path |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|-------------|-----------------|-----------|--------------|
| X1 | Rename workflow to "pipeline" | Zerops "pipeline" includes build phase; ZCP workflow doesn't | 1 | Semantic mismatch with platform term |
| X2 | Rename workflow to "ship"/"release"/"iterate" | Zerops docs never use these terms | 1 | No platform alignment |
| X3 | Rename workflow to "push-dev" | Only applies to one strategy; confusing | 1 | Too narrow |
| X4 | Keep everything as-is | 3-level recursion confirmed; content already diverged to "execute" | 1 | Misalignment is structural, not cosmetic |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| RC1 | Cost of rename | Grep shows 13 code locations, 0 content changes | 1 | 1 | Adversarial corrected primary's overestimate |

## Open Questions (Unverified)
- None. All findings verified against code and live platform.

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|----------------|
| Workflow name | VERIFIED | KB docs survey + platform API verification |
| Step rename | VERIFIED | Code grep (13 hits) + content grep (5 sections) |
| Cost estimate | VERIFIED | Exhaustive grep of `DeployStepDeploy` |
| Platform terminology | VERIFIED | Live Zerops API + zerops-docs |
