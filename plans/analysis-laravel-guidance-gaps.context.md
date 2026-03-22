# Context: analysis-laravel-guidance-gaps
**Last updated**: 2026-03-22
**Iterations**: 1
**Task type**: codebase-analysis

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Root cause is in bootstrap.md workflow, not in knowledge themes | core.md:272,275 + services.md:14,20 already document envSecrets thoroughly | 1 | Knowledge exists but workflow doesn't reference it |
| D2 | Recipe auto-injection preferred over stronger hint language | Incident proves agents ignore hints regardless of wording | 1 | Structural fix > text instruction |
| D3 | Fix in bootstrap.md + guidance.go, not in recipe content | laravel.md already comprehensive and correct | 1 | Recipe is fine; delivery pipeline is broken |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|-------------|-----------------|-----------|--------------|
| A1 | Make all recipes auto-inject for every PHP bootstrap | Would bloat guidance with irrelevant content (Symfony recipe for Laravel bootstrap) | 1 | Need framework detection, not blanket injection |
| A2 | Add APP_KEY-specific guidance to bootstrap.md | Too narrow — same pattern applies to Rails, Django, Phoenix | 1 | Generic "framework secrets" guidance covers all |

## Resolved Concerns
(none yet — iteration 1)

## Open Questions (Unverified)
| # | Question | Status |
|---|----------|--------|
| Q1 | Should recipe auto-injection require framework detection from user intent, or inject all matching recipes? | Needs design discussion |
| Q2 | Should zerops_env be documented as the primary post-import secret-setting mechanism, or keep envSecrets as primary? | Needs team decision |
| Q3 | Does the `.env` shadowing issue affect Symfony/Nette too, or is it Laravel-specific? | Needs verification against symfony.md, nette.md recipes |

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|----------------|
| F1-F3 (recipe delivery gap) | VERIFIED | Code-level trace through briefing.go, guidance.go, bootstrap.md |
| F4-F5 (provision gap) | VERIFIED | Read bootstrap.md provision section, confirmed missing content |
| F6 (.env shadowing) | VERIFIED | Read bootstrap.md warnings vs laravel.md scaffolding guidance |
| R1 (auto-inject recipe) | LOGICAL | Follows from F2 evidence; implementation not yet designed |
| R2-R5 (bootstrap.md changes) | VERIFIED | Exact insertion points identified in bootstrap.md |
