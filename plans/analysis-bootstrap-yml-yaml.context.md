# Context: analysis-bootstrap-yml-yaml
**Last updated**: 2026-04-03
**Iterations**: 1
**Task type**: refactoring-analysis + implementation-planning

## Decision Log

| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| 1 | Keep type/function names (`ZeropsYmlDoc`, `ParseZeropsYml`) as-is | 3 callers, all internal to `tools/` | 1 | Zero user impact, rename is churn for no value |
| 2 | Fallback strategy: try `.yaml` first, fall back to `.yml` in `ParseZeropsYml()` | zcli defaults to `zerops.yaml`; API is extension-agnostic | 1 | Preserves backward compat while aligning with platform default |
| 3 | Knowledge section headers must change atomically with lookup keys | `guidance.go:86` ↔ `core.md:7` exact string match coupling | 1 | Silent failure if desynchronized; no test coverage exists |
| 4 | `eval/prompt.go` uses `strings.Contains` substring match — must be updated with recipe section migration | `eval/markdown.go:10` | 1 | Recipe sections already use `zerops.yaml`; eval won't find them with `"import.yml"` substring |
| 5 | Example tool calls should go in provision rules section (~line 162), not in mode-specific subsections | `bootstrap.md:472,535` already have examples but LLM encounters import at line 123 first | 1 | Fix upstream — show correct params before LLM needs them |

## Rejected Alternatives

| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|-------------|-----------------|-----------|--------------|
| 1 | Rename `ZeropsYmlDoc` → `ZeropsYamlDoc` | Only 3 callers, all internal | 1 | Churn without user benefit; both analysts agree |
| 2 | Inject example calls via guidance assembly code | Guide assembly is for knowledge injection, not static examples | 1 | Content in `bootstrap.md` is the right place for static examples |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| — | — | — | — | — | — |

## Open Questions (Unverified)

- Does zcli fall back from `zerops.yaml` to `zerops.yml` (or vice versa) when default lookup fails? Cannot verify without zcli source code.
- Do ALL recipe markdown files consistently use `## zerops.yaml` headers, or are some still `## zerops.yml`? KB says mixed; needs per-file verification before Phase 3.

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|-----------|----------------|
| ParseZeropsYml fallback | VERIFIED | Read `deploy_validate.go:126-139` |
| guidance.go ↔ core.md coupling | VERIFIED | Read both files, confirmed exact string match |
| eval/prompt.go substring matching | VERIFIED | Read `eval/markdown.go:6-15` |
| bootstrap.md example call gap | VERIFIED | Read provision section (122-202) vs examples (472-535) |
| deploy_guidance.go references | VERIFIED | Grep confirmed 7 matches |
| E2E test file count | LOGICAL | Adversarial grep; individual files not read |
| Recipe section header consistency | UNVERIFIED | KB reports mixed; needs file-by-file check |
