# Review Report: analysis-knowledge-mechanisms.md — Review 2
**Date**: 2026-04-03
**Focus**: (1) content/workflows vs knowledge separation, (2) spec freshness audit, (3) full concept re-validation
**Previous**: review-1.md verified all 8 issues + found 2 gaps (MF1, MF2)

---

## Part 1: Should `internal/content/workflows/` be in `internal/knowledge/`?

### Current architecture

```
internal/content/
├── workflows/          ← 4 workflow scripts (bootstrap, deploy, recipe, cicd)
│   └── *.md            ← Consumed via content.GetWorkflow("name")
├── templates/          ← Config file templates (claude.md, mcp-config.json, etc.)
│   └── *               ← Consumed via content.GetTemplate("name")
└── content.go          ← embed.FS + GetWorkflow/GetTemplate/ListWorkflows

internal/knowledge/
├── themes/             ← 4 theme docs (model, core, services, operations)
├── guides/             ← 20 guides (firewall, scaling, etc.)
├── decisions/          ← 5 decision docs
├── bases/              ← 5 infrastructure base guides
├── recipes/            ← 33 recipes (gitignored, synced from API)
├── engine.go           ← Provider interface (Search, Get, GetBriefing, GetRecipe, etc.)
├── documents.go        ← Document struct (URI, Title, Keywords, TLDR, Content, H2Sections)
├── briefing.go         ← Layered knowledge composition
└── sections.go         ← H2/H3 parsing, normalization
```

### How they interact

The workflow engine (`internal/workflow/guidance.go`) uses BOTH:

```
assembleGuidance()
  ├── resolveStaticGuidance()     → content.GetWorkflow("bootstrap") → <section> extraction
  └── assembleKnowledge()         → knowledge.Provider.GetModel/GetBriefing/GetCore → H2 extraction
```

| Aspect | content/workflows | knowledge |
|--------|------------------|-----------|
| **Purpose** | Procedural scripts (step-by-step instructions) | Factual reference (platform facts, schemas, recipes) |
| **Parsing** | `<section name="...">` tags, raw markdown | `Document` struct: URI, Title, Keywords, TLDR, H2Sections |
| **Consumer** | Workflow engine (system-initiated at each step) | `zerops_knowledge` MCP tool (agent-initiated on demand) |
| **Delivery** | Injected into workflow responses automatically | Pulled by agent via tool calls |
| **Addressing** | By name: `GetWorkflow("bootstrap")` | By URI: `zerops://themes/core`, or search/briefing/recipe |
| **Search** | None (not searchable) | BM25 text search via `Search()` |
| **Embedding** | `//go:embed workflows/*.md` in content.go | `//go:embed themes/*.md bases/*.md ...` in documents.go |

### Verdict: Separation is correct

The packages serve different roles in the delivery pipeline per the spec's "inject rules, point to knowledge" principle (spec-guidance-philosophy.md §2.4):

- **Workflows = INJECT** — procedural instructions injected automatically at each workflow step. They tell the agent WHAT TO DO in what ORDER.
- **Knowledge = POINT** — factual reference available on demand. Provides platform facts, YAML schemas, recipes, service cards.

Merging them would create unnecessary complexity:
- Knowledge's `Document` struct (URI, Keywords, TLDR, H2Sections, BM25 search) is irrelevant for workflow scripts
- Workflows' `<section>` tag parsing is irrelevant for knowledge documents
- Workflows are NOT searchable by design — they're loaded by name at specific workflow steps
- Different embedding strategies (knowledge has ~70 docs parsed into structs; workflows are 4 raw files)

**The current separation is clean and correct. No change recommended.**

The conceptual link between them (workflows inject platform facts AND point to knowledge) is properly handled at the `guidance.go` layer, which is the right place for composition.

---

## Part 2: Spec Freshness Audit

### spec-guidance-philosophy.md (2026-03-22) — OUTDATED (5 issues)

**[SD1] Layer 0 references `themes/universals.md` — FILE DOES NOT EXIST** — CRITICAL
- Spec line 130: `Layer 0: Universals (themes/universals.md)`
- Reality: `universals.md` was deleted during knowledge redesign. `GetUniversals()` at `engine.go:202-213` now extracts "Platform Constraints" H2 from `themes/model.md`.
- The entire layer diagram (lines 117-132) is stale.
- Evidence: `ls internal/knowledge/themes/` → core.md, model.md, operations.md, services.md. No universals.md.

**[SD2] Layer 3 references `runtimes/*.md` — DIRECTORY DOES NOT EXIST** — CRITICAL
- Spec line 121: `Layer 3: Runtime Guides (runtimes/*.md)`
- Reality: `runtimes/` was deleted. Runtime knowledge now lives in hello-world recipes (`recipes/{runtime}-hello-world.md`) + infrastructure bases (`bases/*.md`).
- `getRuntimeGuide()` at `sections.go:163-177` resolves: `recipes/{slug}-hello-world` → `recipes/{slug}` → `bases/{slug}`.
- Evidence: `ls internal/knowledge/runtimes/` → directory not found.

**[SD3] GetRecipe composition claim is wrong** — MAJOR
- Spec line 151: `GetRecipe(name, mode) → universals + runtime guide + recipe`
- Reality: `briefing.go:158-161` says explicitly: "Runtime guides are NOT prepended — each recipe is standalone with its own knowledge." Only universals (Platform Constraints from model.md) are prepended.
- Code: `prependRecipeContext()` calls only `prependUniversals()`.

**[SD4] `filterDeployPatterns` referenced but does not exist** — MAJOR
- Spec line 145: "Mode-aware filtering (`filterDeployPatterns` in `briefing.go`)"
- Reality: `grep -r filterDeployPatterns internal/ --include='*.go'` → 0 matches. Function never existed or was removed.
- Also referenced in spec-bootstrap-deploy.md line 925.

**[SD5] Layer diagram doesn't reflect current structure** — MAJOR
- Current layers should be:
  - Layer 0: Platform Constraints (H2 section from themes/model.md)
  - Layer 1: Core Reference (themes/core.md)
  - Layer 2: Service Cards (themes/services.md)
  - Layer 3: Runtime Guides (recipes/{runtime}-hello-world.md → bases/{slug}.md)
  - Layer 4: Recipes (recipes/*.md)

### spec-bootstrap-deploy.md (2026-03-22) — MOSTLY CURRENT (1 issue)

**[SD6] References `filterDeployPatterns()` (line 925)** — same as SD4 above.

Otherwise the bootstrap flow (5 steps), deploy flow, and step specifications appear to match the code in `internal/workflow/`.

### spec-recipe-quality-process.md — CURRENT

No stale references found. Correctly references recipes (not runtimes), live platform testing, E2E test patterns.

### spec-local-dev.md (2026-03-27) — CURRENT

Environment detection, auth model, topology all match current code.

### Stale comment in code

`recipe_lint_test.go:195` — comment references "universals.md" but `GetUniversals()` reads from model.md. Minor stale comment.

---

## Part 3: Full Concept Re-Validation

### The knowledge delivery concept

The overall concept is sound and well-designed:

1. **Inject vs Point** — workflows inject platform mechanics (10-30 lines per step), agents pull knowledge on demand via `zerops_knowledge`. This prevents context bloat while ensuring critical facts are never missed.

2. **Layered composition** — briefings compose dynamically from runtime guide + service cards + wiring + decisions + version check. Each layer is optional. Recipes inherit universals.

3. **Session-aware mode filtering** — `resolveKnowledgeMode()` auto-detects dev/standard/simple from active workflow. Agent can override with explicit `mode` parameter.

4. **Workflow + knowledge separation** — `guidance.go` assembles step guidance from static workflow content + injected knowledge. Clean composition point.

### Does the mechanisms audit (plan) fit within this concept?

**YES** — all proposed changes are consistent with the concept:

| Proposed Change | Concept Impact | Assessment |
|----------------|---------------|------------|
| Strip Keywords from Content | Reduces noise in POINT delivery (cleaner knowledge) | Aligned |
| Strip TL;DR from Content | Same — cleaner knowledge for LLM | Aligned |
| Add Keywords to Search scoring | Improves POINT delivery (better search results) | Aligned |
| Remove List() from Provider | Interface cleanup, no concept impact | Aligned |
| Fix scope=infrastructure duplication | Fixes a POINT delivery bug (Platform Constraints 2x) | Aligned |
| Remove zerops:// cross-refs | Removes broken references in POINT delivery | Aligned |

### What the system looks like after ALL changes (plan + spec fixes)

```
Agent session starts
  ↓
Agent calls zerops_workflow action="start" workflow="bootstrap"
  ↓
Workflow engine:
  1. content.GetWorkflow("bootstrap") → static script with <section> tags
  2. guidance.go assembles: static guidance + knowledge injection
     - discover step: injects GetModel() (full model.md, CLEAN — no Keywords/TL;DR noise)
     - provision step: injects "import.yml Schema" H2 from core.md
     - generate step: injects GetBriefing() + env vars + zerops.yml schema
     - deploy step: injects "Schema Rules" H2 from core.md
  3. Returns assembled guidance to agent
  ↓
Agent calls zerops_knowledge scope="infrastructure" (on demand)
  → Returns: GetModel() + GetCore() — NO duplication (GetUniversals removed)
  → Content is clean (Keywords/TL;DR stripped during parsing)
  ↓
Agent calls zerops_knowledge recipe="laravel"
  → Returns: Platform Constraints (from model.md) + recipe content
  → Recipe content is clean (recipes use frontmatter, no Keywords)
  ↓
Agent calls zerops_knowledge query="object storage"
  → Search uses Title (+2.0) + Content (+1.0) + Keywords (+1.5?)
  → Returns snippets from clean Content
```

### Remaining concerns

1. **Spec drift is the biggest risk** — spec-guidance-philosophy.md is the canonical reference but 5 claims are wrong. Any agent reading the spec will have incorrect mental model. This should be fixed BEFORE or WITH the mechanisms implementation.

2. **scope=infrastructure intent is ambiguous** — the spec (line 153) says `GetCore()` is "core reference only" but the implementation adds GetModel() + GetUniversals(). After removing GetUniversals() (MF2 fix), the result is GetModel() + GetCore(). Is this the intended design? The spec doesn't document scope=infrastructure as a composition flow.

3. **`extractDecisionSummary()` and `resource_test.go` dead code** — minor cleanup that should happen in the same pass.

---

## Updated Recommendations

| # | Recommendation | Priority | Effort |
|---|---------------|----------|--------|
| R1 | **Fix spec-guidance-philosophy.md** — update layer diagram, remove universals.md/runtimes/ refs, fix GetRecipe composition, remove filterDeployPatterns | CRITICAL | Medium |
| R2 | **Fix spec-bootstrap-deploy.md line 925** — remove filterDeployPatterns reference | HIGH | Trivial |
| R3 | Delete `extractDecisionSummary()` dead code (Step 0) | HIGH | Trivial |
| R4 | Strip Keywords/TL;DR from Content + add Keywords to Search scoring | HIGH | Low |
| R5 | Fix scope=infrastructure duplication — remove GetUniversals() call | HIGH | Trivial |
| R6 | Remove List() + Resource type from Provider | LOW | Low |
| R7 | Remove See Also sections from 23 .md files | MEDIUM | Medium |
| R8 | Document scope=infrastructure as a composition flow in spec | MEDIUM | Low |
| R9 | Fix stale comment in recipe_lint_test.go:195 (universals.md ref) | LOW | Trivial |

### Implementation order

1. **Spec fixes first** (R1, R2, R8) — update authoritative docs before changing code
2. **Dead code cleanup** (R3, R6, R9) — Step 0 per CLAUDE.md
3. **Content stripping + search** (R4) — behavioral change
4. **Scope fix** (R5) — remove duplication
5. **Content cleanup** (R7) — optional
