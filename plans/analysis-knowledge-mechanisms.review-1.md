# Review Report: analysis-knowledge-mechanisms.md — Review 1
**Date**: 2026-04-03
**Reviewed version**: `plans/analysis-knowledge-mechanisms.md`
**Agents**: kb (zerops-knowledge), primary-analyst (Explore), secondary-analyst (Explore)
**Complexity**: Medium (3 agents)
**Focus**: Evaluate implementation readiness; verify all 8 issues; check conceptual consistency

## Evidence Summary

| Agent | Findings | Verified | Logical | Unverified | Downgrades |
|-------|----------|----------|---------|------------|------------|
| kb | 6 findings | 6 | 0 | 0 | 0 |
| primary-analyst | 8 reviews + 2 missed | 10 | 0 | 0 | 0 |
| secondary-analyst | 8 findings + 4 consistency checks | 10 | 2 | 0 | 0 |
| orchestrator | 4 verifications | 4 | 0 | 0 | 0 |

**Overall**: SOUND (with 2 CRITICAL gaps and 1 test impact to address)

---

## Knowledge Base

### From kb agent (zerops-knowledge)

1. `doc.Keywords` — zero production usage in `internal/knowledge/`. **BUT** used in `cmd/zcp/eval.go:378` by `filterRecipesByTag()`. The Keywords struct field MUST be kept.
2. Recipes have NO `## Keywords` or `## TL;DR` sections (they use frontmatter `description:` instead). Stripping only affects themes/guides/bases/decisions (34 files).
3. Content stripping is safe — all downstream code reads from `doc.Keywords`/`doc.TLDR`/`doc.Description` fields, not from Content.
4. MCP Resource Template is a passive MCP endpoint. No internal code calls it. Unknown if any external MCP client uses it.
5. `zerops://` cross-references are effectively broken due to URI prefix mismatch (`zerops://themes/core` vs `zerops://docs/themes/core`).

### From codebase verification (orchestrator)

6. `extractDecisionSummary()` at `sections.go:314-330` — DEAD CODE. Defined + tested, never called in production. `getDecisionSummary()` at line 296 is the active version.
7. `scope=infrastructure` duplicates "Platform Constraints" — `GetModel()` returns full model.md content (includes Platform Constraints H2), then `GetUniversals()` adds the same H2 section again. ~30-40 lines duplicated in the most common reference query.
8. `engine_search_test.go:227` checks `doc.Content` for `"## Keywords"` — but uses OR logic (`## Keywords` OR `## PostgreSQL` OR `postgresql`), so it won't break after stripping (third condition still matches).
9. `store_access_test.go` creates Documents manually (bypasses `parseDocument()`), so stripping logic in `parseDocument()` won't affect these fixtures.

---

## Agent Reports

### Primary Analyst — Correctness + Completeness

**Assessment**: SOUND + CRITICAL GAPS

#### Issue-by-Issue Verification

| Issue | Plan Diagnosis | Verdict | Evidence |
|-------|---------------|---------|----------|
| #1 Keywords not in Search | Correct | VERIFIED | `engine.go:124-135` — Search() never reads `doc.Keywords` |
| #2 TL;DR partially functional | Correct | VERIFIED | `sections.go:306-307` uses `doc.TLDR` actively |
| #3 List() dead code | Correct | VERIFIED | Zero production callers |
| #4 MCP Resources unused | Correct | VERIFIED | No internal callers; URI mismatch real |
| #5 zerops:// cross-refs dead | Correct (count) | VERIFIED | 60 matches across 23 files |
| #6 Search scoring primitive | Correct | VERIFIED | `engine.go:129-135` — Title +2.0, Content +1.0 only |
| #7 Keywords/TL;DR noise in Content | Correct | VERIFIED | `documents.go:87` stores body as-is |
| #8 Description precedence | Correct | VERIFIED | `documents.go:72-79` works correctly |

#### Missed Issues Found

**[MF1] `extractDecisionSummary()` is dead code — MAJOR**
- Location: `sections.go:314-330` (16 lines)
- Only references: `sections_test.go:195,197` (test)
- Active equivalent: `getDecisionSummary()` at `sections.go:296-309` uses `doc.TLDR` instead
- **Action**: Delete function + test. Step 0 cleanup per CLAUDE.md

**[MF2] Platform Constraints duplicated in scope=infrastructure — CRITICAL**
- Location: `tools/knowledge.go:102-115`
- `GetModel()` returns full model.md Content (includes Platform Constraints H2)
- `GetUniversals()` returns the SAME Platform Constraints H2 extracted from model.md
- Both are concatenated into the result → section appears TWICE
- **Impact**: ~30-40 extra lines in every `scope=infrastructure` response
- **Fix options**:
  - A: Remove `GetUniversals()` call from scope handler (model.md already contains it)
  - B: Remove `GetModel()` call, keep only Universals + Core
  - **Recommended**: Option A — model.md provides comprehensive context; removing the redundant Universals extraction is cleanest

### Secondary Analyst — Architecture + Consistency

#### Provider Interface (List removal)

- No mock implements `Provider` — confirmed via grep (zero matches in `*mock*` files)
- `var _ Provider = (*Store)(nil)` at `engine.go:46` will continue to compile after List() removal
- Only 1 test (`engine_doc_test.go:10-14`) calls `store.List()` — delete it
- `Resource` struct at `engine.go:21-26` becomes dead code after List() removal — delete it

#### H2Sections Cache Interaction (Content stripping)

- `H2Sections()` parses from `doc.Content` at runtime — VERIFIED safe
- No production code accesses `H2Sections()["Keywords"]` or `H2Sections()["TL;DR"]`
- All H2Section access is by specific named keys: "Platform Constraints", "Wiring Syntax", "PostgreSQL", etc.
- `workflow/guidance.go:139-140` extracts arbitrary sections by name — still safe (no code requests "Keywords" or "TL;DR" by name)

#### Content Stripping — Delivery Path Analysis

| Delivery Path | Uses doc.Content? | Keywords/TL;DR visible? | After stripping |
|--------------|-------------------|------------------------|-----------------|
| scope=infrastructure | Yes (GetCore, GetModel) | Yes (full Content) | Clean |
| briefing runtime guide | Yes (getRuntimeGuide) | Themes: yes; Recipes: no (no sections) | Themes cleaner |
| briefing service cards | No (H2-extracted) | No (Keywords is separate H2) | No change |
| recipe | Yes (prependRecipeContext) | No (recipes use frontmatter) | No change |
| query (search) | No (300-char snippets) | Low impact | Slightly cleaner snippets |
| workflow inject | No (H2-extracted) | No (Keywords is separate H2) | No change |
| MCP resources | Yes (raw Content) | Yes | Clean (but unused) |

#### Test Impact

| Proposed Change | Tests That Break | Action |
|----------------|-----------------|--------|
| Strip Keywords from Content | `engine_search_test.go:227` — WILL FAIL (checks `"## Keywords"` in Content via OR) | Remove `"## Keywords"` alternative from OR condition |
| Strip TL;DR from Content | None — no test asserts TL;DR string in Content | Safe |
| Remove List() from Provider | `engine_doc_test.go:10-14` TestStore_List | Delete test |
| Remove List() impl + Resource type | Compile-time check at `engine.go:46` still passes | Safe |
| Delete extractDecisionSummary() | `sections_test.go:195,197` | Delete test cases |
| Fix scope duplication | No existing test for this | Add regression test |
| `store_access_test.go` fixtures | Manual Document creation, bypasses parseDocument() | No change needed (optional consistency update) |
| `engine_briefing_test.go` (26 tests) | All check functional content, not Keywords/TL;DR strings | Safe — verified full file (416 lines) |

**Search recall warning**: Stripping Keywords from Content removes those terms from Content-based search scoring. To avoid regression, add Keywords to Search scoring in the same change (R7).

---

## Evidence-Based Resolution

### Verified Concerns

1. **All 8 plan issues are correctly diagnosed** — every claim verified against source code
2. **Keywords stripping from Content is safe** — no production code reads Keywords from Content; all use `doc.Keywords` field
3. **TL;DR stripping from Content is safe** — `getDecisionSummary()` reads from `doc.TLDR` field, not from Content
4. **List() removal is safe** — no production callers, no mocks implement Provider
5. **H2Sections interaction is safe** — no code accesses "Keywords" or "TL;DR" by name via H2Sections()
6. **filterRecipesByTag (eval.go:378)** uses `doc.Keywords` field — field must be kept, but recipes have no `## Keywords` sections anyway so this function is effectively a no-op

### Logical Concerns

7. **scope=infrastructure duplication** — removing `GetUniversals()` from scope handler is the cleanest fix (model.md already includes Platform Constraints). Spec (`spec-guidance-philosophy.md:153`) describes `GetCore()` as "core reference only (platform model + YAML schemas)" — the scope handler goes beyond the spec by also including GetModel() and GetUniversals(), but this appears intentional for comprehensive reference delivery
8. **store_access_test.go fixtures include Keywords/TL;DR in Content** — these bypass parseDocument() so stripping won't affect them, but they represent a conceptual inconsistency after stripping is implemented (test fixtures don't match real parsing behavior)

### Unverified Concerns

None — all concerns verified against code.

### Recommendations (max 7)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | Delete `extractDecisionSummary()` + test as Step 0 cleanup | HIGH | `sections.go:314-330` — dead code, 16 lines | Trivial |
| R2 | Strip `## Keywords` from Content in `parseDocument()` | HIGH | `documents.go:87` — saves ~170L LLM noise | Low |
| R3 | Strip `## TL;DR` from Content in `parseDocument()` | MEDIUM | `documents.go:87` — saves ~34L noise | Low |
| R4 | Fix scope=infrastructure duplication: remove GetUniversals() call | HIGH | `knowledge.go:110-111` — removes ~30-40L duplication | Trivial |
| R5 | Remove `List()` from Provider + delete Resource type + test | LOW | `engine.go:29,167-182,21-26` — dead code | Low |
| R6 | Remove `## See Also` sections from 23 guide/decision files | MEDIUM | 60 broken cross-refs, URI mismatch | Medium |
| R7 | Add Keywords to Search scoring — bundle with R2 to prevent recall regression | MEDIUM | `engine.go:129-135` — Keywords stripped from Content means search loses those terms unless added as separate field | Low |

---

## Revised Version — What Changes and How It Works After Implementation

### Implementation Plan (safe order)

**Phase 1: Dead code cleanup (no behavioral change)**
1. Delete `extractDecisionSummary()` from `sections.go:314-330`
2. Delete test cases in `sections_test.go:195,197`
3. Remove `List()` from Provider interface (`engine.go:30`)
4. Remove `Store.List()` implementation (`engine.go:167-182`)
5. Remove `Resource` struct (`engine.go:21-26`)
6. Delete `TestStore_List` test (`engine_doc_test.go:10-27`)

**Phase 2: Content stripping + search scoring (behavioral change — cleaner LLM output)**
7. Add `stripH2Section(body, heading string) string` helper to `documents.go` (TDD: write tests first — Basic, WithCodeBlock, MultipleH2, NoMatch, LastSection)
8. Call `body = stripH2Section(body, "Keywords")` and `body = stripH2Section(body, "TL;DR")` in `parseDocument()` AFTER extracting keywords and tldr (after line 70, before line 82)
9. Add Keywords to Search() scoring in `engine.go` — prevents recall regression from Content stripping
10. Update `engine_search_test.go:227` — remove `"## Keywords"` from OR condition (WILL fail without this)
11. `store_access_test.go` fixtures bypass parseDocument() — no change needed (optional consistency update)

**Phase 3: Scope duplication fix**
11. In `tools/knowledge.go:110-111`, remove `GetUniversals()` call — `GetModel()` already includes Platform Constraints
12. Add test: `scope=infrastructure` response must NOT contain duplicate "Platform Constraints" heading

**Phase 4: Content cleanup (optional)**
13. Remove `## See Also` sections from 23 guide/decision .md files
14. Optionally add Keywords to Search scoring

### How the System Works After Implementation

1. **Document parsing** (`parseDocument()`): Extracts `Keywords`, `TLDR`, `Description` fields from body. Then strips `## Keywords` and `## TL;DR` H2 sections from body before storing as `Content`. Fields retain parsed data; Content is clean for LLM injection.

2. **scope=infrastructure**: Returns `GetModel()` (full model.md — includes Platform Constraints, Container Universe, Networking, Lifecycle) + `GetCore()` (YAML schemas, rules, grammar). No duplication. Clean Content without Keywords/TL;DR noise. Total noise reduction: ~180-200 lines.

3. **briefing**: Runtime guides and theme docs delivered with clean Content. Service cards still H2-extracted (unaffected). Decision hints still work via `doc.TLDR` field.

4. **recipe**: Already clean (recipes use frontmatter, no Keywords/TL;DR sections). No change.

5. **query (search)**: Snippets extracted from cleaner Content. Optional: Keywords field adds to scoring for better relevance.

6. **Provider interface**: Simpler — 7 methods (was 8). No List(), no Resource type. Clean contract.

7. **`doc.Keywords` field**: Still populated, still usable by `eval.go:filterRecipesByTag()` and for potential future Search scoring. Just not in Content.

---

## Change Log

| # | Section | Change | Evidence | Source |
|---|---------|--------|----------|--------|
| 1 | Missed issue | Added `extractDecisionSummary()` dead code (sections.go:314) | Self-verified: zero production callers | primary-analyst |
| 2 | Missed issue | Added scope=infrastructure Platform Constraints duplication | Self-verified: knowledge.go:110-115 | primary-analyst + orchestrator |
| 3 | Issue #1 | Confirmed Keywords field must stay (eval.go:378 uses it) | kb agent verified | kb |
| 4 | Issue #7 | Verified H2Sections interaction is safe | secondary-analyst verified all access paths | secondary-analyst |
| 5 | Test impact | Identified engine_search_test.go:227 — won't break (OR logic) | Orchestrator verified | orchestrator |
| 6 | Test impact | store_access_test.go bypasses parseDocument() — unaffected | secondary-analyst verified | secondary-analyst |
| 7 | Priority | R4 (scope duplication) added as HIGH priority | ~30-40L duplicated per query | primary-analyst |
