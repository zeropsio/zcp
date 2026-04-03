# Context: analysis-knowledge-mechanisms
**Last updated**: 2026-04-03
**Iterations**: 2
**Task type**: document-review

## Decision Log

| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | All 8 plan issues verified as correctly diagnosed | Code verification across 6 source files | 1 | Every claim matched source code |
| D2 | Keywords field must be kept on Document struct | `cmd/zcp/eval.go:378` uses `doc.Keywords` in `filterRecipesByTag()` | 1 | Removing field would break eval command |
| D3 | Content stripping is safe — no delivery path reads Keywords/TL;DR from Content | Traced all 7 delivery paths (scope, briefing, recipe, query, workflow, MCP resources, H2Sections) | 1 | All downstream code uses parsed fields, not Content |
| D4 | List() removal safe — no Provider mocks exist | grep `*mock*` for Provider = 0 results | 1 | Only Store implements Provider |
| D5 | scope=infrastructure GetUniversals() call is redundant | GetModel() returns full model.md which includes Platform Constraints H2 | 1 | ~30-40 lines duplicated per scope query |
| D6 | extractDecisionSummary() is dead code | Zero production callers, only test references | 1 | getDecisionSummary() at sections.go:296 is active version |
| D7 | content/workflows separation from knowledge is correct | Different consumers, parsing, delivery mechanisms | 2 | Workflows = procedural (injected by engine). Knowledge = factual (pulled by agent). Different embedding, addressing, search needs. |
| D8 | spec-guidance-philosophy.md has 5 outdated claims | universals.md deleted, runtimes/ deleted, GetRecipe composition wrong, filterDeployPatterns missing | 2 | Spec predates knowledge redesign (v3). Must be updated before or with implementation. |
| D9 | spec-bootstrap-deploy.md has 1 outdated reference | filterDeployPatterns() at line 925 | 2 | Same function removal as SD4 |
| D10 | Spec fixes should happen BEFORE code changes | Specs are "Authoritative" per their own headers | 2 | Changing code to match stale specs creates confusion |

## Rejected Alternatives

| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|-------------|-----------------|-----------|--------------|
| RA1 | Remove Keywords field from Document struct | eval.go:378 uses it | 1 | Would break filterRecipesByTag() |
| RA2 | Remove MCP Resource Template | Low cost to maintain (43L), potential future value | 1 | Plan correctly chose SKIP |
| RA3 | Fix Description precedence | Works correctly for both active use cases | 1 | Plan correctly chose SKIP |
| RA4 | Merge content/workflows into knowledge | Different consumers, parsing, embedding | 2 | Would add complexity without benefit. Workflows use `<section>` tags, knowledge uses Document struct. Different addressing (name vs URI). |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| RC1 | H2Sections cache might break after Content stripping | No code accesses H2Sections()["Keywords"] or ["TL;DR"] | 1 | 1 | Safe — all H2Section access is by specific named keys |
| RC2 | engine_search_test.go:227 might break | Uses OR logic: `## Keywords` OR `## PostgreSQL` OR `postgresql` | 1 | 1 | Third condition still matches after stripping |
| RC3 | store_access_test.go fixtures inconsistency | Fixtures bypass parseDocument(), created manually | 1 | 1 | Won't break; optional cleanup for conceptual consistency |

## Open Questions (Unverified)

| # | Question | Context |
|---|----------|---------|
| OQ1 | scope=infrastructure design intent — should it return GetModel() + GetCore() or just GetUniversals() + GetCore()? | Spec doesn't document scope as a composition flow. Current code returns all three (with duplication). After MF2 fix it will be GetModel() + GetCore(). Is full model.md content desired? |

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|------------|----------------|
| Issue #1 (Keywords unused in Search) | VERIFIED | grep + code trace |
| Issue #2 (TL;DR partially functional) | VERIFIED | sections.go:306-307 active usage |
| Issue #3 (List() dead code) | VERIFIED | grep = 0 production callers |
| Issue #4 (MCP Resources) | VERIFIED | No internal callers; external usage unknown but irrelevant |
| Issue #5 (zerops:// cross-refs) | VERIFIED | 60 matches, URI mismatch confirmed |
| Issue #6 (Search scoring) | VERIFIED | engine.go:129-135 code review |
| Issue #7 (Keywords/TL;DR in Content) | VERIFIED | documents.go:87 + all delivery paths traced |
| Issue #8 (Description precedence) | VERIFIED | documents.go:72-79 works correctly |
| MF1 (extractDecisionSummary dead) | VERIFIED | grep = 0 production callers |
| MF2 (scope duplication) | VERIFIED | knowledge.go:110-115 + engine.go:204-222 |
| Content stripping safety | VERIFIED | All 7 delivery paths verified |
| Provider interface safety | VERIFIED | No mocks implement Provider |
| Test impact | VERIFIED | All affected tests identified and analyzed |
| content/workflows separation | VERIFIED | Different consumers, parsing, embedding, addressing |
| spec-guidance-philosophy.md freshness | VERIFIED | 5 stale claims found (universals.md, runtimes/, GetRecipe comp, filterDeployPatterns, layer diagram) |
| spec-bootstrap-deploy.md freshness | VERIFIED | 1 stale reference (filterDeployPatterns) |
| spec-recipe-quality-process.md freshness | VERIFIED | Current — no stale references |
| spec-local-dev.md freshness | VERIFIED | Current — no stale references |
