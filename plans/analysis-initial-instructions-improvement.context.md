# Context: analysis-initial-instructions-improvement
**Last updated**: 2026-03-30
**Iterations**: 1
**Task type**: codebase-analysis

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Root cause is instructions.go:208-211 short-circuit, not missing tool fields | Adversarial MF1 verified by orchestrator read | 1 | Orientation and routing mutually exclusive when they should be complementary |
| D2 | Knowledge injection in direct tools rejected — fix upstream via routing | Adversarial CH2 — violates MCP "dumb" principle | 1 | If LLM is properly routed to workflows, knowledge flows naturally |
| D3 | managedByZCP field needed but insufficient alone — must pair with routing text | Adversarial CH1 — field without instructions is useless | 1 | R2 + R3 together solve the problem |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|------------|-----------------|-----------|--------------|
| A1 | Inject knowledge into deploy/manage/mount tool responses | Violates MCP "dumb" principle (CLAUDE.md) | 1 | Downstream fix; routing clarity solves upstream |
| A2 | Add isBootstrapped field only (without routing text changes) | Adversarial CH1 — LLM won't check field without instructions | 1 | Incomplete; needs paired instruction text |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| - | (none yet) | - | - | - | - |

## Open Questions (Unverified)
- F10: ActiveAppVersion API field could indicate never-deployed services. Mapping it in platform.ServiceStack would enable richer discovery. Needs live API verification.
- Provision checker (F8): exact behavior for READY_TO_DEPLOY with IsExisting=true needs E2E test.

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|----------------|
| Short-circuit root cause (F1) | VERIFIED | instructions.go:208-211 read directly |
| Discovery gap (F2) | VERIFIED | ops/discover.go:28-40 struct verified |
| Adoption guidance gap (F3) | VERIFIED | bootstrap.md + router.go verified |
| Project state detection (F4) | VERIFIED | managed_types.go:36-62 verified |
| Knowledge injection scope (F5) | VERIFIED | guidance.go + tools/ verified |
| IsExisting manual (F6) | VERIFIED | validate.go:36-41 verified |
| Routing text gap (F7) | VERIFIED | instructions.go:47-65 verified |
| Provision checker (F8) | VERIFIED | workflow_checks.go:55-68 verified |
| ActiveAppVersion (F10) | LOGICAL | KB agent report, not in codebase |
