# Knowledge Redesign — Complete Summary for Spec/Doc Updates

**Date**: 2026-04-03
**Purpose**: Self-contained briefing for an agent updating specs, docs, and CLAUDE.md after the v3 knowledge redesign. Covers what changed, why, what's now wrong in docs, and what mechanism issues remain.

---

## Part 1: What changed in v3 and why

### Before (old architecture)
```
themes/universals.md (77L) — "Platform truths" prepended to recipes
themes/core.md (464L)      — Everything: platform model + YAML schemas + Rules & Pitfalls + Causal Chains + examples
themes/services.md (226L)  — Managed service cards + wiring
themes/operations.md (273L)— Ops + flow-specific content mixed
```

### After (v3 architecture)
```
themes/model.md (~145L)      — NEW: Platform model (Container Universe, Lifecycle, Networking, Storage, 
                                Scaling, Base Image) + Container Lifecycle + Immutable Decisions + 
                                Platform Constraints section
themes/core.md (~250L)       — YAML reference ONLY: import.yml schema, zerops.yml schema, Schema Rules,
                                Multi-Service Examples. NO Rules & Pitfalls, NO Causal Chains.
themes/services.md (226L)   — Unchanged
themes/operations.md (141L) — Cleaned: removed Service Selection, Tool Access, Dev vs Stage, 
                                Verification, Troubleshooting (moved to workflow docs)
themes/universals.md         — DELETED
```

### Why the change

1. **Rules & Pitfalls (35 rules) removed from core.md** — E2E testing against live Zerops API proved that the platform import API catches most invalid configurations (valkey@8, minContainers on managed, verticalAutoscaling on storage, hostname format). Remaining rules are covered by: recipe examples (correct patterns shown by example), model.md (conceptual understanding), flow docs (procedural guidance), or new ZCP checkers (envVariables silently dropped, /var/www in prepareCommands, php-nginx as build base).

2. **universals.md deleted** — Every item was covered elsewhere. The file mixed constraints, concepts, and conventions. `GetUniversals()` now extracts the "## Platform Constraints" H2 section from model.md (~12 lines of hard rules).

3. **model.md created** — Platform conceptual model (how Zerops works) was buried in core.md where it was never extracted per-section. Now it has its own file, injected at bootstrap discover step (agent needs to understand platform before planning stack). Also included in scope=infrastructure response.

4. **New checkers added** — 3 new validation checks in `checkGenerate` and `import.go`:
   - `envVariables` at service level in import → warning (API silently drops, E2E verified)
   - `/var/www` in `run.prepareCommands` → error (deploy files arrive after prepare)
   - `php-nginx`/`php-apache` as `build.base` → error (webserver variants are run bases only)

5. **hostname max fixed 25→40** — E2E verified: API accepts up to 40 chars (41 rejected). JSON schema description says 25 but has no maxLength constraint. ZCP's hostname.go regex updated from `{0,24}` to `{0,39}`.

6. **Runtime mode always HA** — E2E verified: all runtime services forced to HA regardless of mode input. Model.md Scaling section documents this.

### Code changes summary

| File | Change |
|------|--------|
| `engine.go` | Added `GetModel()` to Provider interface + Store. `GetUniversals()` now extracts "Platform Constraints" H2 from model.md instead of reading deleted universals.md. |
| `guidance.go` | Discover step now injects `GetModel()` content (platform model for agent planning). |
| `tools/knowledge.go` | `scope=infrastructure` now returns: model + constraints + core + live stacks. Removed universals prepend (constraints come from model.md section). |
| `sections.go` | `getRelevantDecisions()` reads from `decisions/` files directly instead of operations.md H3 subsections. |
| `hostname.go` | Regex `{0,24}` → `{0,39}`. Error messages updated 25→40. |
| `import.go` | Added envVariables-at-service-level warning after `ValidateServiceTypes()`. |
| `workflow_checks_generate.go` | Added prepareCommands /var/www check + php-nginx/php-apache build.base check. |
| `deploy_validate.go` | Exported `BaseStrings()` (was `baseStrings()`). Added `PrepareCommands` field to `zeropsYmlRun`. |
| `events.go` | Event Monitoring rules (filter by hostname, stop polling after FINISHED, check stack.build not appVersion) moved to tool description text. |

### Content changes

| File | Change |
|------|--------|
| model.md | NEW: Container Universe, Two YAML Files, Build/Deploy Lifecycle, Networking, Storage, Scaling (with runtime HA note), Base Image, Container Lifecycle, Immutable Decisions (hostname max 40), Platform Constraints |
| core.md | Removed: STOP warning, Platform Model sections, Rules & Pitfalls (35 rules), Causal Chains (7 rows). Updated: hostname max 25→40 in schema comment. |
| operations.md | Removed: Service Selection Decisions, Tool Access Patterns, Dev vs Stage, Verification & Attestation, Troubleshooting (~132 lines). |
| universals.md | DELETED entirely. |
| bootstrap.md | Added: hostname conventions, priority rules, zeropsSetup guidance, troubleshooting table (6 rows). |
| deploy.md | Added: 4 troubleshooting rows to investigate section. |
| 10 recipes | Added `## Gotchas` with runtime platform rules + proxy trust config. |
| alpine.md / ubuntu.md | Added when-to-use / when-to-switch sections. |

---

## Part 2: Docs/specs that are now WRONG

### CLAUDE.md

| Line | Says | Should say |
|------|------|-----------|
| 32 | `internal/knowledge (BM25)` | `internal/knowledge (text search)` — search is NOT BM25, it's simple additive text matching |
| 43 | `BM25 search, embedded docs, session-aware briefings` | `Text search, embedded docs, session-aware briefings, runtime-aware mode adaptation` |
| Architecture table | No mention of model.md | Add `themes/model.md` — platform model, injected at discover step |
| Architecture table | References universals.md implicitly | universals.md is DELETED. GetUniversals() reads from model.md |

### docs/spec-bootstrap-deploy.md

| Line | Issue |
|------|-------|
| 40 | Diagram mentions "Knowledge Store (BM25 + recipes + schemas)" — not BM25 |
| 293-294 | "Runtime knowledge (injected automatically) / Recipe knowledge (if loaded in discover)" — now also model.md injected at discover |
| 912-921 | Knowledge Invariants table K1-K6 — needs updating: K3 should note model injection at discover; Rules & Pitfalls no longer exists; universals changed |

### docs/spec-guidance-philosophy.md

| Line | Issue |
|------|-------|
| 108 | "Knowledge store (BM25 + recipes)" — not BM25 |
| 116-132 | Layer diagram — ALREADY UPDATED to reference model.md and Platform Constraints. Verify it matches actual code after v3 changes. |
| 127-128 | Layer 1 says "Core Reference (themes/core.md) — YAML schemas, platform rules" — "platform rules" is wrong now (Rules & Pitfalls removed). Should be "YAML schemas, deploy semantics, examples" |

### docs/recipes/README.md

| Line | Says | Should say |
|------|------|-----------|
| 55 | "GetRecipe prepends only platform universals (themes/universals.md)" | "GetRecipe prepends platform constraints (extracted from themes/model.md)" |
| 127 | "Platform knowledge lives in themes/universals.md" | "Platform constraints extracted from themes/model.md" |
| 159 | "Platform universals prepended (from themes/universals.md)" | "Platform constraints prepended (from themes/model.md ## Platform Constraints)" |

### docs/spec-recipe-quality-process.md

| Line | Issue |
|------|-------|
| 106 | "## Keywords — framework-specific terms for BM25 search" — Keywords are NOT used in search at all (see Part 3). Also not BM25. |
| 124 | "Keywords are for BM25 search disambiguation" — Keywords field is parsed but never read by Search(). Dead feature. |

### docs/zrecipator/recipe-via-zcp-analysis.md

| Line | Issue |
|------|-------|
| 175 | Lists `universals.md` — deleted |
| 213 | References "Rules & Pitfalls" — section removed from core.md |

### README.md

| Line | Says | Should say |
|------|------|-----------|
| 34, 47 | "BM25 search" | "Text search" |

### .claude/agents/zerops-knowledge.md

| Line | Issue |
|------|-------|
| 108 | "internal/knowledge (BM25)" | Not BM25 |

---

## Part 3: Knowledge mechanism issues (not yet fixed)

These are systemic issues discovered during the redesign that affect how the knowledge system works, independent of content. Full details in `plans/analysis-knowledge-mechanisms.md`.

### Issue 1: `## Keywords` sections — parsed but never used (HIGH priority)

- 34 markdown files have `## Keywords` sections (comma-separated terms)
- Parsed into `doc.Keywords []string` field in `documents.go:69`
- `Search()` in `engine.go:109-165` scores by Title (+2.0) and Content (+1.0) — **NEVER checks Keywords field**
- Keywords text STAYS in `doc.Content` — LLM sees "## Keywords\nzerops, platform, ..." as noise
- ~170 lines of wasted context across all documents
- The spec (`spec-recipe-quality-process.md:106`) says Keywords are "for BM25 search" but this is false — they do nothing

### Issue 2: `zerops://` URIs in markdown — 60+ dead cross-references

- Guides and decisions end with "See Also" sections containing `zerops://themes/services` etc.
- These are INTERNAL store keys, not MCP resource URIs (MCP uses `zerops://docs/` prefix)
- LLM cannot fetch these — they're plain text, URI pattern doesn't match MCP resources
- 60+ occurrences across guides/ and decisions/

### Issue 3: MCP Resources registered but unused by LLM clients

- `resources.go` registers `zerops://docs/{+path}` ResourceTemplate
- Claude Code, Codex, Gemini CLI — none practically use MCP resources (all use tools)
- Low cost to maintain but creates URI confusion (docs/ prefix vs internal zerops:// prefix)

### Issue 4: Search scoring is primitive

- `strings.Contains` matching with fixed boost (title 2.0, content 1.0)
- No inverse document frequency — common words match everything
- No phrase matching — "object storage" matches any doc with "object" OR "storage"
- Keywords field not used in scoring despite being parsed
- `queryAliases` help but scoring is too coarse

### Issue 5: `## TL;DR` and `## Keywords` visible in LLM Content

- Both sections remain in `doc.Content` (the field injected into LLM context)
- Could be stripped during parsing — parsed data goes to `doc.TLDR` and `doc.Keywords` fields
- TL;DR is actively used (decision summaries) — field useful, section in Content is noise
- Keywords is dead — both field and section are noise

---

## Part 4: E2E verified platform facts

These facts were verified against the live Zerops API on 2026-04-02 and should be considered authoritative:

| Fact | Verification method | Result |
|------|-------------------|--------|
| Hostname max length | Import with 26-41 char hostnames | **40 chars** (41 rejected). Docs/schema say 25. |
| valkey@8 | Import attempt | **Rejected**: "Service stack Type not found" |
| minContainers on postgresql | Import attempt | **Rejected**: "Invalid parameter provided" |
| maxContainers on postgresql | Import attempt | **Rejected**: "Invalid parameter provided" |
| verticalAutoscaling on shared-storage | Import attempt | **Rejected**: "Mandatory parameter is missing" |
| verticalAutoscaling on object-storage | Import attempt | **Rejected**: "Invalid parameter provided" |
| hostname with hyphens | Import attempt | **Rejected**: "Service stack name is invalid" |
| object-storage without objectStorageSize | Import attempt | **Rejected**: "Mandatory parameter is missing" |
| envVariables at service level | Import + discover | **Silently dropped**: import succeeds, env var NOT created |
| mode: HA on runtime | Import + discover | **Forced to HA**: all runtimes show mode: "HA" regardless of input |
| mode: NON_HA on runtime | Import + discover | **Forced to HA**: same as above |
| No priority on managed | Import | **Accepted**: services created, no ordering guarantee |

---

## Part 5: Current file inventory

### themes/ (4 files)

| File | Lines | Role | Delivery |
|------|-------|------|----------|
| model.md | ~145 | Platform model + Container Lifecycle + Immutable Decisions + Platform Constraints | `GetModel()` at discover step + `scope=infrastructure` + `GetUniversals()` extracts "Platform Constraints" H2 |
| core.md | ~250 | YAML schemas + Schema Rules + Multi-Service Examples | `getCoreSection()` per workflow step + `scope=infrastructure` |
| services.md | 226 | Managed service cards + wiring templates | H2 section extraction in `GetBriefing()` |
| operations.md | 141 | Ops reference (networking, CI/CD, logging, scaling, RBAC, production checklist) | Query search only |

### Other knowledge directories

| Directory | Files | Role | Delivery |
|-----------|-------|------|----------|
| recipes/ | 33 | Per-runtime hello-world + per-framework recipes | `GetRecipe()` + `getRuntimeGuide()` in briefing |
| guides/ | 20 | Detailed operational topic guides | Query search only |
| decisions/ | 5 | Service selection (choose DB/cache/queue/search/runtime base) | `getRelevantDecisions()` in briefing |
| bases/ | 5 | Infrastructure runtime guides (alpine, ubuntu, docker, nginx, static) | `getRuntimeGuide()` fallback in briefing |

### Key code files

| File | Lines | Role |
|------|-------|------|
| `engine.go` | ~300 | Store, Search, Provider interface (GetCore, GetUniversals, GetModel, GetBriefing, GetRecipe) |
| `briefing.go` | ~250 | GetBriefing (7-layer composition), GetRecipe (universals + recipe + mode adaptation) |
| `sections.go` | ~330 | H2/H3 parsing, runtime/service normalization, decisions routing |
| `documents.go` | ~230 | Document parsing, embedding, frontmatter extraction |
| `guidance.go` | ~140 | assembleGuidance() + assembleKnowledge() — per-step knowledge injection |
| `tools/knowledge.go` | ~170 | zerops_knowledge MCP tool handler (4 modes: scope, query, briefing, recipe) |
| `server/resources.go` | 43 | MCP ResourceTemplate registration (zerops://docs/{+path}) |

### Knowledge delivery paths (summary)

1. **System prompt** (`instructions.go`) — NO knowledge content. Operational only.
2. **scope=infrastructure** → model.md + Platform Constraints + core.md + live stacks
3. **briefing(runtime, services)** → runtime guide + recipe hints + service cards + wiring + decisions + version check
4. **recipe=name** → Platform Constraints (from model.md) + recipe content + mode header
5. **query=text** → text search across all ~70 documents
6. **Workflow injection** → per-step: discover gets model.md, provision gets import schema, generate gets runtime+deps+schema+envvars, deploy gets Schema Rules
7. **MCP Resources** → registered but unused by LLM clients
