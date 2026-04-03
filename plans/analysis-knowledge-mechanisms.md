# Knowledge System Mechanisms Audit

**Date**: 2026-04-03
**Scope**: All code and content in `internal/knowledge/`, `internal/server/resources.go`, `internal/tools/knowledge.go`
**Context**: During knowledge content redesign (v3), we discovered that several underlying MECHANISMS are broken, non-functional, or redundant. This document catalogs all issues with evidence.

---

## System Overview

The ZCP knowledge system embeds ~70 markdown files at compile time and makes them available to LLM agents via:

1. **Tools** (`zerops_knowledge`) — 4 modes: scope, briefing, recipe, query
2. **Workflow injection** (`assembleKnowledge()`) — per-step H2 section extraction
3. **MCP Resources** (`zerops://docs/{+path}`) — resource template for direct document fetch
4. **Search** (query mode) — text matching across all documents

Documents are parsed from markdown into `Document` structs with fields:
- `Content` — full markdown body (frontmatter stripped), injected into LLM context
- `Title` — extracted from `# H1`
- `Keywords` — extracted from `## Keywords` section
- `TLDR` — extracted from `## TL;DR` section
- `Description` — frontmatter `description:` > TLDR > first paragraph
- `URI` — `zerops://{directory}/{filename}` (internal store key)

---

## Issue 1: `## Keywords` sections — parsed but never used by search

**What it is**: 34 out of ~70 knowledge files have a `## Keywords` section:
```markdown
## Keywords
zerops, platform, architecture, lifecycle, build, deploy, run, container, networking...
```

**How it's parsed**: `documents.go:69` calls `extractKeywords(body)` which finds `## Keywords` H2, splits by comma, stores in `doc.Keywords []string`.

**Who uses `doc.Keywords`**: NOBODY in production code.
- `Search()` at `engine.go:109-165` scores by `strings.Contains(titleLower, word)` (+2.0) and `strings.Contains(contentLower, word)` (+1.0). It does NOT check `doc.Keywords`.
- Only callers of `doc.Keywords` are in test files: `engine_doc_test.go:38,128,131` — asserting keywords were parsed.

**The problem**: Keywords sections serve no purpose in search scoring. BUT they remain in `doc.Content` (the field injected into LLM context). Every time the LLM sees a knowledge document, it sees:
```
## Keywords
zerops, platform, architecture, lifecycle, build, deploy, run, container, networking, vxlan, scaling, storage...
```
This is ~5 lines of noise per document, ~170 lines total across all documents.

**Evidence**:
- `engine.go:124-135` — Search() iterates `doc.Title` and `doc.Content`, never `doc.Keywords`
- `engine_doc_test.go:38` — only test reference to Keywords
- `grep -rn 'doc\.Keywords' internal/` — 4 matches, all in test files

**Options**:
A. Strip `## Keywords` from Content during parsing (parse it out, don't include in body)
B. Delete `## Keywords` sections from all 34 .md files
C. Make Search() actually use Keywords for scoring (boost matching)
D. Both A+B — remove from files AND don't parse

---

## Issue 2: `## TL;DR` sections — partially functional, partially noise

**What it is**: Most theme/guide/decision files have a `## TL;DR` section:
```markdown
## TL;DR
YAML generation reference: import.yml and zerops.yml schemas, rules, and examples.
```

**Who uses `doc.TLDR`**:
- `sections.go:306-307` — `getDecisionSummary()` uses TLDR as the decision hint text in briefings. **This is ACTIVE and IMPORTANT.**
- `documents.go:74-76` — TLDR is fallback for Description (after frontmatter `description:`).

**The problem**: Like Keywords, TL;DR text stays in `doc.Content`. When a document is injected into LLM context, the LLM sees both the TL;DR section AND the full content. For a short document like `choose-database.md`, the TL;DR repeats what the first paragraph says.

**Impact**: Lower than Keywords — TL;DR is usually 1-2 lines, not 5+. And it genuinely helps decision hints.

**Options**:
A. Strip `## TL;DR` from Content after parsing (keep the parsed `doc.TLDR` field)
B. Leave as-is (low impact, TL;DR is useful as document summary even in Content)

---

## Issue 3: `List()` method — dead code in production

**What it is**: `Provider` interface at `engine.go:30` defines `List() []Resource`. `Store` implements it at `engine.go:167-182`. Returns all documents as `Resource` structs with URI, Name, Description, MimeType.

**Who calls it**: NOBODY in production.
- `engine_doc_test.go:12` — test calls `store.List()` to verify document count
- `resources.go` — does NOT call List(). Uses `ResourceTemplate` handler with direct `store.Get(uri)`.
- No tool handler calls List()

**The problem**: Dead interface method. The MCP `listResources` would call it via the go-sdk if a client requested the resource list, but no MCP resource list handler is registered — only a ResourceTemplate handler.

**Evidence**:
- `grep -rn 'store\.List()' internal/server/` — 0 matches
- `grep -rn '\.List()' internal/knowledge/` — only test files

**Options**:
A. Remove from Provider interface + Store (breaking change for interface implementors, but only MockClient in tests)
B. Leave as-is (harmless dead code)

---

## Issue 4: MCP Resource Template — registered but unused by LLM clients

**What it is**: `resources.go:12-43` registers an MCP ResourceTemplate:
```go
URITemplate: "zerops://docs/{+path}"
Name: "zerops-docs"
Description: "Zerops knowledge base documents..."
```

When a client calls `ReadResource("zerops://docs/themes/core")`, it maps to `store.Get("zerops://themes/core")` and returns the document content.

**Who uses it**: No LLM client in practice.
- **Claude Code**: supports MCP resources but in practice uses tools (zerops_knowledge) not ReadResource
- **Codex (OpenAI)**: does NOT support MCP resources
- **Gemini CLI**: experimental/incomplete resource support
- Test: `resources_test.go` verifies registration and read — tests pass

**The problem**: Functional but unused infrastructure. Low cost to maintain (43 lines), but creates confusion because `zerops://docs/` prefix differs from internal `zerops://` prefix used in code and markdown cross-references.

**URI mismatch detail**:
- Internal store key: `zerops://themes/core`
- MCP resource URI: `zerops://docs/themes/core`
- Markdown cross-references: `zerops://themes/core` (matches INTERNAL, not MCP)
- Result: markdown cross-references can't be fetched via MCP resources even if a client tried

**Options**:
A. Remove resources.go entirely (no client uses it)
B. Keep as-is (low cost, future potential)
C. Fix URI mismatch (use same prefix everywhere)

---

## Issue 5: `zerops://` cross-references in markdown — 60+ dead links

**What it is**: Many guide and decision files end with "See Also" sections:
```markdown
## See Also
- zerops://themes/services — managed service reference
- zerops://guides/scaling
- zerops://decisions/choose-cache
```

**Count**: 60+ occurrences across guides/ and decisions/ files.

**The problem**:
1. These URIs use internal store keys (`zerops://themes/services`), NOT MCP resource URIs (`zerops://docs/themes/services`). Even if a client supported resources, the URIs wouldn't resolve.
2. LLM sees these as plain text strings. Cannot click, fetch, or navigate to them.
3. LLM might try to call `zerops_knowledge query="zerops://themes/services"` which would search for the literal string — probably returning no useful results.
4. In practice, LLMs ignore these links entirely.

**Evidence**: `grep -rn 'zerops://' internal/knowledge/ --include='*.md' | wc -l` → 60+ matches in See Also sections.

**Options**:
A. Replace with `zerops_knowledge` tool call suggestions: `zerops_knowledge query="scaling"` instead of `zerops://guides/scaling`
B. Remove See Also sections entirely (LLM can search on its own)
C. Leave as-is (end of files, low noise)

---

## Issue 6: Search scoring — functional but primitive

**What it is**: `Search()` at `engine.go:109-165` implements text matching:
```go
for _, word := range words {
    if strings.Contains(titleLower, word) { score += 2.0 }
    if strings.Contains(contentLower, word) { score += 1.0 }
}
```

Plus `queryAliases` expand common terms: "redis" → "redis valkey", "postgres" → "postgres postgresql".

**What works**:
- Title boost (2x) helps relevant documents surface
- Query aliases handle common synonyms
- Simple and fast — no external dependencies

**What doesn't work well**:
- No inverse document frequency — common words like "service", "container", "deploy" match almost every document
- No phrase matching — "object storage" matches any doc with "object" OR "storage" separately
- No proximity — words on opposite ends of a 200-line document score same as adjacent words
- Keywords field parsed but NOT used in scoring (Issue 1)
- All documents scored equally regardless of type — a 200L guide scores same weight as a 15L base file

**Impact**: Agent searching for "object storage integration" gets operations.md, core.md, guides/object-storage-integration.md all with similar scores. The specific guide might not be top result.

**Options**:
A. Add Keywords to scoring (use the parsed field)
B. Add document-type weighting (guides score higher for query mode)
C. Implement TF-IDF (term frequency × inverse document frequency)
D. Leave as-is (agents can refine queries, briefing mode covers most needs)

---

## Issue 7: `## Keywords` and `## TL;DR` visible in LLM content

**What it is**: When any knowledge content is injected into LLM context (via scope, briefing, recipe, or query), the `doc.Content` field is used. This field contains the full markdown body INCLUDING `## Keywords` and `## TL;DR` sections.

**Example** — what LLM sees when core.md is delivered via `scope=infrastructure`:
```markdown
# Zerops YAML Reference

## TL;DR
YAML generation reference: import.yml and zerops.yml schemas, rules, pitfalls, and complete multi-service examples.

## Keywords
import.yml, zerops.yml, schema, ports, binding, environment variables, autoscaling, yaml, pipeline, tilde, HA, NON_HA, cron, health check, readiness check, prepareCommands, buildCommands, deployFiles, envSecrets, envVariables, preprocessor

---

## import.yml Schema
...
```

The `## Keywords` section is 2-3 lines of comma-separated terms that mean nothing to the LLM as context. The `## TL;DR` repeats what the title and first section already say.

**Impact**: Every knowledge delivery includes ~5 extra lines of noise per document. For `scope=infrastructure` (model + constraints + core = 3 docs), that's ~15 lines. For briefing (runtime guide + service cards), it's in the Content but service cards are H2-extracted (Keywords stripped). For recipes, ~5 lines per recipe.

**The fix**: Strip `## Keywords` and `## TL;DR` sections from `doc.Content` during parsing. The parsed `doc.Keywords`, `doc.TLDR`, and `doc.Description` fields retain the data for programmatic use (decision hints, Description fallback). Content becomes cleaner for LLM consumption.

**Implementation**: In `parseDocument()` at `documents.go:61`, after extracting keywords and tldr, strip those H2 sections from body before storing as Content:
```go
body = stripH2Section(body, "Keywords")
body = stripH2Section(body, "TL;DR")
```

---

## Issue 8: Frontmatter `description` vs `## TL;DR` vs first paragraph — confusing precedence

**What it is**: `Description` field has a 3-level fallback (`documents.go:72-79`):
1. Frontmatter `description:` field (if present)
2. `## TL;DR` section content (if present)
3. First paragraph of body

**Who uses Description**:
- `briefing.go:209-211` — recipe disambiguation (shows Description next to recipe name)
- `engine.go:174` — List() resources (dead code)
- `sections.go:309` — getDecisionSummary fallback (if no TLDR)

**The problem**: Not broken, but confusing. Recipes use frontmatter description (rich, one-line). Themes/guides use TL;DR (often longer than one line, sometimes a paragraph). Some documents have BOTH frontmatter description AND TL;DR with different content.

**Impact**: Low. Description works correctly for its two active use cases (recipe disambiguation + decision summaries).

**Options**: Leave as-is. Low priority.

---

## Priority ranking

| Priority | Issue | Action | Impact |
|----------|-------|--------|--------|
| **HIGH** | #1+#7: Keywords in Content | Strip from Content during parsing | -170L noise across all LLM responses |
| **MEDIUM** | #5: zerops:// dead links | Remove See Also sections or replace with tool suggestions | Cleaner docs, less LLM confusion |
| **MEDIUM** | #7: TL;DR in Content | Strip from Content during parsing (keep TLDR field) | Cleaner per-doc, minor savings |
| **LOW** | #3: List() dead code | Remove from interface | Code cleanup |
| **LOW** | #6: Search scoring | Add Keywords to scoring | Better search results |
| **SKIP** | #4: MCP Resources | Leave registered | Low cost, future potential |
| **SKIP** | #8: Description precedence | Leave as-is | Works correctly |

---

## Relevant code files

| File | Role | Lines |
|------|------|-------|
| `internal/knowledge/documents.go` | Document parsing, embedding, frontmatter extraction | 228 |
| `internal/knowledge/engine.go` | Store, Search, Provider interface, query aliases | ~300 |
| `internal/knowledge/briefing.go` | GetBriefing (7-layer), GetRecipe, prepend universals | ~250 |
| `internal/knowledge/sections.go` | H2/H3 parsing, runtime/service normalization, decisions | ~330 |
| `internal/server/resources.go` | MCP Resource Template registration | 43 |
| `internal/tools/knowledge.go` | zerops_knowledge tool handler (4 modes) | ~170 |
| `internal/knowledge/themes/*.md` | 4 theme files (model, core, services, operations) | ~700 |
| `internal/knowledge/guides/*.md` | 20 guide files | ~2200 |
| `internal/knowledge/decisions/*.md` | 5 decision files | ~235 |
| `internal/knowledge/bases/*.md` | 5 base files | ~100 |
| `internal/knowledge/recipes/*.md` | 33 recipe files (gitignored, synced) | ~3500 |

---

## How content reaches the LLM (delivery paths)

For full delivery map see memory file: `map_knowledge_delivery_paths.md`

Summary relevant to this audit:

1. **scope=infrastructure** → `GetModel()` + `GetUniversals()` + `GetCore()` → raw Content fields concatenated. LLM sees Keywords + TL;DR in full.

2. **briefing** → `GetBriefing()` → runtime guide Content + service card H2 sections + wiring H2 + decision TLDR. Service cards are H2-extracted (Keywords stripped). But runtime guide is full Content (Keywords visible).

3. **recipe** → `GetRecipe()` → universals (Platform Constraints H2 from model.md, no Keywords) + recipe Content (frontmatter stripped but Keywords visible if present in recipes — recipes use frontmatter description, not ## Keywords, so this is mostly clean).

4. **query** → `Search()` → returns snippets (300 char), not full Content. Keywords don't affect scoring but are in the snippet source.

5. **workflow inject** → `getCoreSection()` → extracts specific H2 section from core.md Content. Keywords section is a separate H2 so it's NOT included in extracted sections. Clean.

6. **MCP resources** → `ReadResource()` → returns raw Content. Keywords visible. But nobody calls this.

**Where Keywords/TL;DR noise actually hits the LLM**:
- scope=infrastructure: model.md Keywords + core.md Keywords = ~10 lines noise
- briefing runtime guide: hello-world recipe usually has NO Keywords (uses frontmatter), clean
- recipes directly: recipe files use frontmatter, no Keywords section typically. BUT some theme files have it.
- guides via query: snippets only, low impact

**Biggest noise source**: `scope=infrastructure` call, which returns 3 full documents with Keywords sections.
