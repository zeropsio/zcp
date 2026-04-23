# zcprecipator3 — plan (clean slate)

**Status**: planning. Lives alongside `internal/workflow/` (zcprecipator2, frozen at v8.113.0). Target Go package: `internal/recipe/`. First tag: `v9.0.0` (major, signals architectural break from 8.x).

**Why a v3 exists**: zcprecipator2 was itself a rewrite born of exactly this pattern — by [v34](../zcprecipator2/README.md) the system had accreted 3,438 lines of `recipe.md`, 60+ mixed-audience `<block>` regions, version anchors threaded through sub-agent briefs, and still shipped recipes with fabricated content + failing convergence. v2's thesis was "thirty-four versions of accumulated knowledge are enough to design the system cleanly from scratch." v2 got the stays/rewrites boundary right on paper ([v2 README §1](../zcprecipator2/README.md)) — operational substrate stays, guidance + briefs + checks get rewritten. Then across v35-v39 it re-accreted: atom corpus, classification tables, example banks, hardcoded Go prose, NestJS-specific preship examples inside recipe.md. Same shape, one layer up.

The pattern is clear: every time the agent produces garbage, the reflex is "give it more hints." Hints become hardcodes. Hardcodes become the next recipe.md. v3's job is not another rewrite — it's **holding the boundary** v2 drew but didn't defend. The stays-list from v2's README is authoritative; this document re-states it, names the output formula that makes the "rewrites" column small, and declares byte budgets that make re-accretion mechanically impossible.

**Prerequisites**: read [zcprecipator2 README](../zcprecipator2/README.md) §0-2 + [spec-content-surfaces.md](../spec-content-surfaces.md) once. v2's README is where the architectural decisions were last articulated correctly; v3 does not replace those decisions, it enforces them.

---

## 1. Context — what we're doing and why

### The product

Recipes are Zerops's marketing surface — a porter lands on `zerops.io/recipes`, sees their framework, clicks deploy, gets a working app on Zerops, reads the per-codebase README to understand how to adapt their own app to run the same way. A recipe is simultaneously:

- **A deployable artifact** (an import.yaml that provisions services + per-codebase zerops.yaml that deploys code)
- **Framework knowledge capture** (per-codebase README + gotchas + CLAUDE.md — what porters need to know that they can't infer from platform docs)
- **A tier progression lesson** (6 environment tiers from AI-agent dev to HA-prod showing how to scale this framework on Zerops)

The AI agent's job is to **produce** this artifact for a framework it knows, using Zerops contracts it doesn't yet know, recording what it learns so the porter inherits the learning.

### The recipe chain (scope grows by service breadth, not by tier)

Recipe slugs come in two categories and a fixed chain:

- **Language-level**: `hello-world-{language}` (e.g. `hello-world-bun`, `hello-world-php`). No framework — trivial endpoint proving the Zerops platform contract holds for a runtime. No parent. These exist to prove platform+runtime basics (bind, subdomain, `zerops.yaml` shape, `initCommands`) without any framework variance.
- **Framework-level**: `{framework}-minimal` and `{framework}-showcase` (e.g. `nestjs-minimal`, `nestjs-showcase`, `laravel-minimal`). Framework-level recipes start at `minimal`. There is no `nestjs-hello-world` or `laravel-hello-world` — hello-world is strictly language-level.

Chain resolution is deterministic:
- `{framework}-showcase` → parent is `{framework}-minimal`
- `{framework}-minimal` → no parent
- `hello-world-{language}` → no parent

No three-hop chains. No cross-framework chains. `chain.Resolve()` is effectively a 1-case-per-slug-kind lookup.

All three recipe kinds deploy to the full 6-env ladder (AI-agent → HA-prod). The 6-env ladder is a Zerops platform feature that applies **universally to every recipe**. Hello-world-bun produces 6 envs. nestjs-minimal produces 6 envs. nestjs-showcase produces 6 envs. HA-tier behavior, rolling deploys, `minContainers: 2`, `mode: HA`, `cpuMode: DEDICATED` are platform features available to every recipe at tier 4/5 — not showcase-exclusive discovery.

What grows between recipe kinds is **service set and feature surface**:

- **hello-world-{lang}**: runtime + one endpoint. No managed services. Single codebase. Single feature (status).
- **{framework}-minimal**: framework + one DB. Adds init-commands, migrations, seed, basic `zsc execOnce` patterns for ONE managed service. Typically 1 codebase (may be 2 if framework is API-first and wants a thin dashboard).
- **{framework}-showcase**: framework + DB + cache + broker + storage + search + full feature suite. Codebase count and split is a research-phase decision (see below).

**Showcase codebase-shape decision (research phase)**

Showcase is the only tier where codebase shape is non-trivial. The research phase decides (and records to `Plan.Research`):

| Framework category | Codebase shape |
|---|---|
| **Full-stack monolith** (Laravel, Rails, Django, Next.js with API routes, SvelteKit with server hooks) | 1 codebase. Worker shares codebase (if worker is needed at all — typically via framework's queue tooling). |
| **API-first with thin dashboard** (NestJS, Express, FastAPI, Flask, Spring, Phoenix, Gin) | 2-3 codebases. api + app (separate frontend SPA). Worker: separate codebase if framework favors a long-lived process model (NestJS `createApplicationContext`), shared codebase if framework's queue library is designed to run in the same process as api. |
| **Pure frontend** (Svelte, Vue, Astro, Angular with BFF) | Usually promoted to a framework with backend for showcase purposes. If strictly pure frontend, no showcase — only minimal + hello-world shapes. |

This decision maps to role dispatches:
- 1 codebase: scaffold = 1 dispatch (role = monolith; routes serve both API and app).
- 2 codebases: scaffold = 2 parallel dispatches (api + app). Worker is shared.
- 3 codebases: scaffold = 3 parallel dispatches (api + app + worker-separate). Worker consumes from broker in its own process.

`Role` contracts in `internal/recipe/roles.go` encode the four shapes: `monolith`, `api`, `frontend`, `worker`. Research-phase output picks one of the three codebase-shape patterns from the table above and sets `Plan.CodebaseShape` + per-codebase `Plan.Codebases[].Role`. Scaffold dispatcher fans out accordingly.

**The chain's practical effect**

When showcase for `nestjs` runs, it reads:
- `zerops-recipe-apps/nestjs-minimal-app/` (or `nestjs-minimal-api/`, `nestjs-minimal-app/` if minimal was 2-codebase) — the published per-codebase content
- `zeropsio/recipes/nestjs-minimal/0 — AI Agent/import.yaml` (and the other 5 env imports) — the published tier import.yamls

The scaffold agent at showcase extends those: same `main.ts` patterns (trust proxy, shutdown hooks), same migration shape, adds cache client + NATS client + S3 client + Meilisearch client. The writer at showcase reads minimal's IG items + gotchas and cross-references them rather than re-authoring.

This matters for two reasons:

1. **Bounds the discovery problem per recipe.** Showcase is minimal-work plus showcase-specific service additions, not a full re-derivation.
2. **The published recipe tree at `zerops-recipe-apps/*` + `zeropsio/recipes/*` IS the framework knowledge base.** Framework facts accumulate across published recipes. Pre-loading framework specifics into the engine is wrong in principle and redundant with the product's actual storage mechanism.

### Why zcprecipator2 is being replaced, not refactored

Four-plus iterations across v2, same pathology: content authority is scattered across 6 places (atoms, recipe.md compendium, Go prose functions, main agent free-form, sub-agent free-form, scaffold framework-specific examples). Each refactor has tightened one of the six and let another drift. Each has added hardcode when the agent produced garbage, instead of asking whether the engine should be telling the agent that at all. The dispatcher-vs-transmitted-brief class ([v32](../zcprecipator2/README.md#6-defect-class-registry-seed-list)), the manifest↔content inconsistency class (v34), the Go-source env-README prose class (v8.95→v38), the hardcoded-framework-preship class (v39 scaffold dispatches) — all are instances of the same thing: hints became hardcodes, and the engine accreted faster than the agent could keep up with its own teaching.

v3 starts from the right boundary line and commits to budget caps that make it impossible to re-accumulate into the same mess.

---

## 2. The input formula (the shape of every recipe run)

```
previous recipe (parent in chain, if any)
  +
internal knowledge (zerops_knowledge guides, workspace manifest, platform yaml schemas, tier metadata)
  +
output templates (7 surface contracts, recipe file-tree contract, marker forms)
  +
framework knowledge in model (LLM's training data for this framework)
  =
new recipe
```

Every v3 run is composing these four inputs into a single output. The engine's job is:

1. **Resolve** the four inputs (chain lookup, knowledge delivery, template selection, plan routing).
2. **Orchestrate** the 5-phase flow that lets the agent combine them.
3. **Validate** mechanical contracts (yaml schema, file tree, marker form, citation timestamps).
4. **Stitch** the agent's structured output into the recipe file tree.

The engine does **not** supply framework knowledge — that lives in the LLM's training + the parent recipe. The engine does **not** supply platform knowledge — that already lives in `zerops_knowledge` + the workspace manifest + platform yaml schemas, all of which v2 already built and v3 reuses verbatim. The engine does **not** supply classification rules — those are typed data in the surface registry, not prose in atoms.

What the engine adds is **structure + discipline + stitching**. That's why v3 fits in ~2,000 LoC of new Go code. The rest is operational substrate v2 already got right ([v2 README §1 "Stays as-is"](../zcprecipator2/README.md)).

---

## 3. What v3 reuses from v1/v2 (the stays-as-is list)

Verbatim from v2's own architectural stays-list ([v2 README §1](../zcprecipator2/README.md)), because v2 got this boundary right on paper and v3's job is to defend it.

**Operational MCP tools (unchanged)**:
- `zerops_import` — platform provisioning, import.yaml rendering, project-level secret env vars that flow into the export artifact
- `zerops_deploy`, `zerops_dev_server`, `zerops_browser`, `zerops_logs`, `zerops_verify`, `zerops_subdomain`, `zerops_mount`, `zerops_env`, `zerops_discover`
- `zerops_knowledge` — platform knowledge base, the authoritative source of Zerops facts
- `zerops_record_fact` — fact logger (schema-tightened in v3 but mechanism unchanged)
- `zerops_workspace_manifest` — fresh-context input carrier (the v8.94 architecture that already solves "what the writer sees"); v3's writer brief is essentially: workspace_manifest + surface registry + examples

**Platform-level correctness (unchanged)**:
- SSH execution boundary (v8.90 — commands run container-side, never zcp-side)
- Read-before-Edit sentinel (v8.97 Fix 3)
- Git-config-mount pre-scaffold init (v8.93.1) + post-scaffold `.git/` cleanup (v8.96 Fix #4)
- Export-on-request (v8.103 — no auto-export-at-close)
- `zsc execOnce` semantics + static vs per-deploy seed keys (v8.104 Fix B, `bootstrap-seed-v1` pattern)

**Provision → export pipeline (unchanged)**:
- Project-level / service-level secret env vars set during provisioning flow into the generated import.yaml at export. This is battle-tested and invisible-when-working. v3 calls into the same code paths; it does not re-derive the secret-forwarding rules.
- YAML emitter handles every managed-service type correctly (v8.95 Fix B env-README templates stay as engine data; they lose their prose functions but keep their yaml-emission logic).

**State machine + MCP contract (unchanged)**:
- Substep attestation validation, ordering gates, `SUBAGENT_MISUSE` handling
- Facts log with `FactRecord.Scope` field (v3 tightens the schema, not the storage)

**What v3 adds on top of all this**: a single new Go package `internal/recipe/` that orchestrates these existing tools against a 5-phase state machine, with a new `zerops_recipe` MCP tool exposing three actions (start/complete-phase/record-fact-scoped) and a surface registry driving brief composition. Everything else is reuse.

**What v3 rewrites from v2**:
- `internal/content/workflows/recipe.md` → deleted. No replacement monolith.
- `internal/content/workflows/recipe/briefs/*.md` atoms → reduced to ~8 files (surface contracts + 2 examples per surface + fact-recording discipline). All 11 writer atoms + 4 principle atoms + scaffold/feature atoms collapsed.
- `internal/workflow/recipe_templates.go` 4 prose functions (envAudience, envDiffFromPrevious, envPromotionPath, envOperationalConcerns) → deleted. Their data becomes `Tier` struct fields; prose is writer-authored at finalize from tier Diff().
- `internal/workflow/atom_stitcher.go` → replaced by surface-registry-driven `internal/recipe/briefs.go`.
- `internal/workflow/recipe_guidance.go` 900+ LoC of step-entry guidance composition → replaced by per-phase-entry atoms rendered by workflow state machine.
- Classification-taxonomy + routing-matrix + citation-map as writer-brief prose → replaced by typed `SurfaceContract` registry queried at record-time, not consumed as prose at dispatch-time.

---

## 4. The boundary line

Hardcode what is **Zerops product contract**. Discover everything else.

| Category | Hardcoded (system knows) | Discovered (agent produces, recipe captures) |
|---|---|---|
| **Zerops platform contracts** | YAML schemas, cross-service env-var auto-inject, L7 balancer routes to 0.0.0.0, MinIO forcePathStyle, managed Valkey has no auth, `zsc execOnce` semantics, `httpSupport` requirement, rolling-deploy SIGTERM contract | — |
| **Tier metadata** | 6 typed tier structs (index, folder, label, minContainers, mode, cpuMode, zeropsSetup, devServiceKind) — these are Zerops product decisions | — |
| **Role contracts** | API / frontend / worker role shape (what a role exposes regardless of framework) | — |
| **Recipe output contract** | File tree for zeropsio/recipes, 7 surface contracts (what prose belongs where), content markers | — |
| **Workflow** | 5-phase state machine, fact schema, verification methods | — |
| **Recipe chain** | Given a showcase run, locate and mount the minimal recipe for this framework | — |
| **Framework-specific facts** | — | Scaffold CLI, build cmd, prod entry, dev cmd, ports, bind idiom, trust-proxy idiom, graceful-shutdown idiom, worker bootstrap, dev-server allowlist key, `.gitignore` patterns, all of it |
| **Platform × framework intersections** | — | Every gotcha, every IG item, every env-comment decision — the entire published recipe content |

**Test for every proposed hardcode**: "is this a fact about Zerops, or a fact about frameworks / this specific recipe?" If it's the latter, it's discovered — belongs in the agent's output, not the engine's input. This single test kills recipe.md's 3,438 lines, the 4 prose functions in recipe_templates.go, the NestJS preship example, the classification-taxonomy atom, and most of the writer brief body.

---

## 5. Core design principles

**P1 — No English prose in Go.** Go emits YAML, file paths, and enums. If you find yourself writing `return "Runtime containers carry an expanded toolchain..."` in Go, stop.

**P2 — Prose rules that belong in atoms are a tell.** If an atom is teaching *how to classify / where to route / what to cite*, that's data, not prose. Move it to a typed registry queried at runtime. Atoms carry shape (surface contract structure) and examples (annotated pass/fail), not rules.

**P3 — Each content surface has one author and one validation path.** 7 surfaces × {author, inputs, validation, format, examples} = a Go registry. Writer brief composer, editorial-review, checklist generator all walk the registry. There is no second source of truth for any surface.

**P4 — Facts are structured at record time, not classified at consume time.** `FactRecord{topic, symptom, mechanism, surface_hint, citation}` is the schema. If the scaffold agent recorded `{topic: vite-dev-allowlist, ...}` the writer doesn't re-classify — it reads the structured record.

**P5 — Verification is behavioral, not grep-based.** Preship checks that grep for framework-specific symbols are cargo cult. The real question is "does the app handle X-Forwarded-For correctly?" — you ask that by sending a request, not by grepping for `trust proxy`.

**P6 — Minimal is authority for showcase.** Showcase mounts `recipes/{framework}-minimal/` as reference input. The minimal's zerops.yaml is authoritative framework knowledge. Showcase's scaffold agent extends the minimal's codebases rather than regenerating them from scratch.

**P7 — Budget caps enforce discipline.** If a component spills past its cap, the abstraction is wrong — delete and rethink, don't add a helper. Caps are stated in §8.

**P8 — Strangler fig, not big-bang.** `internal/recipe/` ships alongside `internal/workflow/`. Each tier (hello-world → minimal → showcase) proves itself end-to-end before the next starts. Old code deletes only after showcase-v3 ships a recipe cleaner than showcase-v2 ever did.

---

## 6. Target architecture

### Go packages

```
internal/recipe/
├── tiers.go           // 6 Tier structs (Zerops product data)
├── roles.go           // 3 Role contracts (api / frontend / worker)
├── surfaces.go        // 7 SurfaceContract registry
├── facts.go           // FactRecord schema + storage
├── workflow.go        // 5-phase state machine
├── yaml_emitter.go    // import.yaml renderer (schema from plan + tier, no prose)
├── briefs.go          // sub-agent brief composer (walks surfaces registry)
├── gates.go           // mechanical checks (file exists, marker form, citation timestamp, JSON valid)
├── chain.go           // recipe-chain resolution (locate + mount parent recipe)
├── verify.go          // behavioral verification (deploy, sweep features, check responses)
└── handlers.go        // MCP tool handlers (zerops_recipe_v3 action=*)
```

Everything else is atoms or `zerops_knowledge` topics.

### Content

```
internal/content/recipe_v3/
├── briefs/
│   ├── scaffold/      // role contracts + preship contract (platform-only, no framework prose)
│   ├── feature/       // feature suite contract (what to implement by kind, not how)
│   └── writer/        // surface contracts + examples bank
├── principles/        // Zerops platform principles (bind, SIGTERM, credentials)
└── phase_entry/       // per-phase guidance rendered by workflow at action=start / action=enter-phase
```

No monolithic `recipe.md`. No Go prose functions. No framework-specific atoms.

### MCP surface

One top-level tool: `zerops_recipe` (v3). Actions:

- `start` — open a recipe run, returns plan scaffold + first phase guidance
- `enter-phase` / `complete-phase` — state machine transitions with gate checks
- `build-subagent-brief` — composes from surfaces registry (not atom concatenation)
- `verify-subagent-dispatch` — encoding-normalized SHA check
- `record-fact` — structured fact record (schema-validated at record time)
- `resolve-chain` — given framework + tier, locate + mount parent recipe
- `emit-yaml` — engine renders import.yaml schema from plan + tier (no prose)
- `stitch-content` — engine takes writer's structured payload and writes it into file tree

Nothing else.

### Sub-agents

Three, permanently scoped:

- **Scaffold** (one dispatch per codebase): produces clean source tree + zerops.yaml. Inputs: role contract + platform contract + parent-recipe's zerops.yaml (if chain hit). Records facts. Verifies behaviorally.
- **Feature** (one dispatch for the whole feature suite): produces feature code. Inputs: scaffold's symbol table + feature catalog (kind→contract, not kind→code). Records facts.
- **Writer** (one dispatch, may fix-dispatch once): produces single structured content payload. Inputs: `zerops_workspace_manifest` (v2's existing fresh-context carrier — rendered yaml per tier, per-codebase zerops.yaml, file tree, discovered env vars, active plan) + surface registry + facts log filtered by `surface_hint` + parent-recipe content (if chain hit) + 2 pass + 2 fail examples per owned surface. Returns structured payload; engine `stitch-content` writes payload into the file tree at their canonical paths.

The workspace manifest is the critical input. v2 got this right at v8.94 — the writer reads the manifest tool's output to see the run's full state without having the main agent's debug history polluting its context. v3 keeps this unchanged and relies on it for the "source-of-truth-visible-to-writer" requirement. No `/tmp/writer-brief.txt` round-tripping, no stale context; the manifest tool is a live query.

No fourth sub-agent unless a new product surface (not a new refactor attempt) justifies it.

---

## 7. The recipe chain (the piece v2 never formalized)

### Chain resolution at `action=start`

```
tier=showcase framework=nestjs slug=nestjs-showcase
  → chain.Resolve() walks: showcase → minimal → hello-world
  → for each parent, locates published recipe at:
       ~/recipes/{framework}-{parent_tier}/  (local mount from zeropsio/recipes clone)
       OR fetches from GitHub raw
  → returns ParentRecipe{
       tier,
       codebases: {hostname → {README.md, zerops.yaml, source_tree_sample}},
       env_imports: {tier → import.yaml},
       facts: []FactRecord  (if parent published its fact archive)
    }
```

### What each phase does with the parent

- **Research**: parent's plan is a starting point. `plan.services` inherits parent services; showcase adds cache/broker/storage/search as increments.
- **Provision**: engine renders import.yaml for showcase tiers by **diffing against parent's env import.yaml**, emitting only the deltas plus the new-tier-specific scale fields. Cross-tier promotion prose (env READMEs) is authored by the writer based on the diff, not by Go.
- **Scaffold**: parent's codebases are cloned onto the mount as the starting point. Scaffold agent's job shrinks to "extend codebase X from minimal-shape to showcase-shape" — add NATS subscriber, add cache client, add search client, add worker split, etc. Parent's IG items + gotchas are read (not copied) so showcase content knows what's already documented at the minimal level.
- **Feature**: parent's feature (typically one CRUD endpoint) is the base. Feature agent extends to the showcase feature suite.
- **Writer**: parent's per-codebase README IG items + gotchas are *referenced* (cross-link), not duplicated. Showcase's IG items + gotchas are only the NEW ones that showcase-specific services introduce. This collapses the cross-surface-duplication problem the v28 content-surfaces spec identifies — if a fact is already in the minimal, showcase cross-references rather than re-authors.

### Chain validation

Engine enforces:
- Showcase recipe's zerops.yaml per codebase MUST NOT regress minimal's zerops.yaml (can add, can't contradict)
- Showcase recipe's env-5 tier must promote cleanly from env-4, which must promote cleanly from minimal's single env
- Showcase recipe's IG items must not duplicate parent's IG items verbatim

Parent is authority; showcase is increment. Violation blocks `action=complete-phase`.

### Knowledge capture becomes the product, not a byproduct

This is the payoff. Every published recipe adds to the framework knowledge base available to the next tier's run. Over time:

- New framework's hello-world run: agent does full framework-docs reading, produces baseline platform×framework idioms
- Same framework's minimal run: inherits hello-world's idioms, adds DB-related discovery
- Same framework's showcase run: inherits minimal's idioms, adds service-fanout discovery
- Second framework's hello-world run: starts from zero again (no parent)

And because minimal+showcase read their parent directly (not a "framework catalog"), the knowledge is always fresh — if the minimal recipe is re-run and updates (say, framework version bump changed an idiom), the showcase picks up the new idiom automatically next time it runs.

The "framework catalog" concern from earlier discussion is resolved: **the catalog is the published recipe tree**, not a file in `internal/recipe/`.

---

## 8. Budget caps (discipline mechanism)

Every component has a byte / LoC cap. Spilling over means the abstraction is wrong — DELETE and rethink, don't factor a helper.

| Component | Cap |
|---|---|
| `internal/recipe/*.go` total | **2,000 LoC** |
| Any single `.go` file in `internal/recipe/` | 500 LoC |
| Any sub-agent brief at dispatch time | **15 KB** (was 60KB in v2) |
| Any single atom `.md` file | 5 KB |
| Principle atoms (total across all) | 4 KB |
| Phase-entry guidance (per action response) | 3 KB |
| `zerops_recipe` MCP tool total schema | 2 KB |
| Facts log record | 500 bytes per record; required fields enforced |
| `yaml_emitter.go` | 400 LoC (all 6 tiers + delta rendering) |

If any cap is going to be exceeded: stop, identify which principle (P1-P8) the overage violates, delete the violation. Do not negotiate caps. Caps are the reason v3 stays clean — v2's slow drift into 3,438-line recipe.md is the cautionary tale.

---

## 9. Execution phases

### Phase 0 — Freeze v2 (week 0)

- Tag `internal/workflow/` HEAD as `v8.113.0-archive`.
- Commit a README pointer at `internal/workflow/README.md` saying "this is zcprecipator2, frozen. v3 lives at internal/recipe/. do not add features here."
- Ship no v2 changes after this.

### Phase 1 — Core data + workflow (week 1)

Ship `internal/recipe/`:

- `tiers.go` — 6 Tier structs. Write one test that asserts `Diff(tier[4], tier[5])` yields exactly the expected promotion fields.
- `roles.go` — 3 Role contracts.
- `surfaces.go` — 7 SurfaceContract entries. Each references its example files by path.
- `facts.go` — FactRecord schema + JSONL read/write. Reject records missing required fields.
- `workflow.go` — 5-phase state machine. Test: each phase transition validates its precondition.
- `chain.go` — chain resolver. Test: given showcase+nestjs, locates `~/recipes/nestjs-minimal/` and returns ParentRecipe.
- `gates.go` — mechanical gates.

Green gate: `go test ./internal/recipe/... -race` passes. LoC budget check: total under 2,000.

### Phase 2 — YAML emitter + brief composer (week 2)

- `yaml_emitter.go` — emits import.yaml from `Plan + Tier + ParentImport` (delta-aware).
- `briefs.go` — composes sub-agent briefs by walking `surfaces.go` registry.
  - Scaffold brief: role contract + platform principles + parent's codebase README (if chain hit) + preship contract. Target 3 KB.
  - Feature brief: feature kind catalog + scaffold's symbol table. Target 4 KB.
  - Writer brief: surface contracts + 2 pass + 2 fail examples per owned surface + citation topic list + completion payload schema. Target 10 KB.
- `verify.go` — behavioral verification helpers (deploy status, HTTP response shape, forwarded-for echo).

Green gate: brief composer produces byte-identical output across runs. All 3 briefs under their caps.

### Phase 3 — MCP surface + dogfood showcase (week 3)

- `handlers.go` — MCP action handlers for `zerops_recipe`.
- Register under CLI flag `--recipe-v3`.
- Run directly against the worst case: `nestjs-showcase --v3`. `nestjs-minimal` is already published (zerops-recipe-apps/nestjs-minimal-app + zeropsio/recipes/nestjs-minimal/), so chain resolution has real input. Showcase is 3 codebases + full feature suite + worker split — if v3 ships this clean, every lower-scope recipe is trivially easier. We work backwards from here, not forwards through hello-world.

Green gate (showcase-specific):
- ParentRecipe loaded (nestjs-minimal's zerops.yaml per codebase + env-0 import.yaml); scaffold agent extends rather than regenerates.
- 3 codebases scaffold clean (apidev + appdev + workerdev), each with its own behavioral smoke test passing (not grep-based).
- Feature sub-agent implements all 5 feature kinds (items CRUD, cache-demo, search-items, storage-upload, jobs-dispatch).
- Deploy dev + cross-deploy stage + feature-sweep both green.
- Writer brief ≤ 15 KB at dispatch. Writer authors 3×README + 3×CLAUDE.md + 6×env-import-comments + 6×env-README-paragraphs + 1×manifest.
- Editorial-review returns ≤ 1 CRIT on first pass.
- Close phase completes without `action=skip` bypass.
- Zero engine-emitted prose in env READMEs — all env-README text authored by writer from tier Diff() structured data.
- Cross-surface-duplication check: showcase IG/gotchas do not restate nestjs-minimal's verbatim (cross-reference OK).

If green: v3 is proven on the worst case. If not: iterate `internal/recipe/` without adding files or exceeding caps.

### Phase 4 — Backfill lower tiers (week 4)

Once nestjs-showcase is green, work backward:
- **nestjs-minimal via v3**: prove v3 produces a minimal at least as good as the currently-published nestjs-minimal-v2. This validates that the "no parent" chain case works (minimal has no parent because nestjs has no `hello-world` — hello-worlds are language-level, not framework-level; see §7).
- **hello-world-bun / hello-world-php** (language-level, pick one with existing published reference): prove v3 handles the no-framework-scaffold case. Hello-worlds have no parent and a trivial feature set; primary purpose is exercising the Zerops platform contract without framework complexity.

Green gate: each lower tier ships via v3 at least as clean as its v2 predecessor. If v3 regresses on a lower tier, fix it under caps — do not add hardcode.

### Phase 5 — Deprecate v2 (week 5)

- Once nestjs-showcase-v3 is green AND at least one additional tier or framework has shipped cleanly via v3: delete `internal/workflow/recipe*.go`, `internal/content/workflows/recipe*.md`, `internal/content/workflows/recipe/` atom tree.
- Keep zcprecipator2 run artifacts (`docs/zcprecipator2/runs/v*/`) for audit trail. Delete zcprecipator2 planning docs (`HANDOFF-to-I*`, `plans/v*-fix-stack.md`) — obsolete.
- v3 MCP tool `zerops_recipe` becomes the sole recipe tool. `zerops_workflow` (v2's surface) is removed from server registration.

### Phase 6 — Framework diversity (week 6+)

Each new framework starts at `{framework}-minimal` (first-time framework discovery). Once minimal publishes, `{framework}-showcase` inherits via chain.

Each framework costs roughly: 1 minimal run (30-60 min wall) + 1 showcase run (45-90 min wall) + analysis. Zero Go edits. Zero catalog edits. The published recipe tree is the catalog.

At this point: the "50 frameworks" concern is solved. Each framework costs one hello-world run + one minimal run + one showcase run worth of agent time. Zero Go edits per framework. Zero catalog edits per framework. The published recipe tree is the catalog.

---

## 10. Analysis harness (shrinks the post-run loop)

Commissioning remains manual: build zcp, authorize on Zerops, start a Claude Code session, paste the recipe prompt, let it run, export artifacts, drop them at `/Users/fxck/www/zcprecipator/{name}/`. That loop has irreducible human gates (authorization, platform deploy time).

The **post-run analysis** is what's tedious. Every question an analyst asks ("what did the writer actually produce?", "did the main agent rewrite sub-agent output?", "what's in each dispatch vs the engine's built brief?", "what changed since the previous run?") is currently a grep+jq expedition through 600-1500-line jsonl files. v2's `extract_flow.py` grabs dispatch prompts + selective tool calls but is lossy (thinking blocks, some tool results, assistant text) and every analyst re-does the same reconstruction cold.

v3's harness moves this work to one CLI invocation:

```
zcp analyze recipe-run /Users/fxck/www/zcprecipator/{name}/ [--baseline=/path/to/prev-run/]
```

### What gets produced

All outputs land in `<run-dir>/analysis/` so the run directory is self-contained.

**Raw-log extraction** (lossless — every event from every stream):

- `analysis/raw/main.json` — every event in `SESSIONS_LOGS/main-session.jsonl`, typed + parsed, tool calls linked to their tool_results by id.
- `analysis/raw/sub-<agent-id>.json` — same for each subagent jsonl.
- `analysis/raw/tree.json` — reconstructed execution tree: main session timeline with Agent dispatches nested as sub-branches, each sub-agent's timeline nested under its dispatch. Parent-child linkage on every turn.
- `analysis/raw/tree.md` — the same tree rendered as navigable Markdown, heading-per-agent, subheading-per-substep. Every tool call + result body included inline. This is what an analyst reads first instead of walking jsonl.

**Per-agent summary**:

- `analysis/agents/<agent-id>.md` — one file per agent (main + each subagent):
  - Role, description from meta.json
  - Dispatch prompt received (verbatim) — for subagents
  - Tool-call tally (counts by tool name, total bytes in/out, error rate)
  - All `zerops_record_fact` calls with their payloads
  - Final completion payload (verbatim) — for subagents
  - Wall-time start / end / duration
- `analysis/agents/index.md` — summary table across all agents.

**Dispatch integrity** (per guarded role):

- `analysis/dispatches/<role>/dispatched.txt` — bytes the main agent sent via the Agent tool (extracted from `message.content[*].input.prompt` on the Agent tool_use event).
- `analysis/dispatches/<role>/engine-built.txt` — engine brief reconstructed by calling `briefs.BuildSubagentBrief(plan, role, facts_log_path)` against the current source tree.
- `analysis/dispatches/<role>/diff.md` — unified diff + encoding-normalized SHA comparison + classification (clean / encoding-only / trailing-newline / semantic-paraphrase).

**Surface validation** (v3-specific):

- `analysis/surfaces.json` — for each of the 7 surfaces in `internal/recipe/surfaces.go`, what the run produced, which `ValidateFn` violations fired, author identity (writer / engine / main-agent rewrite detected).
- `analysis/surfaces.md` — human-readable form with file:line citations.

**Structural + session bars** (v2's existing machine-report format, extended):

- `analysis/machine-report.json` — all B-15..B-24 structural + session bars + v3-specific additions (writer-vs-main authorship of each writer-surface file, citation-timestamp presence, chain-resolution success).

**Content output** (every recipe-output file tagged by authorship):

- `analysis/content/<path>` — copy of every file in the recipe output tree, with a sibling `.meta.json` carrying `{author_role, first_write_ts, edit_count, marker_form_valid, size_bytes}`. Detects main-agent-rewrite-of-writer-output silently (which v39 did for all 3 per-codebase READMEs).

### Delta mode

When `--baseline=<prev-run-dir>` is passed, additionally:

- `analysis/delta/structural.md` — bar-by-bar comparison: sub-agent count, retry rounds per substep, close-step reached, sessionless-export count, writer-first-pass-failures count. Highlights regressions (marker with direction + magnitude).
- `analysis/delta/content.md` — per-output-file unified diff. Flags files that appeared/disappeared between runs.
- `analysis/delta/agents.md` — per-agent behavior delta: tool-call counts, record_fact counts, classify-action counts, dispatch-size deltas. Highlights new patterns ("writer introduced 0→10 zerops_knowledge calls, this is new").
- `analysis/delta/regressions.json` — machine-readable list of specific regression patterns detected: `action=skip` bypasses, `verify-subagent-dispatch` SHA mismatches, wrong marker form in writer output, forbidden phrases in env READMEs (gold-test list), main-agent rewrite of writer-owned paths.

Delta lets an analyst answer "what's different vs the last good run" without re-walking. The question that took 30 minutes becomes reading one page.

### What the harness does NOT do

- **Commission runs.** Manual. Requires Zerops authorization + human-commissioned Claude session.
- **Judge content quality.** Structural validation fires on "citation present / correct shape / right length." Judgment on "is this gotcha folk-doctrine or a genuine platform trap" is still the analyst reading the content. Harness reduces the walking, not the judging.
- **Spawn a second Claude instance to auto-verdict.** Verdict remains human-written. The harness outputs shrink "how long to produce a verdict" from hours to minutes by not making the analyst redo extraction.

### Synthetic fixture tests (separate from run analysis, commit-time only)

No platform hit, runs on every PR touching `internal/recipe/**`:

- `TestBriefCompose_UnderCap` — for each subagent role × fixture plan, compose the brief, assert byte count ≤ 15 KB.
- `TestYAMLEmitter_MatchesFixture` — for each `{framework, tier}` fixture plan, emit import.yaml, assert byte-identical to golden.
- `TestSurfaceRegistry_ValidatesKnownGood` — for each surface × curated pass-sample, run the validator, expect pass.
- `TestSurfaceRegistry_RejectsKnownBad` — for each surface × known-bad sample (folk-doctrine gotcha, wrong-surface IG item, env README with forbidden phrases), run the validator, expect the specific violation it should catch.
- `TestChainResolver` — given fixture `{slug, published_tree}`, return expected ParentRecipe.
- `TestTierDiff` — given adjacent tier pair, return typed field diff matching golden.

These run in seconds, catch brief-composition drift + yaml-emitter regression + surface-validator gaps at commit time without requiring a real recipe run.

### Budget + priority

- `cmd/zcp/analyze_recipe_run.go` — ≤ 300 LoC (CLI wrapping).
- `internal/analyze/` — extends v2's existing package with raw-walk, tree reconstruction, surface validation, delta mode. Net new ≤ 800 LoC.
- Fixture tests live under `internal/recipe/testdata/` (shared with Phase 1-2 types).

Ships in **Commission A Phase 2** alongside the brief composer. The harness makes Commission B's showcase iterations tractable — without it, every v3 iteration hits the same tedious walk-the-jsonls-by-hand that drove the current half-day loops.

---

## 11. Success criteria

v3 is "done" when all of these hold:

1. **Hello-world + minimal + showcase** of at least one framework (nestjs) ship via v3 end-to-end.
2. **No English prose** in any `internal/recipe/*.go` file. `grep` for declarative strings longer than 50 chars returns empty.
3. **No framework-specific hardcode** in `internal/recipe/` or `internal/content/recipe_v3/`. `grep -E 'nestjs|laravel|django|rails|NestFactory|createApplicationContext|StatusPanel' internal/recipe/ internal/content/recipe_v3/` returns only occurrences in example files (annotated as such) or doc comments referencing "example-only".
4. **Sub-agent briefs stay under 15 KB** at dispatch time across hello-world + minimal + showcase runs for any framework.
5. **Engine emits zero prose to env READMEs** — all env README text authored by writer from tier Diff() structured data.
6. **Showcase inherits minimal**: chain resolution loads parent recipe, scaffold agent extends parent's zerops.yaml, writer cross-references parent's IG/gotchas rather than duplicating.
7. **Editorial-review CRIT count ≤ 1** on first pass for showcase.
8. **Close phase completes** without `action=skip` on signal-grade substeps.
9. **v2 deleted** from the tree (after v3 ships showcase with parity).

---

## 12. What v3 is explicitly NOT doing

- **Framework catalog in Go.** The published recipe tree is the catalog. No per-framework file in `internal/recipe/`.
- **Preship scripts with framework-specific greps.** Scaffold agent writes its own behavioral smoke test. Engine's platform-agnostic assertions are 5-8 checks, not 30.
- **Monolithic workflow markdown compendium.** No `recipe.md`. Phase-entry guidance is per-action, on-demand, short.
- **Classification/routing tables in the writer brief.** Surface registry is the typed source of truth; writer brief carries shape + examples only.
- **Main-agent free-form rewrites of writer output.** Writer-owned surface paths are refused to main agent at the MCP boundary. Fix-dispatch (`writer-fix` action) is the only path.
- **Optional `verify-subagent-dispatch`.** Every Agent dispatch of a guarded role is engine-verified at call time (PreToolUse hook or equivalent). No opt-in guard.
- **Feature catalog with implementation code.** Feature brief has kind contracts (what a `cache-demo` feature proves), not code. Feature agent implements per framework using scaffold's symbol table.

---

## 13. Risks + watches

- **Chain-resolution mount failures.** If `~/recipes/` doesn't have the parent recipe cloned, showcase can't start. Engine must fetch from GitHub raw or error cleanly — not silently run without parent.
- **"Just add one helper" drift.** Every time the budget caps feel tight, the temptation is to factor a helper that grows into the next `recipe_templates.go`. Re-read §5 principles + §8 caps before adding any file.
- **Writer brief context pressure.** v2's v39 surfaced this — a 60KB brief forced disk-round-trip extraction that broke `verify-subagent-dispatch` fidelity. v3's writer brief is ≤ 15 KB at dispatch, and the heavy content (workspace manifest, per-codebase yaml, source tree) comes from the `zerops_workspace_manifest` tool the writer calls itself, not from brief bytes. Watch that brief composer does not slide past 15 KB; the manifest tool is where runtime state belongs, not the brief.
- **Behavioral verification flakiness.** Deploys fail for network reasons, feature sweeps miss intermittent bugs. Retry budget + deterministic failure modes matter. Start with a 2-retry cap on verification steps; surface real failures.
- **Old code still in tree during Phase 1-5.** If a developer accidentally lands changes in `internal/workflow/` instead of `internal/recipe/`, freeze protection matters. A CI check + pre-commit hook that refuses modifications to `internal/workflow/recipe*.go` after Phase 0 is cheap insurance.
- **Provision → export regressions.** The `zerops_import` pipeline handles secret env var routing from project/service-level settings into the exported `import.yaml`. v3 calls into the same code; no re-implementation. If the v3 workflow bypasses any of the v2 export surface (e.g. by writing its own import.yaml instead of calling `emit-yaml` which delegates to v2's yaml emitter), secret-handling regressions become possible. Watch: v3's `yaml_emitter.go` wraps v2's yaml emitter, does not replace it.

---

## 14. Resolved decisions

Earlier drafts had these as open questions; all five are decided.

1. **No separate fact archive.** The published recipe IS the knowledge carrier. `zcp sync recipe publish` already encodes this: per-codebase content (README with fragments, zerops.yaml, CLAUDE.md, source) lands at `zerops-recipe-apps/{slug}-{hostname}/`; env-tier content (import.yaml per tier + env README per tier) lands at `zeropsio/recipes/{slug}/{env}/`. Showcase's chain reads those published artifacts directly. Facts are a within-run structured input to the writer; across runs, the curated recipe content is the persistent knowledge. No "publish the facts log" step; no separate archive to mount.

2. **Chain resolution is deterministic, not user-supplied.** `chain.Resolve()` applies a fixed walk: `showcase → minimal → hello-world → ∅`. User does not pass `--parent=`. If no parent exists in the published catalog for this framework (e.g. first-ever run of a framework at any tier), `ParentRecipe` is empty and the agent does full first-time discovery from framework knowledge + Zerops contracts. As soon as any tier publishes for a framework, subsequent tier runs for that framework inherit.

3. **Every recipe gets every env.** Hello-world, minimal, showcase — all produce 6 env tiers (AI-agent, Remote/CDE, Local, Stage, Small Production, HA Production). There is no tier-scoped env rendering, no "hello-world skips env 4/5". The env tier ladder is a Zerops platform feature that applies universally to every recipe. The 6 env README + import.yaml pairs are emitted for every recipe scope.

4. **Framework version is latest-stable at run time.** No per-parent version tracking. When showcase for nestjs runs, it uses the latest stable NestJS. If minimal was published at an older NestJS, the showcase agent sees the older minimal's zerops.yaml but is free to update its own scaffold to current. No reconciliation machinery; no "was your parent at X, now you're at Y". Each recipe captures the latest-stable state of its framework-at-publish-time.

5. **v2 deletion triggers on first quality showcase, not full-framework parity.** As soon as one showcase-via-v3 ships a recipe that passes quality bars (editorial-review ≤ 1 CRIT first pass, close completes without `action=skip`, env READMEs zero engine-prose, chain resolution worked cleanly), v2's `internal/workflow/recipe*.go` + `internal/content/workflows/recipe*.md` + `internal/content/workflows/recipe/` atom tree are deleted. Additional frameworks land on v3 only. v2 run artifacts under `docs/zcprecipator2/runs/v*/` stay for audit.

---

## 15. Is this plan actionable? Yes — by commission

Plan is complete. The fresh instance picking this up has:

- Concrete boundary rules (§4) and design principles (§5) with budget caps (§8) that kill accretion mechanically
- Target architecture (§6) with Go file-level breakdown at ~2,000 LoC
- 6-phase execution plan (§9), starting Phase 0 = freeze v2, ending Phase 5 = delete v2
- Test harness spec (§10) that closes the half-day-per-iteration loop
- Resolved decisions on every product question (§14)
- Reused substrate (§3) enumerated so the new code does not accidentally re-implement v2

No Go code yet — Phase 1 makes the plan real.

### Natural commission boundaries

Phase transitions are not all user-review-gated; they're **naturally gated by external events** (published parent recipe, real platform deploys). Grouping by natural boundary gives three commissions:

**Commission A — Phases 0 + 1 + 2 (~1,500 LoC, one session)**
- Pure Go + atoms. No external platform dependency.
- Freeze v2, ship core types, ship yaml emitter + brief composer + verify + handlers + harness CLI (`zcp recipe test`).
- Green gates: `go test ./internal/recipe/... -race` passes, `make lint-local` passes, all 3 briefs compose under their caps, fixture tests all green, total LoC under 2,000 + 400 for harness.
- Output: v3 is compiled, testable, registered under `--v3` flag, harness CLI works against synthetic fixtures. Not yet run against real platform.

**Commission B — Phase 3 dogfood showcase (one session + real platform run)**
- Deploy Commission A's build to the container.
- `zcp recipe test nestjs-showcase --framework=nestjs --tier=showcase --v3 --parent=auto` (harness handles project creation + chain resolution + session spawn + analysis).
- nestjs-minimal is already published — parent chain has real input from day one.
- Wall time: ~45-90 min for the real run + 15 min analysis. Agent coding work between: iterate on brief atoms or surface validators based on analysis output, under caps.
- Green gate: showcase run ships with ≤ 1 editorial-review CRIT, close completes without `action=skip`, writer brief stays ≤ 15 KB, parent-chain cross-references work cleanly.
- If not green: iterate `internal/recipe/` in-place (still within caps), re-run. Three iterations max before stepping back to rethink.

**Commission C — Phases 4 + 5 + 6 (one or two sessions)**
- Backfill: re-run nestjs-minimal via v3, prove parity with current published minimal. Run a language-level hello-world (`hello-world-bun` or `hello-world-php`) via v3 to prove the no-parent path.
- Once green: delete v2. Then expand framework coverage (laravel-minimal → laravel-showcase, django-minimal → django-showcase, etc.) as demand requires.
- Natural gate between Phase 4 and Phase 5: the parent-of-parent case (hello-world having no parent) should be proven before deleting v2, as insurance.

### Handoff prompt for Commission A

```
Read docs/zcprecipator3/plan.md front-to-back.

Then follow Commission A from §15:
- Phase 0 (freeze v2) as one commit.
- Phase 1 (core types) as one commit.
- Phase 2 (yaml emitter + briefs + verify + handlers + `zcp recipe test` harness CLI) as one or more commits.

Stop when:
- Everything under internal/recipe/ compiles, passes race-tests, passes make lint-local.
- All fixture tests green under internal/recipe/testdata/.
- `zcp recipe test --help` shows the harness CLI.
- Total internal/recipe/ LoC ≤ 2,000. cmd/zcp/recipe_test.go ≤ 400.

Do NOT:
- Read zcprecipator2 atoms, recipe.md, HANDOFF-to-I*, runs/v*/ analyses.
- Port code from internal/workflow/recipe*.go — start fresh from plan §3-§7.
- Run real recipe against the Zerops platform in this commission (that is Commission B).

When done, commit all of Commission A, push the branch, hand back to user for Commission B planning.
```

### What a fresh instance will be tempted to do (watch for this)

- **Port `internal/workflow/recipe_templates.go`'s tier logic.** The yaml emitter + tier structs in Phase 1-2 must be derived fresh from Zerops platform YAML schema + [spec-content-surfaces.md](../spec-content-surfaces.md), not lifted from v2. Porting brings the prose + framework assumptions back. Verify: `wc -l internal/recipe/tiers.go` should be ~120-150 LoC of pure struct definitions + one `Diff()` function. If it creeps past 300, the instance has ported prose or switch statements that shouldn't be there.

- **Put framework specifics into `roles.go`.** Role contracts describe WHAT (routes, artifacts, platform obligations) not HOW. If `roles.go` mentions NestJS, Laravel, Svelte — revert and rewrite. Role is framework-agnostic.

- **Extend briefs beyond 15 KB.** If the writer brief won't fit under 15 KB, the instance is re-inlining content that belongs in the surface registry or in `zerops_knowledge` topics. Cut, don't cap-negotiate.

- **Implement `zerops_workspace_manifest` from scratch.** It exists (v8.94). Import it. If the instance writes a parallel manifest tool, stop and have it call the existing one.

### What's intentionally under-specified

Things the plan leaves as design-time decisions for Commission A to make:

- **Exact field shape of `Tier` struct** — should derive from reading the 6 current tier behaviors in Zerops docs + cross-checking against what `spec-content-surfaces.md §5` says env README and env import.yaml comments need to differentiate.
- **`SurfaceContract.InputFn` signature** — probably `func(*Plan, *FactsLog, *ParentRecipe) Inputs` but Phase 1 can refine.
- **Harness internals** — how `zcp recipe test` actually spawns a Claude Code session (headless CLI, MCP handshake, or background process with log-tailing) is an implementation detail. Target: simplest thing that captures all artifacts + exits cleanly.
- **Handler action naming under `zerops_recipe`** — plan lists `start / enter-phase / complete-phase / build-subagent-brief / verify-subagent-dispatch / record-fact / resolve-chain / emit-yaml / stitch-content` but the exact action names can shift if simpler names emerge during implementation.

These are not open questions blocking commissioning — they are local decisions the implementer makes while shipping Phase 1-2. Caps + tests catch drift.

---

## 16. Reading order for whoever picks this up

1. This document front-to-back.
2. [zcprecipator2 README](../zcprecipator2/README.md) §0-2 — the stays/rewrites architectural decisions. v3 holds this boundary; it does not re-derive it.
3. [spec-content-surfaces.md](../spec-content-surfaces.md) — the surface contracts v3 encodes as typed data in `internal/recipe/surfaces.go`.
4. One published recipe from `zeropsio/recipes` (any framework, showcase tier) — the output shape v3 is producing.
5. `internal/workflow/recipe_templates.go` — read the structure ONLY (tier metadata, yaml emitter). The 4 prose functions (`envAudience` et al., L226-421) are the negative example — read them only to confirm you can name why they are wrong.

Do NOT read zcprecipator2's atom corpus, `recipe.md`, brief composers, or the 39 run analyses under `runs/v*/`. They are not reference material. They are the model v3 is replacing. Looking at them first will anchor the architecture to the old model and the budget caps (§8) will be exceeded before Phase 2.
