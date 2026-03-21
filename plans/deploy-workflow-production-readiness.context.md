# Review Context: deploy-workflow-production-readiness
**Last updated**: 2026-03-21
**Reviews completed**: 2 + user feedback iteration
**Resolution method**: Evidence-based (no voting)

## Core Philosophy (from user feedback)
- **We don't know what the user wants from their application** — never assume app correctness
- **Help, don't gatekeep** — validate platform integration, provide diagnostics, deliver knowledge
- **Value is in contextual diagnostics** — when things break, point to the right place based on WHERE in the Zerops pipeline the failure occurred
- **Respect modes and strategy transitions** — user may switch push-dev → ci-cd, dev → standard
- **Maximize knowledge delivery** — Zerops-specific mechanics for the current runtime, mode, strategy

## Decision Log
| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | Create separate DeployStepChecker type | bootstrap_checks.go:24 typed to *BootstrapState | R1 | No premature abstraction (only 2 types) |
| 2 | Add context.Context to DeployComplete() AND DeployStart() | engine.go:358 and :341 have no ctx; checkers need it | R1+R2 | Matches BootstrapComplete pattern; DeployStart also needs ctx |
| 3 | Deploy checkers in internal/tools/ | Existing pattern: tools/workflow_checks*.go | R1 | Follow existing file layout |
| 4 | Keep resolveDeployStepGuidance for static content, use assembleGuidance for knowledge+iteration | resolveStaticGuidance (guidance.go:56) is bootstrap-only for prepare/verify steps; needsRuntimeKnowledge (guidance.go:67) already handles DeployStepPrepare | R1+R2 | Avoids step naming conflict while reusing iteration/knowledge |
| 5 | Keep deployTargetPending constant | deploy.go:152,154,161,164,273 — live | R1 | Only constant with live callers |
| 6 | Checkers validate PLATFORM, not APPLICATION | User philosophy: we don't know what user wants | R1+user | Help, don't gatekeep |
| 7 | Merge Phase 2+3 concepts: validation + diagnostics | Diagnostic feedback IS the primary value | R1+user | Contextual diagnostics > hard gates |
| 8 | 2 checkers (prepare + result), not 3 | Verify step is informational, not blocking | R1+user | Simpler, matches philosophy |
| 9 | Dev→stage is informational, not a hard gate | User may want to push broken code to dev | R1+user | Don't assume app correctness |
| 10 | Re-discover env vars via API at deploy-prepare | DiscoveredEnvVars only on BootstrapState; deploy is standalone | R2 | Cheap (one API call/dep), handles post-bootstrap changes |
| 11 | Fix ErrBootstrapNotActive → ErrDeployNotActive in deploy handlers | workflow_deploy.go:29,51,62 uses wrong error code | R2 | Semantic correctness |

## Rejected Alternatives
| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | Generalize StepChecker for both workflows | Only 2 types exist | R1 | Premature abstraction |
| 2 | Health check validation in deploy checker | We don't know if user wants/has health checks | R1+user | Imposes app assumptions |
| 3 | Hard dev→stage gate | User may intentionally deploy broken code to dev | R1+user | Gatekeeping, not helping |
| 4 | Subdomain access verification as blocker | Service may not have HTTP endpoint | R1+user | Application-dependent |
| 5 | 3 separate checkers (prepare/execute/verify) | Verify is informational; execute should provide diagnostics not block | R1+user | Over-engineering |
| 6 | Full assembleGuidance() unification (replace resolveDeployStepGuidance entirely) | resolveStaticGuidance (guidance.go:56) only handles bootstrap step names; DeployStepPrepare/Verify would route to wrong source | R2 | Step name incompatibility; keep deploy's own static guidance |
| 7 | Skip env var validation in deploy (bootstrap already validated) | Env vars can change after bootstrap; deploy should be standalone | R2 | Correctness gap if deps change |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | All plan claims accurate? | 15/15 verified | R1 | R1 | All CONFIRMED |
| 2 | Dead code list complete? | grep: 0 production callers, all 4 R2 agents confirmed | R1 | R1+R2 | Complete |
| 3 | Phase ordering correct? | Dependencies verified | R1 | R1 | Sound |
| 4 | Checkers too restrictive? | User philosophy | R1+user | R1+user | Platform-only validation + diagnostics |
| 5 | DiscoveredEnvVars unavailable in deploy? | Only on BootstrapState | R2 | R2 | Re-discover via API (decision 10) |
| 6 | Step naming mismatch blocks Phase 3? | StepDeploy=="deploy" works; prepare/verify need deploy-specific dispatch | R2 | R2 | Keep resolveDeployStepGuidance for static, assembleGuidance for knowledge+iteration (decision 4) |
| 7 | Strategy gate exists? | Verified at workflow.go:227-248 | R2 | R2 | Exists and works (adversarial claim refuted) |
| 8 | checkGenerate path resolution reusable? | filepath.Dir x2 pattern is generic (workflow_checks_generate.go:29) | R1 | R2 | Reusable, no bootstrap coupling in path logic |
| 9 | API detail for diagnostics? | buildLogs in deploy response (confirmed); zerops_verify+logs for post-deploy; events hint field available | R1 | R2 | Multiple data sources confirmed |

## Open Questions (Unverified)
- Strategy transition support — how to update ServiceMeta when user switches strategy mid-lifecycle? (Phase 4, non-blocking)
- Events API `hint` field utility — worth investigating for Phase 2 diagnostic builder (LLM-friendly status summaries)

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Section 2 (What's Done) | HIGH | All claims VERIFIED across 2 reviews |
| Section 3.1-3.2 (Platform validation) | HIGH | Scoped to platform, env var source resolved |
| Section 3.3 (Dead code) | HIGH | Grep-verified, confirmed by all 4 R2 agents |
| Section 3.4 (Iteration escalation) | HIGH | Step name dispatch resolved (decision 4) |
| Phase 1 (Cleanup + enrichment + error fix) | HIGH | Safe, verified, error code fix added |
| Phase 2 (Validation + diagnostics) | HIGH | Env var source decided, context threading scoped |
| Phase 3 (Iteration escalation) | HIGH | Approach specified: keep deploy static, use assembleGuidance for knowledge+iteration |
| Phase 4 (Polish) | HIGH | Low risk |
