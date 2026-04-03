# Context: Knowledge System Redesign

**Last updated**: 2026-04-02
**Iterations**: 1 (final)
**Task type**: refactoring-analysis
**Status**: Analysis complete, ready for implementation

## Decision Log

| # | Decision | Evidence | Rationale |
|---|----------|----------|-----------|
| D1 | Do NOT rename services.md | 17 hardcoded `"zerops://themes/services"` references | High-cost rename for no functional benefit |
| D2 | universals.md: concepts only (~20L new), no YAML | briefing.go:160-162 prepends to every recipe | YAML examples stay in core.md Multi-Service Examples, injected at provision step only |
| D3 | Import.yml examples: core.md, injected at provision | Agent needs examples at provision (not recipe loading) | getCoreSection(kp, "Multi-Service Examples") — new inject |
| D4 | Runtime rules → recipes ## Gotchas | getRuntimeGuide() loads recipe as runtime guide (sections.go:163-177) | Flows through existing briefing delivery |
| D5 | Flow content → workflow docs | content/workflows/ already exists | Themes/ stays universal platform knowledge only |
| D6 | Knowledge inject step-aware, NOT mode-aware | guidance.go:27-44 — same inject all modes | Mode = procedure (static), knowledge = facts (inject) |
| D7 | Adoption deploy: GET / → 200 only | User's app may lack /health, /status | checkGenerate already skips adopted (line 38) |
| D8 | No import.yml / multi-service wiring in recipes | universals (concept) + services.md (wiring) + core.md (examples) | Agent composes from three sources |
| D9 | Stack Layers = universal concept | Old laravel/django/symfony all had same pattern | Generic version in universals, not per-framework |
| D10 | Old recipe patterns NOT restored per-framework | Derivable from universal knowledge + services.md + recipe zerops.yml | No duplication needed |

## Rejected Alternatives

| # | Alternative | Why Rejected |
|---|------------|-------------|
| R1 | New `bootstrap-flow.md` in themes/ | Mixing flow back into themes/ recreates the problem |
| R2 | Rename services.md → wiring-fundamentals.md | 17 hardcoded references, high regression risk |
| R3 | Expand universals.md to 150+ lines with YAML examples | Prepended to every recipe — bloat. Examples go to core.md, injected at provision only |
| R4 | Add `scope="bootstrap"` delivery mode | Over-engineering; fix existing routing first |
| R5 | Add multi-service wiring examples to recipes | Universal knowledge in universals + services.md; recipes don't need managed service catalog |
| R6 | Restore per-framework Stack Layers (laravel Layer 0-4) | Too verbose, duplicates universals. Generic concept sufficient. |
| R7 | Restore per-framework import.yml in recipes | Import is platform-level; agent composes from schema + examples + universals |

## Resolved Concerns

| # | Concern | Resolution |
|---|---------|-----------|
| C1 | Are Causal Chains universal or runtime-specific? | MIXED: 6 universal + 5 runtime-specific. Split. |
| C2 | Does removing runtime rules break scope=infrastructure? | Yes technically, but briefing(runtime=X) is correct delivery path. Aligns with architecture. |
| C3 | Is guides/ orphaned by design or accident? | By design (query-only), but a gap. Optional Phase 3 improvement. |
| C4 | Two separate deploy paths (bootstrap vs workflow) | Troubleshooting goes to BOTH bootstrap.md deploy AND deploy.md investigate |
| C5 | Recipe research has no knowledge inject path | Add research case to assembleRecipeKnowledge() switch |
| C6 | Removing operations.md H2 breaks 8 tests | Phase 3 (refactor getRelevantDecisions) BEFORE Phase 1b (remove H2) |
| C7 | How does LLM know how to build import.yml? | Schema (core.md) + examples (core.md Multi-Service Examples, newly injected at provision) + concept (universals Stack Composition) + rules (bootstrap.md static guidance) |
| C8 | universals.md getting overloaded with YAML? | YAML examples stay in core.md. universals gets concepts only (~20L). |
| C9 | Adoption flow verification too strict | checkDeploy: GET / → 200 for adopted. checkGenerate already skips (line 38). |
| C10 | Post-adoption runtime context gap | BuildTransitionMessage() includes knowledge hints for adopted services |

## Open Questions

- Should guides/ be integrated into briefing mode, or is query-only sufficient?
- Should assembleRecipeKnowledge() inject decisions at research step? (proposed but low priority)

## Confidence Map

| Area | Confidence | Basis |
|------|-----------|-------|
| Content classification (core.md, operations.md) | HIGH | Line-by-line read by 3 agents + orchestrator + 2 reviewers |
| Routing gap (guides/decisions) | HIGH | Code-verified in briefing.go, sections.go |
| Workflow integration design | HIGH | guidance.go, recipe_guidance.go, deploy_guidance.go all verified |
| Adoption flow | HIGH | checkGenerate:38, checkDeploy:142, bootstrap.md adoption sections verified |
| universals.md sizing | HIGH | Resolved: concepts only (~20L), YAML stays in core.md |
| Implementation sequence correctness | HIGH | Phase ordering verified against test dependencies |
