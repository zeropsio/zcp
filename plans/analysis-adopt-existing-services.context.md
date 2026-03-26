# Context: analysis-adopt-existing-services

**Last updated**: 2026-03-26
**Iterations**: 1
**Task type**: implementation-planning

## Decision Log

| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Extend bootstrap, NOT new workflow | `workflow_checks.go:64` IsExisting already load-bearing; engine/session/checkers are workflow-agnostic | 1 | New workflow duplicates 95% of bootstrap; IsExisting + conditional logic achieves same result with ~470L vs ~1250L |
| D2 | No `origin` field on ServiceMeta | Zero consumer code paths branch on origin; YAGNI | 1 | Deploy, strategy, routing are all origin-agnostic. Git history + BootstrapSession provide audit if needed. |
| D3 | PreserveCode as BootstrapState map, NOT RuntimeTarget field | Plan is immutable after discover; code preservation is per-iteration generate-step concern | 1 | Follows Strategies map pattern (mutable state separate from sealed plan) |
| D4 | Default to preserve existing code | Risk of overwriting production logic outweighs convenience of fresh scaffold | 1 | RISK1: silent code corruption is highest-impact risk in adoption scenario |

## Rejected Alternatives

| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|------------|-----------------|-----------|--------------|
| A1 | New "adopt" workflow (separate from bootstrap) | Engine, session, checkers are all workflow-agnostic; IsExisting already works in bootstrap | 1 | 95% code duplication, violates DRY, creates maintenance burden for two near-identical workflows |
| A2 | `origin` field on ServiceMeta | Zero consumers; no behavioral branching on origin anywhere in codebase | 1 | Dead weight — adds field + validation + docs for zero functional value |
| A3 | `PreserveExistingCode` on RuntimeTarget | Plan is sealed after discover; code preservation is per-iteration decision | 1 | Conflates stable (plan) with transient (step guidance); violates plan immutability pattern |
| A4 | Separate adopt.md guidance doc (~500L) | 80%+ content would duplicate bootstrap.md sections | 1 | Single source of truth; extend bootstrap.md with conditional sections instead |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| RC1 | NON_CONFORMANT is a dead end | `router.go:129-158` already routes to bootstrap+debug/deploy | 1 | 1 | Not a dead end — already has routing. Adoption = expanding discover guidance for option (c) |
| RC2 | IsExisting minimally used | 8+ tests, E2E scenario, load-bearing logic at workflow_checks.go:64 | 1 | 1 | Already actively used and tested; primary undercounted |

## Open Questions (Unverified)

1. Import with existing hostname + override=false: silent success, error, or partial? Needs live verification.
2. Shared storage relationship discovery: no API field, needs inference or SSH inspection.
3. Can adopted services with existing zerops.yml have their env var mappings validated against discovered vars without parsing the YAML from the filesystem?

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|---------------|
| Bootstrap extension approach | HIGH | Code evidence: IsExisting tested, engine agnostic, router handles NON_CONFORMANT |
| Provision checker changes | HIGH | Code: workflow_checks.go:55-68 clearly shows where to add IsExisting dev check |
| Generate conditionality | MEDIUM | Pattern exists (Strategies map) but no implementation yet; needs testing |
| Guidance adequacy | MEDIUM | bootstrap.md option (c) is 1 line; expansion scope estimated but not drafted |
| Import idempotency for adoption | LOW | UNCHECKED against live platform — override=false behavior undocumented |
