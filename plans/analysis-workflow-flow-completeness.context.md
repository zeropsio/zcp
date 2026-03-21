# Review Context: analysis-workflow-flow-completeness
**Last updated**: 2026-03-21
**Reviews completed**: 1
**Resolution method**: Evidence-based

## Decision Log
| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | bootstrap.md verify section is orphaned dead content | bootstrap.md:778 has `<section name="verify">`, no code extracts it, close uses hardcoded constant | R1 | Section exists but no step named "verify" — content never delivered to agents |
| 2 | monitor.md is dead content | state.go:32-34 missing "monitor" in immediateWorkflows | R1 | File exists, no code path accesses it |
| 3 | BootstrapStoreStrategies is vestigial | grep: only called in tests, not by any tool handler | R1 | handleStrategy bypasses engine, writes ServiceMeta directly |
| 4 | nil liveServices in BootstrapCompletePlan is by design but creates delayed validation | workflow_bootstrap.go:42, validate.go:125 docs | R1 | Documented as "skipped when unavailable"; liveTypes IS provided |
| 5 | Deploy iteration behavior is undocumented | deploy.go:253-272 resets indices 1+; spec §7.3 only covers bootstrap | R1 | Code is correct but spec doesn't document it |

## Rejected Alternatives
| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | "Strict linearity broken by skip gates" | bootstrap.go CompleteStep enforces forward-only order; skip = forward progression | R1 | Skipping is compatible with linear progression — adversarial claim refuted |
| 2 | "Managed-only iteration is broken" | validateConditionalSkip (bootstrap.go:315-327) prevents re-running skipped steps | R1 | Guards exist; adversarial claim refuted |
| 3 | "BootstrapCompletePlan nil for BOTH liveTypes and liveServices" | workflow_bootstrap.go:42 passes liveTypes from cache, only liveServices is nil | R1 | Adversarial overstated — type validation does happen |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | Step progression bypass | bootstrap.go:146-167 CompleteStep validates name match | R1 | R1 | Strictly linear, enforced by code |
| 2 | Iteration index correctness | bootstrap.go:107 resets 2-3; deploy.go:259 resets 1+ | R1 | R1 | Both correct |
| 3 | Session cleanup ordering | engine.go:179-184 outputs before delete | R1 | R1 | Correct: write metas, then delete session |
| 4 | Attestation injection | JSON-escaped, never evaluated | R1 | R1 | SAFE by design |
| 5 | Registry lock safety | POSIX flock via withRegistryLock | R1 | R1 | SOUND for cooperative processes |
| 6 | Path traversal via hostname | platform.ValidateHostname: [a-z0-9] only | R1 | R1 | Prevented at API boundary |
| 7 | Managed-only iteration | validateConditionalSkip prevents re-running | R1 | R1 | Skipped steps stay skipped |
| 8 | checkGenerate SSHFS path | Graceful fallback: mount path first, projectRoot second | R1 | R1 | Sound defensive design, works both ways |

## Open Questions (Unverified)
| # | Question | Context |
|---|---------|---------|
| 1 | checkGenerate path: does projectRoot/{hostname}/ match actual SSHFS mount in container mode? | Depends on where .zcp/state lives relative to mount paths |
| 2 | Can WorkflowState have multiple non-nil workflow fields simultaneously? | Engine lifecycle likely prevents it but no structural enforcement |
| 3 | Managed-only iterate test coverage | Logic sound but no explicit test exists |

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Bootstrap flow (5 steps) | HIGH | VERIFIED: all step progression, checkers, skip logic |
| Deploy flow (3 steps) | HIGH | VERIFIED: strategy gate, target building, iteration |
| CI/CD flow (3 steps) | HIGH | VERIFIED: provider extraction, strategy gate |
| Immediate workflows | HIGH | VERIFIED: stateless, 3 of 4 .md files accessible |
| Managed-only fast path | HIGH | VERIFIED: validateConditionalSkip with empty targets |
| Iteration logic | HIGH | VERIFIED: bootstrap resets 2-3, deploy resets 1+ |
| Session lifecycle | HIGH | VERIFIED: atomic init, cleanup, resume |
| Guidance assembly | MEDIUM | bootstrap.md verify section orphaned (gap found) |
| Strategy flow | MEDIUM | Two parallel paths exist (one vestigial) |
| Plan validation | MEDIUM | CREATE/EXISTS checks skipped (nil liveServices); types validated |
| checkGenerate paths | LOW | Container-mode path resolution unverified |
