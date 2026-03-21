# Review Context: analysis-workflow-flow-completeness

**Last updated**: 2026-03-21
**Reviews completed**: 1
**Resolution method**: Evidence-based

## Decision Log

| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | Deploy no-checker is design choice, not vulnerability | Correctness F3 + Architecture F4 | R1 | Deploy operates on already-validated infrastructure; attestation-only is correct for operational workflows |
| 2 | Discover free-text bypass is MAJOR, not CRITICAL | Security F1 + orchestrator trace | R1 | Practical harm is "empty bootstrap" (no metas, no services). Not destructive. But should be blocked. |
| 3 | Strategy auto-assign happens in outputs, not checker | `bootstrap_outputs.go:28-29` + `workflow_checks_strategy.go` | R1 | Two layers: checker validates explicit, outputs auto-assign for dev/simple |
| 4 | Router all-metas on empty liveServices is defensive fallback | `router.go:110-111` | R1 | Better to over-suggest than under-suggest during API failure |

## Rejected Alternatives

| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | Add step checkers to deploy workflow | Correctness analysis | R1 | Deploy is operational — structural checkers would be stale immediately. Agent + tools are the gate. |
| 2 | Add step checkers to CI/CD workflow | Correctness analysis | R1 | CI/CD validates external systems (GitHub, GitLab). Structural checks meaningless. |
| 3 | Per-session file locking | Security F2/F3 | R1 | Low probability, atomic rename prevents corruption. Monitor rather than implement. |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | Provision can be skipped | `bootstrap_steps.go:31` Skippable=false | Security F5 | R1 | REFUTED — provision is mandatory, cannot be skipped |
| 2 | Env var validation gap | `workflow_checks_generate.go:92-109` | Spec line 962 | R1 | Fixed in code, spec is stale |

## Open Questions (Unverified)

- U1: Session Resume TOCTOU — concurrent resume theoretically possible but very low probability
- U2: Concurrent session file read/write — atomic rename prevents corruption but not logical inconsistency
- Deploy workflow: should it read RuntimeType from ServiceMeta or from a discover call at prepare step?

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Bootstrap flow (6 steps) | HIGH | 5 checkers verified, plan validation verified, all paths traced |
| Bootstrap guidance assembly | HIGH | Layer-by-layer verified, iteration delta confirmed |
| Deploy flow (3 steps) | MEDIUM | No checkers (design choice), but knowledge gap V2 is real |
| Deploy guidance pipeline | LOW | RuntimeType not passed — agents lack runtime knowledge |
| CI/CD flow (3 steps) | LOW | No skip, no iterate, no knowledge injection — structurally incomplete |
| Session lifecycle | HIGH | Atomic writes, registry locking, PID detection all verified |
| Router | HIGH | State routing, intent boost, stale meta filtering all verified |
| ServiceMeta lifecycle | HIGH | 2-phase lifecycle (provision + completion) verified |
| Spec accuracy | LOW | 7+ stale sections identified |
