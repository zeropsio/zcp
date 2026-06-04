# Converge knowledge retrieval onto a single format — remove MCP resource surface

**Date:** 2026-06-04
**Decision (Karel):** B — remove the MCP *resources* protocol surface entirely; the single
canonical retrieval format is the `zerops_knowledge` TOOL invoked with `uri="zerops://<ns>/<path>"`.

## Why (settled, not re-litigated here)
1. **Protocol universality** — MCP `tools` are the lowest common denominator every client implements;
   `resources` are an optional capability codex/grok/antigravity likely don't expose. A surface only
   some agents can reach is a per-client fork, not a single source of truth.
2. **Semantic fit** — ZCP knowledge is ADAPTIVE (mode-adapted recipes, session-aware briefings,
   `{hostname}` substitution in atoms). A static resource GET can't carry it and would leak
   unsubstituted placeholders (the `resolveAtomURI` safety boundary proves this). Resources are the
   wrong shape even on clients that support them.
3. **Dead weight + trap** — the `zerops://docs/{+path}` template is emitted/documented NOWHERE; the
   resource list is empty (only a template, no concrete resources); it half-works only by prefix
   accident. Proven live: `ReadMcpResourceTool zerops://guides/X` → `-32002`; `…docs/guides/X` → body.

## Two coupled changes
**(A) Remove the resource surface.** **(B) Converge every agent-facing bare `zerops://` emission onto
the `zerops_knowledge uri="…"` form** that `synthesize.go:213 referenceStub` already renders.

---

## Removal scope (A) — 3 sites, pure deletion

| File | Action | Note |
|---|---|---|
| `internal/server/resources.go` | DELETE whole file (43 ln) | `registerResources`, `resourceURIPrefix`, the lone `AddResourceTemplate("zerops://docs/{+path}")`, the `docs/`→store-key rewrite. `resourceURIPrefix` has no other repo reference. `s.store` is independently consumed (field init server.go:123) — not orphaned. |
| `internal/server/server.go:133` | DELETE the `s.registerResources()` call | Only call site. **Capabilities:** go-sdk gates `capabilities.resources` on `resourceTemplates.len()>0`; `ServerOptions` (server.go:113-116) never sets `HasResources`, so removing the template auto-un-advertises the capability — no flag to clear. Leave 113-116 untouched. |
| `internal/server/resources_test.go` | DELETE whole file (157 ln) | 4 tests pin the deleted surface; the `testResourceServer` helper is local to this file, strands nothing. |

**Optional hardening (Phase 1):** add a `server_test.go` assertion that the initialize handshake does
NOT advertise `capabilities.resources` — pins the un-advertise so a future `AddResourceTemplate`
re-introduction is caught.

---

## Convergence scope (B)

### B1 — query= search output (the prime misroute vector) — 1 Go site
`internal/tools/knowledge.go:176-179` query= mode returns `jsonResult([]knowledge.SearchResult)` —
each hit marshals a **bare** `{"uri":"zerops://…","title":…,"score":…,"snippet":…}`. The bare `uri`
reads to the agent as a resource URI (exactly what triggered the incident).

**Fix at the TOOLS layer (a view type), NOT the `knowledge.SearchResult` struct** (the struct keeps
`json:"uri"` — internal `Search()` consumers + `engine_search_test.go` read `r.URI`/`r.Snippet`).

**Load-bearing branch (verified):** `store.Search` can return `zerops://recipe-atom/<id>` synonym URIs
(`wire_contract_synonyms.go:50+`). These are **NOT `uri=`-fetchable** — the fetch dispatch
(`knowledge.go:211`) only handles `zerops://atoms/` then `store.Get`, which 404s on `recipe-atom`.
So the fetch hint must branch on the `zerops://recipe-atom/` prefix and emit
`zerops_workflow action=dispatch-brief-atom atomId=<id>` (the redirect the synonym snippet already
carries) instead of a `uri=` form. Naively wrapping every URI in `uri="…"` would create a NEW dead
handle — worse than today.

**DECISION: B-replace** (Codex-confirmed). View type = `fetch` + title/score/snippet, **no bare `uri`**.
Codex repo-searched: no tool-JSON consumer beyond the 2 `knowledge_test.go` decoders; internal
`knowledge.SearchResult` consumers stay untouched. Zero bait, fully Information-Contract-correct.
**atomId derivation (Codex):** for `zerops://recipe-atom/<atomId>`, the atomId IS the URI suffix —
`strings.CutPrefix(uri, "zerops://recipe-atom/")`; the snippet is NOT needed. `dispatch-brief-atom` is
live (routed `workflow.go:356`, fetches atomId `workflow.go:764`). All other search-hit prefixes
(themes/bases/recipes/guides/decisions, `documents.go:14`) are `uri=`-fetchable → `zerops_knowledge uri=`.

### B2 — committed theme markdown — 8 sites (DURABLE in this commit)
**Correction to the analysis:** themes are **TRACKED/committed**, NOT synced (`.gitignore` lists only
recipes/guides/decisions). So these edits land in the code commit, no upstream push.

`internal/knowledge/themes/refinement-references/`:
- `ig_one_mechanism.md:155,217` (217 = sentence-leading, reword)
- `refinement_thresholds.md:42,95,135,203,227,250`

Replace each bare `` `zerops://themes/refinement-references/<name>` `` →
`` `zerops_knowledge uri="zerops://themes/refinement-references/<name>"` `` (preserve qualifiers
like "9.0 anchor"). Read verbatim by the recipe-refinement sub-agent.

### B3 — gitignored guide markdown — 3 sites (UPSTREAM, NOT in this commit)
**Correction:** guides are **gitignored/synced from zeropsio/docs** — edits to them are git-ignored
(can't be committed) and survive to the released binary ONLY via CI's fresh `sync pull`. So these are
an **upstream zeropsio/docs PR** (`zcp sync push guides`), decoupled from the code commit, **Karel-gated**.

`zerops-yaml-advanced.md:9`, `deployment-lifecycle.md:86`, `production-checklist.md:144` — each a bare
`` `zerops://guides/readiness-health-checks` `` cross-ref → tool-call form.

### B4 — lint guard (optional, recommended; single-owner tell==check)
`TestNoBareZeropsURIInAgentContent` — scan agent-facing markdown (knowledge/themes, content/atoms,
content/examples, content/workflows) for a backtick-wrapped bare `zerops://…` NOT preceded by
`zerops_knowledge uri=`; fail with file:line. Mirror `atoms_lint.go` style. **Scope must exclude**
guides (gitignored, upstream-owned) and the 3 Aleš recipe sites until converted — else it red-fails on
content this commit doesn't touch.

---

## leaveAsIs (correctly classified non-changes)
- `knowledge.SearchResult` struct (`engine.go:8-12`) — keeps `json:"uri"`; internal consumers read it.
- `wire_contract_synonyms.go` snippet redirects — already carry `dispatch-brief-atom`; the B1 branch reads the prefix.
- `knowledge.go` jsonschema `uri=` description, recovery hints (122/134/272-273), doc comments, fetch dispatch — already tool-call-scoped or non-agent-facing.
- `synthesize.go:212-215 referenceStub` — the CANONICAL form to converge onto; do not touch.
- Internal store-key literals (`recipe_corpus_store.go`, `recipe_guidance.go`, `cmd/zcp/eval.go`), all `zerops://recipes/*`/`zerops://themes/*` test fixtures.
- `internal/schema` `AddResource` — jsonschema compiler (santhosh-tekuri), UNRELATED to MCP resources.
- `internal/recipe/validators_kb_quality.go:25-52` — validation-set store keys, never agent-surfaced.
- `briefs_refinement.go:100` HEADER (`zerops_knowledge uri=zerops://…/<name>`) — already correct; but `:101-102` CATALOG is bare → moved to the recipe decision above (analysis misclassified the whole block as "leave").
- All client configs (`.mcp.json`, 6 init adapters, `.claude.json`, VS Code ext, AGENTS/CLAUDE bodies) — plain stdio + tool-scope `mcp__zerops__*` wildcard; zero resource refs.

## RECIPE-SCOPE DECISION (Codex 2× BLOCKER — "VSE" reach into Aleš's scope)
Codex caught that deferring these contradicts the stated goal ("agent never sees bare `zerops://`") —
the recipe refinement/design **sub-agents read these briefs**, so a bare URI here is the same bait, just
in the sub-agent's context. All 4 are `internal/recipe/` (Aleš). All conversions are **TRANSPARENT**
(bare URI → tool-call form; same retrieval target, clearer form, no recipe-authoring behavior change).

| Site | Current (bare, agent-facing) | Pin to update |
|---|---|---|
| `briefs_refinement.go:101-102` | catalog of 7 entries `- \`zerops://themes/refinement-references/<name>\`` under a tool-call header | `briefs_rendered_substrate_pin_test.go:498` |
| `refinement_suspects.go:100` | suspect `Reason`: bare `` `zerops://themes/refinement-references/kb_shapes` `` | `briefs_refinement_test.go:88` (strengthen) |
| `briefs_design_tokens.go:29` | "Full spec: `zerops://themes/design-system`" (bare) | `briefs_design_tokens_test.go:46` |
| `content/briefs/feature/tailwind_componentry.md:11` | bare prose URI (line 12 already has the correct form) → fold 11 into 12 | `briefs_feature_pass_test.go:144` |

**Decision (Karel):** (1) include all 4 now (transparent; I update the 3 recipe pins to assert the
tool-call *shape*, not URI substrings; flag Aleš as courtesy) — honors "VSE", removes ALL bait; OR
(2) explicitly narrow the goal to exclude recipe-authoring (Aleš converges separately) — keeps the plan
internally consistent but leaves bare URIs in the recipe sub-agent briefs. **Recommend (1).**

---

## Phases (RED→GREEN, ≤5 files each)
1. **Remove resource surface** — delete 3 sites + (opt) handshake test. Verify: `go test ./internal/server/... && go build ./...`.
2. **B1 query= fetch hint** — RED: `TestKnowledgeTool_Query_EmitsFetchHint` (normal hit `fetch == zerops_knowledge uri="…"`) + `…_SynonymHit_FetchIsDispatch` (recipe-atom hit `fetch == zerops_workflow action=dispatch-brief-atom atomId=…`). GREEN: tools-layer view type. Update the 2 `[]SearchResult` parse tests iff B-replace. Verify: `go test ./internal/tools/... ./internal/knowledge/... ./integration/...`.
3. **B2 theme markdown** — 8 edits. Verify: `grep -rn 'zerops://' internal/knowledge/themes/ | grep -v zerops_knowledge` EMPTY; `go test ./internal/recipe/... -run Refinement`.
4. **B4 lint guard** (opt) — `TestNoBareZeropsURIInAgentContent`, scoped. Negative + positive test.
5. **B3 guides** — upstream zeropsio/docs PR via `sync push guides` (Karel-gated, separate from commit).

## Risks
- **Handshake (wording per Codex NIT):** removing the template makes go-sdk **omit `capabilities.resources` from the initialize result** (verified: `HasResources || resources.len()>0 || resourceTemplates.len()>0`, SDK server.go:587 → all false). Note the SDK does NOT enforce caps on the client side — an ignoring client could still call `resources/list`/`read` and get an empty/not-found, which is the correct tools-only state. No ZCP config drives such a probe. Pin with the Phase-1 test.
- **SearchResult reshape** safe ONLY at the tools layer; mutating the struct breaks `engine_search_test.go`.
- **recipe-atom branch** is load-bearing (else new dead handle). Pin with the synonym test.
- **guides revert** — B3 in-repo edits are git-ignored; durable only via upstream push.
- **lint scope** — must exclude guides + Aleš sites or it red-fails on untouched content.

## IMPLEMENTED (2026-06-04) — per-phase status
- **P1 resource removal** ✅ — `resources.go` + `resources_test.go` deleted, `server.go:133` call removed; `TestServer_DoesNotAdvertiseResourcesCapability` added (handshake omits `capabilities.resources`).
- **P2 query= fetch hint (B-replace)** ✅ — tools-layer `searchHit{fetch,title,score,snippet}` + `fetchDirective` (recipe-atom → `dispatch-brief-atom`); 2 RED tests added, 2 parse tests migrated off bare `uri`.
- **P3 themes (8)** ✅ — `ig_one_mechanism.md` (2) + `refinement_thresholds.md` (6) converged; durable in commit (themes committed).
- **P4 recipe sites (3 of 4)** ✅ — `briefs_refinement.go:102` catalog + `refinement_suspects.go:100` + `tailwind_componentry.md:11` converged; 2 pins updated (substrate catalog → tool-call shape, tailwind anchor → tool-call form). ⚠️ **`briefs_design_tokens.go:29` NOT done — read-denied by permission settings; cannot edit. NEEDS KAREL.**
- **P5 guides (3)** ✅ on disk — `zerops-yaml-advanced/deployment-lifecycle/production-checklist` converged; gitignored → NOT in commit, lands via pre-release `sync push guides`.
- **P6 lint** ✅ — `TestNoBareZeropsURIInAgentContent` (scans git-tracked agent-facing `.md` via the repo_drift pattern; gitignored guides auto-excluded).

**Verification:** `go test ./... -short` EXIT 0; `make lint-local` 0 issues; `-race` on server/tools/knowledge/content/recipe green. Comprehensive grep: only remaining agent-facing bare-URI emission is the blocked `briefs_design_tokens.go:29` (all other `` `zerops://`` hits are Go doc-comments / internal sync transform).

## Backward compat — all transparent
`.mcp.json` (stdio, no caps), permission allowlist (`mcp__zerops__*` is TOOL-scope — a resource removal removes no tool), `.claude.json` (empty arrays), 6 init adapters (stdio only), AGENTS/CLAUDE bodies (name the tool, no bare URI), VS Code ext (no MCP resource touch), handshake bit (re-negotiated every stdio reconnect, no cached on-disk state).

## DECISIONS (Karel, 2026-06-04)
1. **RECIPE SCOPE = include all 4 now.** Convert the 4 recipe-scope sites; update 3 recipe pins to
   tool-call shape; flag Aleš as courtesy. Full convergence — no bare bait anywhere.
2. **B3 guides = edit in-repo now, push upstream PRE-RELEASE** (same flow as PR #347). The 3 guide edits
   land on disk now (build-ready); the `sync push guides` PR happens just before the release, with
   explicit authorization at that point. NOT in the code commit (gitignored).
3. **B4 lint = add it.** `TestNoBareZeropsURIInAgentContent`. Scope: committed agent-facing markdown
   (knowledge/themes, content/atoms, content/examples, content/workflows, recipe/content). EXCLUDE the
   gitignored/synced dirs (guides, recipes, decisions) — guides fold in after the pre-release push merges.

*(Resolved by Codex:* B-replace correct; recipe-atom branch correct (atomId = URI suffix); removal blast
radius complete; baseline `go test ./internal/{server,tools,knowledge} -short` green.)
