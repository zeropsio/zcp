# Review Context: analysis-deploy-target-tracking
**Last updated**: 2026-03-21
**Reviews completed**: 1
**Resolution method**: Evidence-based

## Decision Log
| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | Keep per-target tracking, wire into production | User directive | R1 | User explicitly wants to implement it |
| 2 | Wire via checker internals, NOT new MCP action | workflow_checks.go:148-211, Architect + KB Scout | R1 | checkDeploy() already queries per-target API status; adding action="target-update" grows API surface unnecessarily |
| 3 | Deploy workflow only, NOT bootstrap | bootstrap.go:35-42, Architect C1 + KB Scout | R1 | Bootstrap uses subagents per service pair; parent doesn't need per-target state |
| 4 | Keep DeployTargetOut at 3 fields | deploy.go:295-302, Architect C2 | R1 | Deliberate encapsulation; error context via Message field |
| 5 | DevFailed() integrates into checkDeploy() | deploy.go:275-282, Zerops Expert C4 | R1 | ZCP policy enforcement (dev gates stage) belongs in checker, not guidance text |
| 6 | Closure approach for DeployState access | checkProvision precedent (StoreDiscoveredEnvVars) | R1 | No StepChecker signature change needed |

## Rejected Alternatives
| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | Add action="target-update" to MCP tool | Architect: first per-entity action, granularity shift; DX: naming confusion | R1 | Grows API surface, adds naming inconsistency, checker-internal approach achieves same result |
| 2 | Expose Error in DeployTargetOut | Architect C2: deliberate 3-field design; Security C2: attestation may contain secrets | R1 | DeployResponse.Message already carries error context; exposing Error breaks encapsulation |
| 3 | Add per-target tracking to bootstrap | KB Scout: subagents handle per-service independently; bootstrap.go has no Targets field | R1 | Architectural mismatch; bootstrap subagents self-report |
| 4 | Delete per-target tracking entirely | User directive to keep | R1 | User explicitly wants to wire it in |
| 5 | Extend action="complete" with hostname param | DX R2; considered as alternative to action="target-update" | R1 | Checker-internal approach is simpler; no MCP changes needed at all |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | Status enum validation missing | deploy.go:239-251 | Security C1 | R1 | Implementation plan Step 0a: add validTargetStatuses check |
| 2 | Attestation length unbounded | deploy.go:243 | Security C4 | R1 | Implementation plan Step 0b: add maxAttestationLen |
| 3 | Hostname validation in handler | deploy.go:240-248 | Security C3 | R1 | Moot: checker-internal approach uses only known targets from API query |
| 4 | Zero production callers | grep all internal/ | QA C1/C2 | R1 | Implementation plan Step 1-2: wire via checker |
| 5 | No tool handler for target-update | workflow.go action switch | QA C4 | R1 | Moot: checker-internal approach, no new handler needed |

## Open Questions (Unverified)
| # | Question | Status |
|---|----------|--------|
| 1 | Could attestation content contain secrets (SSH errors, connection strings)? | Plausible but no evidence of actual leakage. Mitigated by not exposing in DeployTargetOut. |
| 2 | Per-target iteration (reset single target vs all) — needed? | Deferred. Current ResetForIteration resets all targets. Future enhancement if needed. |

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Checker-internal approach | HIGH | checkProvision precedent + workflow_checks.go:148-211 |
| Status validation fix | HIGH | deploy.go:29-33 constants exist, just not enforced |
| DevFailed() in checkDeploy | HIGH | deploy.go:275-282 tested, checkDeploy already per-target |
| DeployTargetOut stays 3 fields | HIGH | deploy.go:295-302 deliberate conversion |
| Bootstrap exclusion | HIGH | bootstrap.go:35-42 no Targets field, subagent model |
| Closure approach for DeployState | MEDIUM | checkProvision captures engine (workflow_checks.go:39-40), same pattern |
| Per-target iteration deferred | MEDIUM | No current need, ResetForIteration works for step-level |
