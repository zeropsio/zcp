# Review Context: analysis-meta-status-purpose

**Last updated**: 2026-03-20
**Reviews completed**: 1
**Resolution method**: Evidence-based

## Decision Log

| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | Remove Status field entirely | 0 decision-point reads (grep verified), MetaStatusDeployed never written, intermediate values overwritten | R1 | Dead code masquerading as documented state |
| 2 | Remove intermediate meta writes (writeServiceMetas calls) | engine.go:164,240 write values unconditionally overwritten by bootstrap_outputs.go:35,57 | R1 | Vestigial writes with no crash recovery reader |
| 3 | Keep Type field | deploy.go:163 reads m.Type for svcCtx.RuntimeType | R1 | Load-bearing in deploy path for knowledge injection |
| 4 | Keep BootstrapSession/BootstrappedAt | Low cost audit trail, intentional design | R1 | Defer removal; document purpose |
| 5 | Meta = "bootstrap decision store" (not state, not cache) | API is orthogonal (operational state), meta stores ZCP-only decisions | R1 | Clear boundary prevents second source of truth |
| 6 | Extend via Decisions map, not new top-level fields | Decisions pattern proven (router, guidance, strategy all read it) | R1 | CLAUDE.md: "extend existing mechanisms" |

## Rejected Alternatives

| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | Remove Type field (architecture proposal) | deploy.go:163 actively reads m.Type; removing breaks deploy knowledge injection | R1 | Adversarial correction verified by orchestrator |
| 2 | Full schema minimization (remove Type, BootstrapSession, BootstrappedAt) | Type is load-bearing; audit fields are low-cost | R1 | Over-aggressive; breaks deploy path |
| 3 | Keep Status but make it useful | Would add complexity for a field that duplicates session-level progress tracking | R1 | Session state already tracks step completion; meta doesn't need to duplicate |
| 4 | Add Status-driven routing (router reads Status) | Router already has filterStaleMetas + Decisions-based routing; Status would be redundant | R1 | Doesn't add capability Decisions can't provide |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | "Meta creates second source of truth" | Meta stores ZCP-only decisions (mode, strategy, pairing) that API doesn't track | R1 | R1 | Concern unfounded for most fields; Status was the only confusing field, now removed |
| 2 | "Layering violation: session state in persistent store" | Intermediate writes are step-boundary checkpoints, not live session state | R1 | R1 | Overstated; resolved by removing intermediate writes entirely |
| 3 | "Intermediate writes serve crash recovery" | No code reads intermediate metas for recovery; session state handles this | R1 | R1 | Vestigial; removed |

## Open Questions (Unverified)

| # | Question | Priority | Notes |
|---|----------|----------|-------|
| 1 | Do external systems (CI/CD, dashboards) read .zcp/services/*.json during bootstrap? | LOW | If yes, intermediate write removal may need reverting |
| 2 | Do agents rely on "— bootstrapped" text in guidance summaries? | LOW | Removing Status removes this text; likely no impact but unverified |

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Status removal | HIGH | VERIFIED: 0 decision reads, overwrite pattern, dead constant |
| Type retention | HIGH | VERIFIED: deploy.go:163 reads actively |
| Intermediate write removal | HIGH | VERIFIED: overwrite pattern, 0 recovery readers |
| BootstrapSession/At retention | MEDIUM | LOGICAL: 0 reads but intentional audit design |
| Decisions as extension pattern | HIGH | VERIFIED: router, guidance, strategy all use it |
| Meta purpose definition | HIGH | VERIFIED: API/meta orthogonality confirmed by live platform |
