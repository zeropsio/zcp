# Review Context: deploy-workflow-guidance-redesign
**Last updated**: 2026-03-21
**Reviews completed**: 2
**Resolution method**: Evidence-based (R1: 6 agents, R2: 6 agents — 12 total)

## Decision Log
| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | zerops_deploy is ASYNC — guidance must use polling model | ops/deploy.go returns BUILD_TRIGGERED immediately | R1 | KB-verifier refuted "blocks until complete" claim via live code inspection |
| 2 | RuntimeType fetched from API, not ServiceMeta | service_meta.go:22-29 has no RuntimeType field | R1 | All 4 analysts confirmed. Matches "deploy is standalone" philosophy. |
| 3 | DeployStepChecker separate type from StepChecker | bootstrap_checks.go:24 StepChecker takes (*ServicePlan, *BootstrapState) | R1 | Deploy has neither type. 2 checker types = no premature abstraction. |
| 4 | ctx + checker params are NEW WORK for engine methods | engine.go:341,358 lack ctx; engine.go:137 has ctx for bootstrap | R1 | Plan correctly schedules in Phase 3 (not "done"). |
| 5 | Phase 2 must REMOVE assembleKnowledge from deploy buildGuide | deploy.go:344-350 currently calls assembleKnowledge | R1 | Current code violates "no knowledge injection in deploy" principle. Active removal needed. |
| 6 | Phase 6 Route refactor is significant, not "verify and adjust" | router.go:81-104 boostByIntent, intentPatterns, editorial Reasons | R1 | Route is a full recommendation engine. Two options: refactor or document exception. |
| 7 | BuildTransitionMessage at bootstrap_guide_assembly.go:58 | grep confirmed | R1 | Plan v1 referenced wrong file (bootstrap_outputs.go). Corrected in v2. |
| 8 | GuidanceParams has 11 fields, deploy uses 4 | guidance.go:10-22 | R1 | Plan v1 said "10 fields" — corrected in v2. |
| 9 | Events API hint field confirmed at ops/events.go:40 | KB-verifier live test | R1 | ZCP-generated, not native Zerops API. processHintMap + appVersionHintMap. |
| 10 | checkDeployResult must use Events API process status | Process statuses (FINISHED/FAILED) differ from service statuses (ACTIVE/RUNNING) | R1 | BUILD_FAILED is process status, not service status. |
| 11 | DeployTarget.Status kept in Phase 1, removed in Phase 3 | BuildResponse:309 reads t.Status; removing field breaks compilation | R2 | Phase 1 removes only dead methods/fields/constants. Status field survives until Phase 3 checker populates it. |
| 12 | BuildIterationDelta safe with nil plan | bootstrap_guidance.go:94: `_ *ServicePlan` — parameter ignored | R2 | Tests at :467 already call with nil plan. No safety concern. |
| 13 | stateDir same for bootstrap and deploy | engine.go:27: stateDir set once at Engine construction | R2 | filepath.Dir(filepath.Dir(stateDir)) pattern works identically for both workflows. |
| 14 | StepDeploy == DeployStepDeploy == "deploy" is intentional | Plan §2.4 documents shared constant | R2 | Same string value by design. Add code comment during implementation. |
| 15 | Existing workflow_checks_deploy_test.go tests bootstrap, not deploy workflow | Tests call checkDeploy from workflow_checks.go:145 | R2 | New deploy workflow tests go in separate file. |

## Rejected Alternatives
| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | Store RuntimeType in ServiceMeta | Would require bootstrap schema change + migration | R1 | API re-read simpler, keeps deploy standalone |
| 2 | Reuse StepChecker for deploy | bootstrap_checks.go:24 signature incompatible | R1 | Different params needed — no abstraction with only 2 types |
| 3 | Keep assembleKnowledge in deploy buildGuide | Violates Decision #12 "no knowledge injection in deploy" | R1 | Must actively remove, not just add new behavior |
| 4 | Remove DeployTarget.Status in Phase 1 | BuildResponse:309 reads t.Status — breaks compilation | R2 | Keep until Phase 3 when checker results replace it |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | zerops_deploy blocking assumption | ops/deploy.go returns BUILD_TRIGGERED | R1 (kb-verifier) | R1 v2 | Guidance templates rewritten to async model |
| 2 | RuntimeType data source unknown | ServiceMeta has no field | R1 (all analysts) | R1 v2 | Decision #2: fetch from API at deploy time |
| 3 | BuildTransitionMessage wrong file | grep confirmed at bootstrap_guide_assembly.go:58 | R1 (kb-research) | R1 v2 | Phase 5 reference corrected |
| 4 | GuidanceParams field count | guidance.go:10-22 has 11 fields | R1 (kb-research) | R1 v2 | Corrected to 11 |
| 5 | Events API hint field existence | ops/events.go:40 TimelineEvent.Hint | R1 (kb-verifier) | R1 v2 | Resolved |
| 6 | ctx missing = BLOCKING? | Plan Phase 3 explicitly schedules ctx addition | R2 (adversarial) | R2 v3 | Not blocking — planned new work |
| 7 | BuildIterationDelta nil plan safety | Parameter is `_ *ServicePlan` (ignored) + existing tests | R2 (adversarial) | R2 v3 | Safe — param unused |
| 8 | stateDir path for deploy | Engine.stateDir set once, shared across workflows | R2 (adversarial) | R2 v3 | Same path, pattern reusable |
| 9 | Checker wiring underspecified | Bootstrap pattern in workflow_checks.go available | R2 (adversarial) | R2 v3 | Reference to existing pattern added |

## Open Questions (Unverified)
| # | Question | Context | Priority |
|---|----------|---------|----------|
| 1 | Service status "STOPPED" — does it exist? | Not observed in live API or E2E tests | LOW |
| 2 | GetServiceEnv API call latency | Assumed cheap, not benchmarked | LOW |
| 3 | Dev mode zsc noop behavior | Referenced in deploy.md, not verified on live | LOW |
| 4 | Phase 6 decision: refactor Route or document exception? | Route is recommendation engine. User philosophy says "dumb data". | MEDIUM — decide before Phase 6 |
| 5 | Where to store RuntimeTypes for buildGuide access? | Add to DeployState or pass as param? | LOW — resolve in Phase 2 |

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Dead code list (Phase 1) | HIGH | VERIFIED — grep, 8+ agents confirmed across 2 reviews |
| Error code fix (Phase 1) | HIGH | VERIFIED — 3 occurrences at exact lines |
| DeployTarget.Status sequencing (Phase 1→3) | HIGH | VERIFIED — BuildResponse:309 dependency confirmed |
| Async deploy model (Phase 2) | HIGH | VERIFIED — ops/deploy.go + kb-verifier |
| assembleKnowledge removal (Phase 2) | HIGH | VERIFIED — deploy.go:344-350 + guidance.go:67 |
| Guidance templates (Phase 2) | HIGH | VERIFIED — all data sources confirmed |
| RuntimeType resolution (Phase 2) | HIGH | VERIFIED — ServiceMeta gap confirmed, API available |
| BuildIterationDelta reuse (Phase 2) | HIGH | VERIFIED — nil plan safe, step constant match |
| DeployStepChecker design (Phase 3) | HIGH | VERIFIED — StepChecker incompatibility confirmed |
| Engine ctx addition (Phase 3) | HIGH | VERIFIED — current signatures confirmed |
| Checker wiring pattern (Phase 3) | HIGH | VERIFIED — bootstrap pattern at workflow_checks.go |
| stateDir path (Phase 3) | HIGH | VERIFIED — engine.go:27 shared across workflows |
| Init instructions (Phase 4) | MEDIUM | LOGICAL — content defined, integration point unverified |
| Transition improvements (Phase 5) | HIGH | VERIFIED — BuildTransitionMessage location confirmed |
| Route refactor (Phase 6) | MEDIUM | VERIFIED gap exists, decision (refactor vs exception) open |
