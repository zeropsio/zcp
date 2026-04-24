# Run 8 readiness — what ships and why

Run 7 closed 5 phases because the gates were trivial, not because the recipe works. This plan enumerates what changes before run 8 so the next dogfood produces a structurally and content-correct showcase.

Reference material (all already written):
- [spec-content-surfaces.md](../../spec-content-surfaces.md) — the 7 surface contracts + classification taxonomy + citation map + anti-patterns
- `/Users/fxck/www/laravel-showcase-app/` — reference per-codebase deliverable (README + CLAUDE.md + commented `zerops.yaml`)
- `/Users/fxck/www/recipes/laravel-showcase/` — reference recipe-root deliverable (root README + 6 env folders)
- [CHANGELOG.md](../CHANGELOG.md) — everything shipped v9.5.1 → v9.5.6

Run-7 artifacts: [docs/zcprecipator3/runs/7/](../runs/7/)

---

## 1. Goals for run 8

A recipe run that produces a deliverable a porter can click-deploy AND whose content passes every single-question test in `spec-content-surfaces.md`.

**Must land by run 8 start:**

1. Stage cross-deploy per codebase — dev→stage push + stage verify — proves the `prod` setup works, not just `dev`.
2. Browser verification of the scaffolded UI — visual proof `appdev` renders what the features expose.
3. Seed data — porter deploying tier 4/5 sees populated dashboard on first hit, not empty lists.
3a. Port v2's init-commands concept into v3 — two `zsc execOnce` key shapes (per-deploy `${appVersionId}` for idempotent migrations vs static keys for non-idempotent seeds), revision suffix for forced re-run, in-script-guard pitfall. v3 scaffold brief today has 5 lines on this; v2 had full atom coverage. Without it, sub-agents pick the wrong key shape (run 7 skipped seeds entirely). Procedural half — post-deploy data verification + burned-key recovery — ports into scaffold + feature phase_entry atoms.
4. Engine-owned templates with `<!-- #ZEROPS_EXTRACT_START:NAME# -->` markers; fragments come from the in-phase author per the authorship map (§2.0); engine owns structure.
5. Commented `zerops.yaml` per codebase — comments authored by the scaffold sub-agent inline as the yaml is written; feature sub-agent extends when adding stanzas. Stitch copies the committed file verbatim.
6. Sub-agent + main-agent content-authoring briefs — placement rubric (yaml-comment / IG / KB / CLAUDE.md), spec excerpts, tone rules carried at the point of authorship.
7. Recipe-session-aware `zerops_record_fact` + `zerops_workspace_manifest` (deferred from v9.5.5).
8. Classification taxonomy in sub-agent briefs (rubric at decision-moment) AND engine safety net — facts tagged `framework-quirk` / `self-inflicted` / `library-metadata` never reach the stitched output.
9. Finalize gates walking the spec — length bounds, marker presence, citation-map attachment, cross-surface uniqueness.

**Stretch for run 8 (ship if tractable, else run 9):**

10. Tone linters — causality-word check on `zerops.yaml` comments + env `import.yaml` comments; meta-agent-voice regex on env READMEs.
11. Factuality gate — root README framework claim vs scaffolded `package.json`.
12. `verify-subagent-dispatch` SHA check on every guarded Agent dispatch (plan §6 — still not implemented, run 7 showed feature-phase wrapping).

---

## 2. Workstreams

### 0. Guiding principle — who writes what, when

**The agent holding the densest context for a fragment writes that fragment, at the moment the context is densest. Finalize is assembly + validation, not generation.**

Three consequences follow:

1. **No writer sub-agent dispatch.** Run 7's writer reconstructed reasoning from committed files that two earlier agents already had in hand. That reconstruction is the efficiency hole and the quality hole (stale, guessed causality). Fragments are authored in-phase by whoever made the decision.

2. **Three codebase surfaces share one content budget.** `zerops.yaml` comments, IG, and KB aren't independent outputs — they partition the same "why" into stanza-scoped / narrative / absence-or-consequence. They MUST be authored in one pass by one author with a placement rubric, otherwise they duplicate (same "why" in all three) or under-cover (each assumes the reader saw another).

3. **Authorship map — pinned for run 8:**

| Fragment | Author | Authored during |
|---|---|---|
| `root/intro`, root deploy-links + cover | main agent | finalize (research atom + tier list) |
| `env/<N>/intro`, `env/<N>/scaling-note` | main agent | finalize (tier metadata) |
| `env/<N>/import-comments` (per service + env-var block) | main agent | finalize (platform education, not codebase-specific) |
| `codebase/<hostname>/intro` | scaffold sub-agent | scaffold phase |
| `codebase/<hostname>/integration-guide` | scaffold sub-agent; feature sub-agent extends | scaffold phase; feature phase |
| `codebase/<hostname>/knowledge-base` | scaffold sub-agent; feature sub-agent extends | scaffold phase; feature phase |
| `codebase/<hostname>/claude-md/*` | scaffold sub-agent; feature sub-agent extends | scaffold phase; feature phase |
| `zerops.yaml` inline comments | scaffold sub-agent (inline as yaml is written); feature sub-agent extends when adding stanzas | scaffold phase; feature phase |

Scaffold/feature sub-agents author yaml comments **inline in the committed file**, not as separate fragments — matches the reference `laravel-showcase-app/zerops.yaml`. Stitch copies the committed yaml verbatim; engine does not splice comments post-hoc.

---

### A. Content output — engine templates + fragment assembly + deliverable split

**The shift:** engine owns structural templates and runs an assembler. Fragments come from the authorship map above, stored in the recipe plan. No dispatched writer sub-agent.

**Changes:**

1. **`internal/recipe/content/templates/` — new tree of Go text/template files:**
   - `root_readme.md.tmpl` — title, cover, deploy button, tier links, `#ZEROPS_EXTRACT_START:intro#` marker, footer
   - `env_readme.md.tmpl` — title, link back, `#ZEROPS_EXTRACT_START:intro#` marker
   - `codebase_readme.md.tmpl` — title, `#ZEROPS_EXTRACT_START:intro#`, deploy button, cover, "## Integration Guide", `#ZEROPS_EXTRACT_START:integration-guide#`, "### Gotchas", `#ZEROPS_EXTRACT_START:knowledge-base#`
   - `codebase_claude.md.tmpl` — headers for "Zerops service facts", "Zerops dev", "Notes" with marker regions the writer fills
   - `codebase_zerops_yaml.tmpl` — yaml skeleton with `# <@comment:NAME# -->` splice markers at canonical anchor points (one per setup, one per build/run/deploy subtree, one per env-var block)

2. **Template data model** — each template receives `Plan` + (for per-codebase) `Codebase` + (for per-env) `Tier`. Go text/template substitutes structural fields: slug, framework, hostnames, tier suffixes, deploy-button URLs, `zeropsSubdomain` env-var references. No prose in templates; prose goes in fragments.

3. **Plan carries fragments directly — no writer payload.** Fragment slots live on the recipe plan, populated by the owning agent in-phase via a `record-fragment` handler action:
   ```
   Plan.Fragments: {
     "root/intro": string,
     "env/0/intro": string, ... "env/5/intro": string,
     "env/0/import-comments": {<hostname>: string, ...},
     "codebase/<hostname>/intro": string,
     "codebase/<hostname>/integration-guide": string,
     "codebase/<hostname>/knowledge-base": string,
     "codebase/<hostname>/claude-md/<section>": string,
   }
   Plan.Citations: {topic -> guide-id}  // as before
   ```
   No fragment type for `zerops-yaml-comment/*` — scaffold sub-agent writes comments inline in the committed `zerops.yaml` as it authors the file. Stitch copies it verbatim.

4. **New handler action: `record-fragment`** — `zerops_recipe action=record-fragment fragmentId=<id> body=<string>`. Callable by:
   - scaffold sub-agent (via its brief) for `codebase/<hostname>/*`
   - feature sub-agent for extending `codebase/<hostname>/integration-guide`, `knowledge-base`, `claude-md/*`
   - main agent in finalize for `root/*`, `env/<N>/*`
   
   Append-on-extend semantics for IG/KB/CLAUDE.md (feature sub-agent extends scaffold's body). Overwrite for root/env (main agent writes once in finalize). Engine validates fragmentId is in the declared schema per-surface.

5. **`stitch-content` becomes an assembler:**
   - Walk template tree for each canonical surface
   - Render template with plan's structural data
   - Substitute each `#ZEROPS_EXTRACT_START:NAME#` marker with `Plan.Fragments[<marker>]`
   - For per-codebase `zerops.yaml`, copy the committed file from the codebase's working tree (already comment-annotated by scaffold sub-agent)
   - Write to canonical paths in the two-root split:
     - `<outputRoot>/` (recipes-repo shape): root README + `<tier>/README.md` + `<tier>/import.yaml`
     - `<outputRoot>/codebases/<hostname>/` (apps-repo shape): README, CLAUDE.md, `zerops.yaml` (copied, not spliced)
   - Missing fragment → gate failure, not silent empty. Validators run after substitution.

6. **No writer sub-agent; no writer brief; no writer `completion_payload`.** `internal/recipe/content/briefs/writer/*` deleted. Finalize atom tells main agent to (a) call `record-fragment` for every root/env fragment, (b) call `stitch-content`, (c) act on any validator failures by editing the offending fragment and re-stitching. Scaffold/feature fragment authoring is covered in Workstream F.

**Files touched:**
- New: `internal/recipe/content/templates/*.tmpl`
- New: `internal/recipe/assemble.go` — template rendering + marker substitution + validator invocation
- Modified: `internal/recipe/handlers.go` — `record-fragment` action; `stitch-content` delegates to `assemble.go`
- Modified: `internal/recipe/workflow.go` — `Plan.Fragments` field; `record-fragment` merges into plan
- Modified: `internal/recipe/surfaces.go` — `SurfaceContract` adds template path + expected fragment ids + author role
- Deleted: `internal/recipe/content/briefs/writer/` — no writer dispatch

**Test coverage:**
- `TestAssemble_TemplateRendersStructuralData` — fragments missing, structure still lands from plan (title, deploy button, tier links)
- `TestAssemble_FragmentSubstitution` — marker block content swapped to plan fragment body
- `TestAssemble_MissingFragmentFailsGate` — fragment-id schema gate catches missing fragments
- `TestAssemble_CopiesCommittedYaml` — per-codebase `zerops.yaml` copied verbatim (including inline comments) from working tree
- `TestAssemble_DeliverableSplit` — files land in both outputRoot/ and outputRoot/codebases/<hostname>/
- `TestHandler_RecordFragment_AppendVsOverwrite` — IG/KB/CLAUDE.md append on repeat; root/env overwrite

### B. Phase atom completeness — stage + browser + seed + init-commands verification

**Changes:**

1. **Scaffold atom** (`phase_entry/scaffold.md`): after each codebase's dev-deploy + verify green, MUST cross-deploy dev→stage and verify stage hostname green before recording "scaffold complete." Add explicit step:
   ```
   zerops_deploy sourceService=<hostname>dev targetService=<hostname>stage
   zerops_verify targetService=<hostname>stage
   ```

2. **Feature atom** (`phase_entry/feature.md`): adds seed step alongside migrate — `zsc execOnce <slug>-seed-v1 --retryUntilSuccessful -- node dist/seed.js`. Seed populates minimum data so the UI shows something on first click-deploy. Clarify seed idempotency (`INSERT ... ON CONFLICT DO NOTHING` OR static-key re-run discipline — see B.6).

3. **Feature atom**: adds agent-browser verification step after feature dev-deploy green — navigate to `appdev` URL, exercise each feature tab, capture console errors. Memory `project_browser_verification.md` calls this out explicitly.

4. **Feature atom**: cross-deploy dev→stage per codebase at feature complete-phase; verify stage hostnames.

5. **Finalize atom**: makes content-surface contracts concrete — reference spec-content-surfaces.md's per-surface test question per fragment id, tell main agent to fill one fragment at a time via `record-fragment`, cite its guide from the citation map.

6. **Port v2's init-commands procedural atom** into scaffold + feature phase_entry atoms — ported from [`internal/content/workflows/recipe/phases/deploy/init-commands.md`](../../../internal/content/workflows/recipe/phases/deploy/init-commands.md). After every dev-deploy that has `initCommands`:
   - Read runtime logs (`zerops_logs serviceHostname=<hostname>dev limit=200 severity=INFO since=10m`); confirm framework-specific success lines (applied-migration rows, "N articles seeded", "Meilisearch: indexed N documents").
   - Post-deploy data check — query DB / search index / cache. Do NOT infer "initCommands ran" from "deploy ACTIVE" alone; a prior failed deploy can burn the execOnce key silently.
   - Burned-key recovery: touch any source file → `zerops_deploy` → the fresh deploy-version re-fires per-deploy keys. Hand-run + redeploy only when recovery path 1 is not available.

**Wrapper discipline refinement:** atom explicitly separates what the main agent decides (resource name, endpoint shape, codebase-for-each-feature) from what the sub-agent discovers (libraries, config shape, packages). Don't pre-chew library choices in the dispatch wrapper — the sub-agent consults `zerops_knowledge` and decides.

**Files touched:**
- `internal/recipe/content/phase_entry/scaffold.md`
- `internal/recipe/content/phase_entry/feature.md`
- `internal/recipe/content/phase_entry/finalize.md`
- New: `internal/recipe/content/principles/init-commands-model.md` — conceptual half (two key shapes + revision suffix + in-script-guard pitfall); cited by scaffold + feature briefs in Workstream F. See F for authoring; this workstream just references it from the atoms.

### C. Fact classification pre-routing (writer sees clean input only)

**Current:** writer receives all facts from scaffold + feature phases. Writer decides classification per spec, which is error-prone (run 7 had 4 of 8 facts that should have been discarded but might have shipped).

**Target:** `build-brief briefKind=writer` walks the facts log, tags each fact with one of {`platform-invariant`, `intersection`, `framework-quirk`, `library-metadata`, `scaffold-decision`, `operational`, `self-inflicted`} per spec-content-surfaces.md taxonomy. Tagging logic:
- `surfaceHint: platform-trap` with mechanism that names a Zerops mechanism and citation present → `platform-invariant` OR `intersection`
- `surfaceHint: framework-quirk` or mechanism names only framework API → `framework-quirk` → DROP
- `surfaceHint: scaffold-decision` → routes to `zerops.yaml` comments or IG per fact's `RouteTo`
- `surfaceHint: operational` → routes to CLAUDE.md
- `surfaceHint: self-inflicted` or `tooling-metadata` → DROP

Discarded facts never enter the writer's brief. Writer brief payload lists only the survivors grouped by target surface + guide-id auto-attached from citation map.

**Files touched:**
- New: `internal/recipe/classify.go` — spec taxonomy as Go rules (≤ 150 LoC — it's a decision tree + citation map lookup, no prose)
- Modified: `internal/recipe/briefs.go::BuildWriterBrief` — pre-filter facts
- Modified: `internal/recipe/content/briefs/writer/examples/*.md` — reflect that facts arrive pre-classified + citation-attached

### D. Spec validators — finalize gates that walk the spec

**Current:** finalize `complete-phase` runs `DefaultGates()` + `FinalizeGates()` which only checks env-imports-present + structural schema. Prose content never validated.

**Target:** each `SurfaceContract.ValidateFn` implements its spec-declared rules, walking the stitched output (not the payload). Concrete rules:

| Surface | Validator |
|---|---|
| Root README | length 20-50 lines; contains `#ZEROPS_EXTRACT_START:intro#`; exactly 6 deploy-button links (one per tier); factuality — any framework name in body must appear in a codebase's `package.json` (or composer.json / requirements.txt / etc.) |
| Env README | length 40-120 lines; contains `intro` marker; body does NOT contain the word "agent" when audience is porter-facing (meta-voice lint); tier promotion verb present ("promote", "outgrow", "upgrade", "from tier N", "to tier M") |
| Env import.yaml comments | every service block has comment; comment contains a causal word (`because`, `so that`, `otherwise`, `required for`, `trade-off`, `—`); anti-pattern — first sentence across runtime-service blocks is not identical (templated-opening check) |
| Codebase IG | has `integration-guide` marker; ≥ 2 numbered items; first item is "Adding `zerops.yaml`"; no IG item body contains the recipe's scaffold-only filenames (e.g. `migrate.ts`, `main.ts`, helper file names) |
| Codebase KB | has `knowledge-base` marker; every bullet starts with a bold symptom (`**...**`); every bullet with topic in citation map includes guide-id reference |
| Codebase CLAUDE.md | ≥ 1200 bytes; ≥ 2 custom sections beyond template |
| Codebase zerops.yaml | every scaffold-authored comment contains a causal word (why-not-what check); no comment narrates what the field does in isolation |

**Cross-surface uniqueness** — each fact's `Topic` appears in exactly one stitched surface body (or as a cross-reference link `See: …`).

**Files touched:**
- New: `internal/recipe/validators/*.go` — one file per surface's ValidateFn, wired via RegisterValidator at init
- Modified: `internal/recipe/gates.go` — `FinalizeGates()` also runs RegisteredValidators over every written surface
- Modified: `internal/recipe/handlers.go` — finalize `complete-phase` reads stitched output and passes to validators

**Test coverage:**
- `TestValidator_RootREADME_FactualityMismatch` — README claims React but package.json lists svelte → fail
- `TestValidator_EnvREADME_MetaAgentVoice` — body contains "agent mounts SSHFS" → fail
- `TestValidator_ImportComments_TemplatedOpening` — all runtime blocks start with identical sentence → fail
- `TestValidator_KB_CitationRequired` — gotcha names `object-storage` topic but no `object-storage` guide id → fail
- `TestValidator_CrossSurfaceUniqueness` — same topic appears in two surfaces → fail

### E. Deferred gate plumbing (v9.5.5 follow-through)

**Current:** `zerops_record_fact` + `zerops_workspace_manifest` still gate on v2's `engine.SessionID()` (internal/tools/record_fact.go:74, workspace_manifest.go:50). Run 7 worked for `workspace_manifest` by accident or because the user had a v2 workflow open — not engine guarantee.

**Target:** same `RecipeSessionProbe` pattern from v9.5.5 applied to both tools. When recipe session is active, tool routes to `recipe.Store.RecordFact(slug, ...)` (new method) or surfaces plan+facts directly.

**Files touched:**
- Modified: `internal/tools/record_fact.go` — accept `RecipeSessionProbe`; route fact to `recipe.Store.CurrentSession().RecordFact` when recipe-only context
- Modified: `internal/tools/workspace_manifest.go` — accept `RecipeSessionProbe`; emit recipe-flavored manifest when in recipe session
- Modified: `internal/recipe/handlers.go` — `Store` gains `CurrentSingleSession()` (or equivalent) so sub-agent calls resolve to the open recipe without needing to know the slug
- Modified: `internal/server/server.go` — plumb `recipeStore` to RegisterRecordFact + RegisterWorkspaceManifest

### F. Sub-agent fragment-authoring briefs — place the guidance where the authoring happens

**Why this workstream exists:** Workstream A removed the writer sub-agent and pinned fragment ownership to whoever holds the densest context. That only works if each authoring agent receives, in its brief, the guidance it would otherwise lack. The scaffold sub-agent can't write a coherent IG + KB + `zerops.yaml` comments set without: a placement rubric (so the same "why" doesn't appear in all three), spec-content-surfaces.md contract excerpts for each fragment it owns, tone-of-voice rules, and typical-length bounds. Same for feature sub-agent extensions. Same for main agent's platform-narrative fills in finalize.

**Placement rubric (embedded in scaffold + feature briefs, verbatim):**

| Content type | Lands in |
|---|---|
| Comment about a stanza that IS in the `zerops.yaml` file | `zerops.yaml` inline comment above the stanza |
| Narrative framing of build/run/deploy topology; walks the reader through the setup | `integration-guide` fragment (may embed the commented yaml inline as a code block) |
| Absence (why something is NOT in the yaml) | `knowledge-base` fragment |
| Alternative considered and rejected | `knowledge-base` fragment |
| Non-obvious consequence of a yaml choice, not visible from reading the file | `knowledge-base` fragment |
| Constraint an AI agent editing this repo must know | `claude-md/notes` fragment |
| Service fact (port, hostname, base image, siblings) | `claude-md/service-facts` fragment |

Rubric is enforced by: (a) the brief tells the sub-agent to produce IG + KB + CLAUDE.md + inline-yaml-comments in one pass from this rubric; (b) validators (Workstream D) check cross-surface uniqueness — a "why" that appears in two surface bodies fails.

**Scaffold sub-agent brief changes:**

1. **New "Content authoring" section** in `briefs/scaffold/` — explains:
   - The sub-agent authors 4 outputs in this phase: the `zerops.yaml` file (with inline comments), and 3 fragment record-fragment calls (IG, KB, CLAUDE.md notes/facts)
   - Placement rubric (table above)
   - Tone of voice: causal ("because", "so that", "otherwise", "trade-off"), not descriptive; why, not what; short (2-3 lines per comment, 3-5 bullets per KB, 2-4 items per IG)
   - Spec excerpts per fragment owned (copied from `spec-content-surfaces.md`):
     - IG: surface contract + test question + anti-patterns
     - KB: surface contract + symptom-bold pattern + citation map + anti-patterns
     - CLAUDE.md: surface contract + intended audience
     - zerops.yaml comments: why-not-what rule + causal-word requirement + no templated-opening
   - Worked example (short) from `laravel-showcase-app`: "we chose `php-nginx@8.4` because... → inline yaml comment" vs. "`predis` over `phpredis` because php-nginx lacks the extension → KB entry (no phpredis line to comment on)"

2. **Record-fragment call list** added to the scaffold brief's completion payload section:
   ```
   record-fragment fragmentId=codebase/<hostname>/intro
   record-fragment fragmentId=codebase/<hostname>/integration-guide
   record-fragment fragmentId=codebase/<hostname>/knowledge-base
   record-fragment fragmentId=codebase/<hostname>/claude-md/service-facts
   record-fragment fragmentId=codebase/<hostname>/claude-md/notes
   ```
   Plus the expectation that `zerops.yaml` committed in the codebase already carries inline comments.

3. **Brief cap discipline** — the new Content section must fit within the 3 KB scaffold-brief cap. Spec excerpts get compressed to 2-3 lines each per fragment owned; placement rubric is the source, not full spec prose.

**Feature sub-agent brief changes:**

1. **"Content extension" section** — explains:
   - Its additions extend scaffold's fragments, not replace them
   - record-fragment append semantics for IG/KB/CLAUDE.md
   - When a feature adds a dep, env var, or command: extend `zerops.yaml` (commit a stanza with inline comment) AND add KB bullet if it's a non-obvious consequence AND append to IG if it changes topology
   - Placement rubric identical to scaffold (copied by reference, not duplicated in-brief)
   - Typical-length: 1-2 KB bullets per feature, 0-1 IG item per feature (features are code, IG is topology — most features don't change topology)

2. **Record-fragment call pattern** — fragmentIds use the same ids as scaffold; append semantics take care of concatenation.

**Main agent finalize atom changes:**

1. **New "Fragment authoring" section** in `phase_entry/finalize.md` — explains:
   - Main agent authors 3 fragment groups: `root/*`, `env/<N>/*`, `env/<N>/import-comments` (per-service)
   - Placement rubric is simpler — all main-agent content is platform-narrative for porters, never codebase-specific implementation detail
   - Spec excerpts per fragment owned — root README contract, env README contract (tier audience + promotion vocabulary + meta-agent-voice prohibition), env import.yaml contract (causal word, no templated opening)
   - Author sequence: one record-fragment per slot, then `stitch-content`, then read validator output and iterate on failures

2. **Citation map attachment** — when a fragment cites a topic present in the citation map, main agent includes the guide-id reference. This becomes a validator pass (Workstream D).

**Classification rubric sourcing:** Workstream C's taxonomy is ALSO carried in the scaffold + feature briefs, not just the (now-deleted) writer brief. At decision-moment, the sub-agent decides whether a fact about what it just did is `platform-invariant` / `intersection` / DISCARD-class, and routes the fragment accordingly. Engine-side classification (C) becomes a final safety net that checks the author didn't misclassify.

**Port v2's init-commands concept atom** — new principle file alongside `env-var-model.md`, ported from [`internal/content/workflows/recipe/phases/generate/zerops-yaml/seed-execonce-keys.md`](../../../internal/content/workflows/recipe/phases/generate/zerops-yaml/seed-execonce-keys.md). Covers:
- Two key shapes: `${appVersionId}` (per-deploy, re-converges every deploy — idempotent-by-design migrations, additive schema work) vs static string like `bootstrap-seed-r01` (once per service lifetime — seeds, scout:import, initial S3 objects, bootstrap ops).
- Revision suffix pattern — bump `r01` → `r02` to force re-run when the seed data itself changes (not when code around it changes).
- In-script-guard pitfall — `if (count > 0) return` combined with per-deploy key skips idempotent sibling work (search-index creation, cache warmup) inside the guarded branch; DB populated + index empty → silent 500s on later searches. Pick the key shape that matches the operation's lifetime; the guard is not the right lever.
- Decomposition rule — when one command does several non-idempotent things, either gate all on one static key OR decompose into separate `initCommands`; don't mix lifetimes.

Cited by:
- Scaffold brief's `platform_principles.md` §Migrations — replaces the current 5-line stub with a 2-line reference + the concept atom appended when the brief composer sees `surfaceHint: migrations` on any fact.
- Feature brief's `content_extension.md` — when the feature adds a seed, scout:import, or any `initCommand`, brief tells sub-agent to consult `init-commands-model` before choosing the key shape.
- Writer-surface citation map gains `init-commands` topic so KB gotchas about key-shape choice carry the guide-id.

Brief cap discipline: concept atom target ~1 KB so it can be injected into both briefs without blowing the scaffold 3 KB / feature 4 KB cap. Content-authoring rubric + init-commands-model combined stay under budget by pointing out references (not inlining full prose).

**Files touched:**
- New: `internal/recipe/content/principles/init-commands-model.md` — ported v2 concept (target ~1 KB)
- New: `internal/recipe/content/briefs/scaffold/content_authoring.md` — placement rubric + spec excerpts + tone + examples (target ~800 bytes)
- New: `internal/recipe/content/briefs/feature/content_extension.md` — append semantics + short rubric ref (target ~500 bytes)
- Modified: `internal/recipe/content/briefs/scaffold/platform_principles.md` — §Migrations replaced by 2-line reference to `init-commands-model.md`
- Modified: `internal/recipe/content/phase_entry/scaffold.md` — point sub-agent at content_authoring.md
- Modified: `internal/recipe/content/phase_entry/feature.md` — point sub-agent at content_extension.md
- Modified: `internal/recipe/content/phase_entry/finalize.md` — main-agent fragment-authoring walkthrough + record-fragment action list
- Modified: `internal/recipe/briefs.go::BuildScaffoldBrief` / `BuildFeatureBrief` — include the new content sections; inject `init-commands-model.md` when facts carry `surfaceHint: migrations` or when scaffold tier declares any `initCommands`
- Modified: `internal/recipe/content/briefs/writer/citation_topics.md` → migrates to engine-side citation map (since writer deleted); add `init-commands` topic pointing at zerops_knowledge guide id
- Deleted: `internal/recipe/content/briefs/writer/` — no more writer dispatch (also covered in A)

**Test coverage:**
- `TestBrief_Scaffold_IncludesContentRubric` — brief bytes contain placement rubric anchors + all 4 fragment-owned ids
- `TestBrief_Scaffold_IncludesInitCommandsModel` — when any scaffolded codebase has `initCommands`, brief injects the concept atom
- `TestBrief_Scaffold_UnderCap` — brief stays under 3 KB after adding content section + init-commands-model
- `TestBrief_Feature_AppendSemantics` — brief tells sub-agent to extend, not overwrite
- `TestBrief_Feature_SeedInjectsInitCommandsModel` — when feature plan declares a seed or scout:import step, feature brief injects the concept atom
- `TestPhaseEntry_Finalize_ListsRootEnvFragments` — atom enumerates every root/* and env/<N>/* fragment id main agent must produce
- `TestInitCommandsModel_TopicsListed` — concept atom mentions both key shapes, revision suffix, in-script-guard pitfall, decomposition rule (content contract — not just "exists")

---

## 3. Ordering + commits

Dependencies:
- A (templates + fragment assembly + record-fragment action) is foundational for F (briefs that tell authors which fragmentIds to record), C (output classification routes to fragmentIds), D (validators walk assembled output)
- E is independent, small, one commit
- B depends on F for the "sub-agent authors fragments" step wording in the atoms AND for `init-commands-model.md` (B's post-deploy verification + burned-key recovery references the concept atom F ships)
- F depends on A (needs record-fragment action defined)

**Commit order:**

1. **E — deferred gate plumbing** (1 commit, ≤ 100 LoC + tests). Isolated change, unblocks sub-agent `record-fact` + `workspace-manifest` under recipe session.

2. **A1 — templates + plan-fragment schema + assembler + record-fragment action** (1 commit, ≤ 600 LoC). Core refactor; deletes writer brief tree.

3. **A2 — two-root deliverable split + committed-yaml copy** (1 commit, ≤ 250 LoC). On top of A1.

4. **F — sub-agent + main-agent content-authoring briefs + init-commands concept port** (1 commit, content-only — new brief md files, `init-commands-model.md` principle, phase_entry edits, brief-composer wiring, brief-cap tests). Ships the concept atom so B can reference it.

5. **B — phase atom completeness (stage cross-deploy + seed + browser + init-commands verification + wrapper discipline)** (1 commit, atoms-only). Depends on F because the scaffold + feature phase_entry atoms reference `init-commands-model.md` which F ships. Merges cleanly with F because F already touched the same phase_entry files.

6. **C — classification pre-routing** (1 commit, ≤ 200 LoC + taxonomy tests). Taxonomy used by both F (rubric in briefs) and engine safety net here.

7. **D — spec validators** (1-2 commits, ~300 LoC per surface validator file + tests).

Between commits: `go test ./... -count=1 -short` green + `make lint-local` green.

Final milestone commit: update `docs/zcprecipator3/CHANGELOG.md` with the run-8-readiness story.

---

## 4. Acceptance criteria for run 8 green

Run 8 is "first truly deliverable finish" when:

1. **Stage deploys green** — every codebase has a passing `zerops_verify` on its stage hostname, not just dev.

2. **Browser verification recorded** — agent-browser step executed against `appdev` URL; screenshots or console-log capture present in facts log.

3. **Seed script ran once, data visible** — `GET /items` returns ≥ 3 items before the agent does any manual create.

4. **Stitched output has canonical structure:**
   - `README.md` at output root has deploy button + 6 tier links + cover + `#ZEROPS_EXTRACT_START:intro#` content
   - Each `<tier>/README.md` has `intro` marker + tier audience fragment
   - Each `codebases/<hostname>/README.md` has `intro` + `integration-guide` + `knowledge-base` markers all populated
   - Each `codebases/<hostname>/zerops.yaml` (copied from committed file) has ≥ 5 comment blocks, each with a causal word
   - Each `codebases/<hostname>/CLAUDE.md` ≥ 1200 bytes + ≥ 2 custom sections

5. **Factuality lint passes** — no README claims a framework absent from any codebase's manifest.

6. **Fragments authored in-phase, not in a finalize writer pass** — facts log shows `record-fragment` calls by scaffold sub-agent at scaffold-completion, feature sub-agent at feature-completion, main agent at finalize. No writer sub-agent dispatch. Classification taxonomy counts surfaced via `status` action.

7. **Citation map attachment** — every KB gotcha with a citation-map topic carries its guide-id.

8. **Cross-surface uniqueness** — no topic keyword appears in > 1 surface body.

9. **Finalize gates all pass** — validator output shows pass on every ValidateFn, not just structural.

10. **Recipe is click-deployable end-to-end** — the `<outputRoot>/4 — Small Production/import.yaml` successfully imports into a fresh project (validated by hand at end of run 8 if not automated).

---

## 5. Non-goals for run 8

Keep out of scope, ship separately or deferred:

- **`verify-subagent-dispatch`** — plan §6 calls for it, v3 never implemented it, run 7 showed feature-phase wrapping. Real issue but it's a dispatch-integrity concern, not a content-quality one. Ship after run 8 confirms the content pipeline works.
- **Chain-resolution delta yaml emission** — plan §7 "showcase diffs against minimal's import.yaml". Current emitter emits full yaml per tier. Defer until nestjs-minimal gets re-run via v3.
- **`requireAdoption` gate fix for recipe-provisioned services** — noted in v9.5.5 CHANGELOG. Didn't bite run 6/7 because fresh state dir. Defer until it does.
- **Automated click-deploy verification** — run-8 acceptance check 10 is manual. Automating it (spin a test project, import tier 4 yaml, verify green) is worth doing but separate harness work.
- **LLM-based editorial-review** — the run-analysis loop. Separate from in-engine gates. Out of scope.

---

## 6. Risks + watches

- **Template caps** — the templates MUST stay small (structural only, no prose). If a template grows past ~2 KB it's carrying prose it shouldn't. Fragment authors own prose inside markers; engine templates own layout.
- **Classification ambiguity** — some facts genuinely straddle classifications (a NestJS worker's `createMicroservice` choice is framework-specific, but the Zerops reason — "separate process for NATS consumer" — is platform-driven). The taxonomy rules need to handle these deterministically, not require judgement. Sub-agent applies rubric in-phase; engine (Workstream C) is the safety net.
- **Validator false positives** — causality-word check might reject valid "Why: <explanation>" comments that happen to lead with a causal phrase that isn't in the allowlist. Keep the allowlist generous + log validator failures so we can iterate.
- **Scaffold brief cap pressure** — Workstream F's content_authoring section competes with existing brief bytes for the 3 KB cap. The scaffold brief was already near cap after v9.5.6's `zerops_knowledge` guidance addition. Mitigation: compress platform_principles content where F's rubric covers the same ground; keep spec excerpts to 2-3 lines per fragment owned.
- **Context loss between scaffold and feature phases** — feature sub-agent extends fragments that scaffold wrote. It receives the current fragment bodies via its brief so it extends coherently, not blindly. Brief payload must include current `Plan.Fragments[codebase/<hostname>/*]` for the feature's codebase. Watch the feature-brief 4 KB cap.
- **Atom cap pressure on scaffold + feature + finalize** — B adds steps. Watch each atom's rendered size against the 3 KB phase-entry guidance cap.
- **Validator failure → main-agent edit loop could churn** — if a validator rejects a fragment, main agent calls `record-fragment` again with a fix and re-stitches. If the failure root cause is a scaffold/feature sub-agent fragment (not a main-agent fragment), main agent has to either (a) edit the sub-agent's fragment in place — which bypasses the decision-moment context — or (b) re-dispatch the sub-agent. Decide during D: allow main-agent edits to codebase fragments, or gate them behind a "source agent role" check. Leaning (a) with a validator-passed audit note, because re-dispatch defeats the efficiency gain.

---

## 7. Open questions

1. **Template format** — Go text/template vs a simpler string-replace on `{{slug}}` / `{{hostname}}` tokens? text/template is more expressive but couples templates to Go parsing errors. String-replace is robust but limited. Commission A chose neither; decide at A1.

2. **Marker naming** — reference deliverable uses `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->`. Stick with that (matches `zcp sync push recipes` extractor). Don't invent our own.

3. **Seed script location + shape** — does the recipe author's seed live in `src/seed.ts` (consistent with migrate), or is there a convention `src/init/seed.ts`? Pick during B.

4. **Browser verification artifact** — where does the screenshot / console capture live in the facts log? New fact type or a manifest field? Decide during B.

5. **Factuality lint depth** — match README framework names against just `package.json` (Node/frontend), or across all codebases' manifests (composer.json, requirements.txt, Cargo.toml, go.mod)? Start Node-only, generalize later.

6. **Cross-surface-uniqueness exact-match vs topic-keyword** — exact string match will miss paraphrases; topic-keyword match requires a topic tagger. Start with the fact's own `Topic` field as the key, enforce exactness on that. Loose prose overlap is post-hoc editorial review, not engine gate.

7. **Committed-yaml-comment authorship** — scaffold sub-agent writes inline comments in `zerops.yaml` at creation. If a feature adds a stanza, it must add the inline comment at the same time, or the comment never gets written. Open question: should validator D's "every stanza has a comment" check run against the committed file OR only against stanzas the scaffold sub-agent authored? Decide during F — likely the former, because that's the deliverable porters read. Feature sub-agent brief must enforce "add stanza ⇒ add comment."

8. **Fragment storage format** — `Plan.Fragments` as a flat `map[string]string` vs a typed struct tree. Flat map is simpler and matches the fragment-id keying used by `record-fragment`. Typed struct gives compile-time guarantees on which ids exist per surface. Start flat + validate ids against `SurfaceContract.ExpectedFragments`; reconsider if the validator layer gets too loose.

9. **Main-agent fragment edit vs sub-agent re-dispatch on validator failure** — see risk 7 above (main-agent edits allowed, audited in facts log). Revisit if run 8 shows main-agent edits corrupting codebase-specific causality that only the sub-agent could know.
