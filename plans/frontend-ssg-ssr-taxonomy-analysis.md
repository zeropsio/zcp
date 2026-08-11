# Frontend recipe taxonomy (SSG/SSR mode-as-spine) — implementation analysis

## 1. Executive summary

This is **feasible and low-risk if done as a derived predicate, not a new enum** — but the enforcement surface is a **quad-gate, not a dual-gate**, and getting all four to agree on "static vs node-server" is the load-bearing design problem, not a footnote.

The static-vs-node binary already exists three times in the codebase — `topology.RuntimeStatic`/`RuntimeDynamic` (`internal/topology/types.go:7-8`), the classifier `topology.RuntimeClassFor` (`internal/topology/runtime_class.go:28-43`), and the per-codebase resolver `prodRuntimeType(cb)` (`internal/recipe/yaml_emitter.go:500-505`) — so "mode" is not a new fact; it is a derivation over a fact the `Plan` already serializes (`Codebase.ProdRuntimeBase`, `internal/recipe/plan.go:213-223`). The single biggest content lever is that `showcase_scenario.md:29-31` already says *"Scope the cards to the managed-service categories the recipe actually provisions"* — the feature brief is **already services-list-driven** (verified: line 29-31 reads "A recipe without a queue/broker doesn't render a Queue card"), so dropping `broker` from an SSG plan's `Plan.Services` makes the Queue card vanish with zero brief-composer change.

The shape of the work is therefore: one ~25-line derivation predicate in `recipe/`, **four** mode-branches across the **four** showcase enforcement gates, one research-guidance edit, one shape-classification edit, two new SSG scenario/atom variants, and a YAML-emitter byte-golden re-baseline.

**The four showcase gates that must all learn mode** (the central correction over the first draft, which named only two):

| # | Gate | File:line | Discriminator today | Mandates today |
|---|------|-----------|---------------------|----------------|
| G1 | `gateTierServiceSet` (recipe) | `internal/recipe/phase_entry.go:246-264` | `Plan.Services[].Hostname` strings vs `{db,cache,broker,storage,search}` | all 5 hostnames present |
| G2 | `validateShowcaseServices` (workflow) | `internal/workflow/recipe_validate.go:331-376` | `serviceTypeKind(t.Type)` enums + `t.Hostname` | 5 kinds + `hasApp` + `hasWorker` + `queue` hostname |
| G3 | `validateShowcaseFeatureCoverage` (workflow) | `internal/workflow/recipe_features.go:256-298` | `serviceTypeKind(t.Type)` → surface + `t.IsWorker` | feature must cover every kind + `api` + `ui` + `worker` |
| G4 | `gateWorkerSubscription` (recipe) | `internal/recipe/validators_worker_subscription.go:387-393` | `Plan.Tier == tierShowcase` + per-codebase `IsWorker` | queue-group + drain teaching on every separate worker |

G1 and G2 diverge on **input representation** (hostname strings vs kind enums) *and* on what each reads to derive mode (G1 reads recipe-side `Codebase.ProdRuntimeBase`; G2 reads workflow-side `RecipeTarget.Type` — **different fields that can disagree**, see §5 rationale point 3 and §10 decision 7). G3 is a *second* workflow mandate that independently forces `worker` + `api` + `ui` coverage. G4 self-excludes for SSG only because an SSG plan has no worker codebase — but it must be confirmed, not assumed.

The recommendation is **Approach A (derived mode)** for the gate/brief spine, with the *naming discipline* of Approach B borrowed only where a stored value genuinely helps (a clear, greppable, test-pinned predicate name). The single biggest risk is **cross-gate mode-derivation desync** (G1 reads a different field than G2/G3): if the recipe gate derives "static" from `ProdRuntimeBase="static"` while the workflow gate derives it from `RecipeTarget.Type` classifying `RuntimeStatic`, a Next-`output:export` plan whose `Type` is still `nodejs@22` (with `DevBase`/prod-base static) classifies *dynamic* at the workflow gate and *static* at the recipe gate — the exact desync this design must eliminate. §2.1 fixes this by making prod `Type=static` the single contract both gates read.

## 2. Target model (recap)

Frontend frameworks get the same minimal/showcase ladder backends have, organized around **rendering mode** because on Zerops a frontend has exactly two platform configurations and that binary *is* the recipe's teaching subject:

- **STATIC (SSG/SPA)**: `run.base: static`, Nginx serves built files, **no process**, build-time env only, no readiness/health, **app touches DB at BUILD time** (data baked, rebuild-to-refresh).
- **NODE-SERVER (SSR)**: `run.base: nodejs`, framework's node adapter runs a live process, runtime env, readiness/health, **live DB connection per request**.

Ladder:
- **minimal-SSG + minimal-SSR** = the config-teaching pair for dual-mode frameworks (Next `output:export`, SvelteKit `adapter-static`, Nuxt static preset). SSR-only frameworks (Remix/RR7) get one.
- **showcase = ONE flagship in the framework's NATURAL mode**: Astro → SSG content platform; Next/Nuxt/SvelteKit → SSR dynamic app.
- **The deepest distinction the mode encodes**: WHEN the DB is touched (SSG=build-time baked; SSR=runtime).
- **Keystone artifact** is the irreducibly per-framework **node-server adapter/output config** (Next `output:'standalone'`, Nuxt nitro node preset, SvelteKit `adapter-node`, Astro `@astrojs/node`, RR7/Remix node, Angular `@angular/ssr`) — because the default deploy target for all of these is serverless, and Zerops' pitch is "run it as a long-lived container."
- **Per-track background story** replaces the contrived worker: SSG → scheduled/triggered rebuilds; SSR → live updates (broker→SSE push).
- Frontend showcase is naturally **shape 1** (monolith) for an SSG content-platform *only if* research-phase classification is taught to admit it (see §4 row "Shape/role" and §3.2 — the existing classification reserves shape-1 monolith for *view-rendering* servers, so this is a real edit, not a no-op).

## 3. Current-state map (file:line grounded)

### Tier system — deterministic code
- **Three recipe tiers** are const strings: `tierMinimal`/`tierHelloWorld`/`tierShowcase` (`internal/recipe/chain.go:140-144`). The workflow layer mirrors them as `RecipeTierMinimal`/`RecipeTierShowcase`/`RecipeTierHelloWorld` (`internal/workflow/recipe.go`).
- **`gateTierServiceSet`** (`internal/recipe/phase_entry.go:221-267`) is the recipe-side tier→service constraint: hello-world ⇒ 0 managed (`:233-238`), minimal ⇒ ≥1 managed (`:240-245`), showcase ⇒ the 5 canonical **hostnames** `{db, cache, broker, storage, search}` in a map literal (`:247`), matched against `Plan.Services[].Hostname` (`:248-252`), emitting `showcase-service-set-incomplete` when any missing (`:259-263`).
- **Recipe tier ≠ environment tier**: `Tiers()` returns 6 environment tiers (`internal/recipe/tiers.go`); orthogonal to the hello-world/minimal/showcase axis. Do not conflate. Pinned by `TestTiers_Count` (=6).

### The FOUR independent showcase enforcement gates — deterministic code
This is the corrected enforcement inventory. The first draft named only G1+G2; G3 and G4 are equally load-bearing.

- **G1 — `gateTierServiceSet`** (`phase_entry.go:246-264`, in `researchGates()`): showcase branch, hostname-string match, `showcase-service-set-incomplete`. (Same fn as above.)
- **G2 — `validateShowcaseServices`** (`internal/workflow/recipe_validate.go:331-376`, called from `ValidateRecipePlan`): matches on `serviceTypeKind(t.Type)` **enums** (`:350-352`), hard-mandates `MessagingHostnameConvention = "queue"` (const at `:313`; reject at `:357-361`), and **requires both** a runtime app target (`!hasApp` reject at `:364-365`) *and* a worker target (`!hasWorker` reject at `:367-368`). This gate has no awareness of G1's hostname map and vice-versa.
- **G3 — `validateShowcaseFeatureCoverage`** (`internal/workflow/recipe_features.go:256-298`, called unconditionally for showcase from `validateFeatures` at `:157-158`): builds `requiredSurfaces` seeded with `api`+`ui` always (`:258-259`), adds a surface per managed kind via `showcaseKindToSurface` (`:262-263`, table at `:234-241`), and adds `worker` whenever any target `IsWorker && IsRuntimeType` (`:265-266`). Then it fails the plan if any required surface is uncovered (`:292-296`). **This is a third parallel mandate** — an SSG showcase that (correctly) has no broker and no worker would still be forced to cover `queue`/`worker` if the agent ever declared them, and the validator unconditionally forces `api`+`ui`. The first draft's "regression-free by construction" claim is false until G3 is mode-gated.
- **G4 — `gateWorkerSubscription`** (`internal/recipe/validators_worker_subscription.go:387-417`): fires when `Plan.Tier == tierShowcase` (`:391-393`), iterating every codebase, skipping non-workers (`:396-398`) and shared workers (`:399-401`), to enforce queue-group + drain teaching on each *separate* worker. It is **tier-keyed, not mode-keyed**. An SSG showcase legitimately has no worker codebase, so it **self-excludes only because the loop body is empty** — this must be *confirmed by a regression assertion*, not assumed.

### Shape / role — deterministic code
- **`gateShapeClassificationConsistency`** (`internal/recipe/phase_entry.go:269-317`): shape 1 ⇒ exactly 1 `RoleMonolith` (`:289-295`); shape 2/3 ⇒ 1 `RoleAPI` + ≥1 `RoleFrontend` (`:296-308`); shape 3 ⇒ exactly 1 separate worker (`:309-314`). **Rendering-mode-blind**, and shape-1 is *role*-constrained to monolith.
- **Research-phase classification** (`research.md:46-63`) routes **full-stack view-rendering** frameworks (Laravel/Blade, Rails/ERB, Django/Jinja2, Phoenix/HEEx, SvelteKit+server, Next.js+server) → **shape 1 monolith** (`:50-52`), and **API-first JSON-only** frameworks → **shape 2/3** (`:53-60`). The "Frontend default" (`:65-69`) is "Svelte + Vite compiled to static," deploying `static` in prod — but it is offered **only as the shape-2/3 frontend codebase**, never as a shape-1 monolith. **An Astro SSG content-platform is a frontend, not a view-rendering server** — under current guidance it would route to shape 2/3 (needing a `RoleAPI` sibling), which contradicts the "showcase = shape-1 monolith" target. This is a real classification edit (see §3.2 and §10 decision 8), not a no-op.
- Roles are 4 framework-agnostic contracts (`internal/recipe/roles.go`); `HasUICodebase()` treats `RoleFrontend`||`RoleMonolith` as UI-owning (`internal/recipe/plan.go:160-170`).

### Runtime / topology vocabulary — deterministic code
- `RuntimeClass` already encodes the binary: `RuntimeStatic`/`RuntimeDynamic` (`internal/topology/types.go:7-8`); `RuntimeClassFor` is a **5-value** classifier — `static`/`nginx`→`RuntimeStatic` (`runtime_class.go:39-40`), `php-nginx`/`php-apache`→`RuntimeImplicitWeb` (`:36-37`), managed→`RuntimeManaged` (`:32-33`), empty→`RuntimeUnknown` (`:29-30`), everything-else→`RuntimeDynamic` (`:42`). `Mode` (`types.go:14-29`) is deploy-lifecycle, orthogonal. There is **no `RenderMode`**.
- **The 5-value enum matters for the negative branch**: a php-nginx monolith classifies `RuntimeImplicitWeb`, NOT `RuntimeDynamic`. So the node-server predicate MUST be written `!isStaticTrack`, never `recipeMode(p)==RuntimeDynamic` (the latter would wrongly exclude php/implicit-web monoliths from the node-server track). See §6 Phase 0.1.
- `Codebase.ProdRuntimeBase` (`internal/recipe/plan.go:213-223`) carries the resolved prod `run.base` (e.g. `"static"` for a Vite SPA whose build base is `nodejs@22`). **Engine-populated at stitch-content time** via `parseProdRuntimeBaseFromYaml`; falls back to `BaseRuntime` when empty. `prodRuntimeType(cb)` (`yaml_emitter.go:500-505`) reads it and is what the emitter writes as `services[].type` (`yaml_emitter.go:251,347,368`).
- **The workflow side has no `ProdRuntimeBase` equivalent.** `RecipeTarget` (`internal/workflow/recipe.go:150-193`) carries `Type` (`:152`), `DevBase` (`:171-177`), `IsWorker` (`:153`), `Role` (`:154`) — but **no per-target prod-runtime-base and no role-vs-monolith UI scan.** The plan-level `RecipePlan.RuntimeType` (`recipe.go:90`) is a single primary-runtime string, NOT per-target. `DevBase` already encodes the build/run asymmetry: a Vite-SPA target sets prod `Type=static` + `DevBase=nodejs@22`. **This asymmetry — prod `Type` is the contract both sides must read — is the fix for the cross-gate desync (§2.1, §5 point 3).**

### Brief trees — mix of deterministic loading + agent-judgment content
- **Two trees**: `internal/recipe/content/` (v3 authoritative; `phase_entry/`, `briefs/`) and `internal/content/workflows/recipe/` + the v2 kind table `internal/content/workflows/recipe.md`. The v2 kind table pins `2a Frontend static`/`2b Frontend SSR` to hello-world (`recipe.md:45-46`), and — critically — documents the **field contract**: "submit exactly these keys — the schema rejects unknown properties" (`recipe.md:38`). This is the canonical doc for what `recipePlan` accepts, so it is more than a stale label table (see §3.3 / I3-fix).
- **`BuildFeatureBrief`** loads `showcase_scenario.md` only `if plan.Tier == tierShowcase` (`internal/recipe/briefs.go:691-693`); loads `worker_subscription_shape.md` only `if pass==FeaturePassBackend && plan.Tier==tierShowcase && plan.HasWorkerCodebase()` (`:709-711`); design tokens gated on `pass==FeaturePassFrontend && tierShowcase && HasUICodebase()` (`:757-760`). All tier-only, no mode branch.
- **`showcase_scenario.md:29-31`** is already services-list-scoped — verified: "Scope the cards to the managed-service categories the recipe actually provisions… A recipe without a queue/broker doesn't render a Queue card." This *scoping rule* is the lever that makes the minimalist gate work with zero composer change.

### Frontend corpus — gitignored, Strapi-synced
- `internal/knowledge/recipes/` holds ~26 frontend variants, **all hello-world** (`nextjs-ssr-hello-world`, `nextjs-static-hello-world`, etc.). No minimal/showcase frontend recipes exist. The dir is gitignored (`.gitignore:42-53`); only `mailpit.md` is committed as a stopgap. The loader is slug-string-driven, so new `{framework}-{mode}-{tier}` slugs need no loader change.

### Pipeline threading — deterministic code
- `gateTierServiceSet` + `gateShapeClassificationConsistency` run in `researchGates()` (`phase_entry.go:131-139`). Provision runs `DefaultGates()` only (no service-set re-check). The YAML emitter (`yaml_emitter.go:47-296`) iterates `plan.Services`/`plan.Codebases` and is tier-oblivious except for the slug suffix + per-tier env seed — so **filtering `Plan.Services` upstream cascades through the emitter** — but NOT "for free" on byte output: dropping a service shifts sort order and dangles any `EnvComments`/`ProjectEnvVariables` keyed by that service (see §6 Phase 1.2 and §8).

## 4. Gap analysis (per subsystem)

| Subsystem | Gap for target model |
|---|---|
| **G1 — Tier gate (recipe)** | `gateTierServiceSet` showcase branch hardcodes 5 hostnames (`phase_entry.go:247`); cannot express SSG's smaller set `{db,cache,storage,search}` or forbid `broker`. Minimal collapses all DB-less cases — and **SSG-minimal at 0 managed is byte-indistinguishable from hello-world at this gate** (both require 0), so the discriminator must be `(Tier==minimal AND isStaticTrack)`, not the service count (see §10 decision 1, and I2-fix in §6 Phase 1.1). |
| **G2 — Tier gate (workflow)** | `validateShowcaseServices` mandates a worker target (`recipe_validate.go:367-368`), an app target (`:364-365`), the `queue` hostname (`:357-361`), and all 5 kinds (`:332-338,370-373`). The worker + queue mandates are SSR-only truths; SSG showcase has no runtime worker and no broker. **Diverges from G1 on representation AND on the mode-derivation input** (G2 has only `RecipeTarget.Type`, no `ProdRuntimeBase`). |
| **G3 — Feature coverage (workflow)** | `validateShowcaseFeatureCoverage` always forces `api`+`ui` (`recipe_features.go:258-259`) and adds `worker` for any `IsWorker` target (`:265-266`) and `queue` for messaging (`:262-263`). An SSG showcase must not be forced to cover `worker`/`queue`. **Third parallel mandate — the first draft missed it.** |
| **G4 — Worker subscription (recipe)** | `gateWorkerSubscription` is tier-keyed (`validators_worker_subscription.go:391`). SSG self-excludes only because no separate worker exists; must be **confirmed by regression assertion**. If any SSG fixture ever declares a build-time worker, G4 needs a mode guard. |
| **Shape/role** | `gateShapeClassificationConsistency` is mode-blind (fine) but **shape-1 is role-locked to monolith** (`phase_entry.go:289-295`), and research classification reserves shape-1 for view-rendering servers (`research.md:50-52`). An Astro SSG content-platform showcase as shape-1 monolith requires **editing research.md classification to admit a static frontend-monolith** (§3.2). Without it the agent routes Astro to shape 2/3 → needs a `RoleAPI` sibling → not the intended single-codebase flagship. |
| **Topology** | No `RenderMode` exists, but **none is needed** — the binary is already `RuntimeStatic`/`RuntimeDynamic`. The only gap is a recipe-side *predicate* that reads it pre-stitch, plus a workflow-side classifier over `RecipeTarget.Type`. |
| **Mode availability timing** | The gate runs at **research phase**; `ProdRuntimeBase` is **stitch-time** populated. Gap: the agent must *declare* the static signal at research-phase `update-plan` — recipe-side via `Codebase.ProdRuntimeBase="static"`, workflow-side via `RecipeTarget.Type="static"` (prod). Stitch later re-derives `ProdRuntimeBase` + overwrites — self-verifying on the recipe side. |
| **Brief content** | `showcase_scenario.md` is SSR-shaped (Queue card at `:25`). Need an SSG variant (rebuild-trigger story, no Queue card). `worker_subscription_shape.md` is correctly gated on `HasWorkerCodebase()` (`briefs.go:709`) so it self-excludes for SSG (verified). |
| **Adapter knowledge** | No per-framework node-adapter/output-config table exists anywhere in the v3 tree. This is the keystone artifact and is missing. |
| **YAML byte determinism** | Dropping `broker` from `Plan.Services` changes the emitter's iteration/sort order and dangles any `EnvComments`/`ProjectEnvVariables` keyed by the dropped service. No phase re-baselines those byte goldens (§6 Phase 1.2). |
| **Corpus** | No `{framework}-{mode}-minimal`/`-showcase` recipes in Strapi yet; engine must tolerate the new slugs (loader already does). |

## 5. Design decision — how "mode" enters the model

Two grounded proposals were developed:

**Approach A — Derived predicate (minimalist).** No new enum, no slug change, no new Plan/Codebase field. Recipe side: add `recipeMode(p *Plan) topology.RuntimeClass` = `RuntimeClassFor(prodRuntimeType(uiCodebase))` over the first UI codebase (`RoleFrontend||RoleMonolith`, reusing the `HasUICodebase` scan at `plan.go:160-170`). Workflow side: classify the UI/app target by `topology.RuntimeClassFor(t.Type)` over `RecipeTarget.Type`. Mode is recomputed on every read. Gates branch on `isStaticTrack`. Blast radius: ~25-line predicate file, four small gate branches, two content edits, a golden re-baseline.

**Approach B — First-class `topology.RenderMode`.** New enum `{RenderModeUnset, RenderModeSSG, RenderModeSSR}` in `topology/types.go`, new `render_mode.go` with `RenderModeFor(framework)` + `ShowcaseServiceSetFor(rm)` + `RenderModeAgreesWithRuntimeClass(rm, rc)`, stored **per-codebase** on `Codebase.RenderMode` and mirrored on workflow `RecipeTarget.RenderMode`. Gates/briefs branch on `(Tier, PrimaryRenderMode())`.

### Recommendation: **Approach A**, with one borrowed discipline from B.

**Rationale, grounded in blast-radius + architecture rules:**

1. **The binary already exists in topology** (`RuntimeStatic`/`RuntimeDynamic`, `types.go:7-8`) with its 5-value classifier (`runtime_class.go:28-43`). Approach B's new `RenderMode` would be a *fourth* axis re-encoding a binary topology already owns. The CLAUDE.md promotion rule ("new shared type → topology first") means *don't put shared types in `workflow/`* — not *invent a new type when an existing one suffices*. Reusing `RuntimeClass` honors that rule *and* the "prefer derivation" spirit.

2. **Approach A honors the codebase's strongest invariant pattern**: "X is DERIVED, never stamped" (auto-close, CLAUDE.md §"Auto-close is DERIVED"). Mode recomputed per-read from serialized runtime strings has no on-disk discriminator to desync — structurally the same win the auto-close refactor banked.

3. **Self-verifying on the recipe side, BUT the two gates read different inputs — this is the design's hard part.** The recipe gate (G1) derives mode from `Codebase.ProdRuntimeBase` (`plan.go:213-223`); the workflow gates (G2/G3) have only `RecipeTarget.Type` (`recipe.go:152`) and no `ProdRuntimeBase`. **These can disagree**: a Next-`output:export` target whose prod `Type` is left at `nodejs@22` (with the static intent only in `ProdRuntimeBase`/`DevBase`) classifies `RuntimeDynamic` at G2/G3 and `RuntimeStatic` at G1. The fix (§2.1): **make prod `Type=static` the single contract both sides read.** A Vite-SPA / `output:export` target sets prod `Type=static` and `DevBase=nodejs@22` (the asymmetry `DevBase` exists for — `recipe.go:171-177`). Then G1's `prodRuntimeType(cb)` (which already prefers `ProdRuntimeBase`, falling back to `BaseRuntime`) and G2/G3's `RuntimeClassFor(t.Type)` agree because both see "static" as the prod runtime. Approach B's `RenderModeAgreesWithRuntimeClass` re-implements this check that the shared-`Type` contract gets for free — but only once the contract is enforced; this is NOT automatic, hence §2.1 is mandatory, not optional. (Recipe-side stitch still overwrites `ProdRuntimeBase` from the authored yaml, so a recipe that *claims* SSG but ships a `nodejs` `run.base` gets caught at stitch.)

4. **The feature *scenario card-set* needs zero change** because `showcase_scenario.md:29-31` is already services-scoped and `worker_subscription_shape.md` is already `HasWorkerCodebase()`-gated (`briefs.go:709`) — verified. A *separate SSG scenario atom* is still wanted (rebuild-trigger story replacing the live-worker story), but it is additive, not a Parts-list-cardinality-changing split of the SSR atom (§6 Phase 3.1 keeps `showcase_scenario.md` byte-stable).

5. **Architecture-test safety**: Approach A adds **no topology import to the core `Plan` struct** and touches **no layering rules** (`internal/topology/architecture_test.go`). The predicate file `recipe_mode.go` *does* import `topology` for `RuntimeClassFor`/`RuntimeStatic` — which is allowed (recipe→topology; two recipe files already import topology), but the claim is precisely "no topology import on the `Plan` struct," not "no topology import anywhere." Approach B adds a `topology` import to the `Plan`/`Codebase` structs themselves, a new cross-file coupling.

**Borrowed discipline from B:** name the predicate clearly and pin it (`recipeMode`/`isStaticTrack` + a dedicated test) so it's greppable, write the negative branch as `!isStaticTrack` (never `==RuntimeDynamic`, per the 5-value enum, §3), and **forbid `broker` for SSG showcase** (not merely omit it) so the gate fails closed.

**What Approach A costs (be honest):**
- **Less greppable** than a stored `Plan.Mode` scalar — debugging "why this gate fired" means running the predicate mentally over the UI codebase. Mitigated by a clear name + test.
- **Dual-write subtlety**: the agent seeds the static signal early (recipe `ProdRuntimeBase`, workflow prod `Type`), fields normally engine-populated late. Needs a crisp doc-comment or a future reader thinks it's a bug.
- **Cross-gate contract enforcement is manual**: §2.1 must police that prod `Type` and `ProdRuntimeBase` agree; the desync is only structurally killed once that contract holds. This is the single most important correctness obligation.
- **First-UI-codebase-wins**: a shape-2 plan with a static frontend *and* a node monolith is ill-defined — but that combination is architecturally incoherent under "mode IS the config, one per recipe," so the limitation matches intent (§10 decision 6).

## 6. Implementation plan (layered, TDD, bottom-up)

Each item: files · change · RED test first · difficulty · deps. Execute top-to-bottom.

### Phase 0 — Predicate foundation (recipe layer; topology untouched)

- [ ] **0.1 — `recipeMode` predicate.** `trivial`. **No deps.**
  - File: `internal/recipe/recipe_mode.go` (NEW, ~25 lines).
  - Change: `func recipeMode(p *Plan) topology.RuntimeClass` — find first cb where `cb.Role==RoleFrontend||cb.Role==RoleMonolith` (mirror `HasUICodebase`, `plan.go:160-170`), return `topology.RuntimeClassFor(prodRuntimeType(uiCB))`; `RuntimeUnknown` when no UI codebase. Add `func isStaticTrack(p *Plan) bool { return recipeMode(p)==topology.RuntimeStatic }`. **Write the node-server predicate as `!isStaticTrack`, never `==RuntimeDynamic`** — `RuntimeClassFor` is 5-valued and a php-nginx monolith returns `RuntimeImplicitWeb` (`runtime_class.go:36-37`), which `==RuntimeDynamic` would wrongly drop. Imports `topology` (allowed; recipe→topology).
  - RED: `internal/recipe/recipe_mode_test.go` — static UI cb (`ProdRuntimeBase="static"`) ⇒ `RuntimeStatic`; dynamic UI cb (`BaseRuntime="nodejs@22"`, empty `ProdRuntimeBase`) ⇒ `RuntimeDynamic`; **php-nginx monolith cb ⇒ `RuntimeImplicitWeb` and `isStaticTrack==false`** (pins the 5-value safety); no-UI (api-only) plan ⇒ `RuntimeUnknown` and `isStaticTrack==false`.

### Phase 1 — Recipe-side gate G1 + YAML determinism (the spine)

- [ ] **1.1 — Mode-branch `gateTierServiceSet` (G1).** `moderate`. **Deps: 0.1.**
  - File: `internal/recipe/phase_entry.go:239-264`.
  - Change: in the **showcase** case (`:246`), `if isStaticTrack(ctx.Plan)` use want-map `{db,cache,storage,search}` **and reject `broker` presence** (new violation `ssg-showcase-no-broker`); else keep the existing `{db,cache,broker,storage,search}` map byte-for-byte (zero regression). In the **minimal** case (`:239`), `if isStaticTrack` require `managed==0` with a distinct message and code (`ssg-minimal-no-runtime-managed`) — the discriminator is `(Tier==tierMinimal AND isStaticTrack)`, **NOT the service count** (a 0-managed SSG-minimal is indistinguishable from hello-world by count alone; see §10 decision 1). Else keep `managed>=1` (`minimal-needs-one-managed-service`). ~15 added lines in the existing switch; no new gate fn, no `researchGates()` reorder.
  - RED: add to `internal/recipe/phase_entry_test.go` (`TestResearchGate_*` table, `:304-466`): SSG-minimal 0 svc PASSES; **SSG-minimal + 1 db FAILS with code `ssg-minimal-no-runtime-managed`** (asserting the *new* code, proving the static branch is reached — not the legacy `minimal-needs-one-managed-service`); SSG-showcase `{db,cache,storage,search}` PASSES; SSG-showcase + broker FAILS `ssg-showcase-no-broker`; **node-server showcase `{db,cache,broker,storage,search}` still PASSES** (regression pin — keep `TestResearchGate_RejectsDogfoodPathology`'s `showcase-service-set-incomplete` case green by exercising the default/dynamic track).

- [ ] **1.2 — YAML-emitter byte-golden re-baseline for SSG (no broker).** `moderate`. **Deps: 1.1.**
  - Files: `internal/recipe/yaml_emitter.go:47-296` (verify, likely no logic change); `internal/recipe/yaml_emitter_test.go` (+ any `*_golden*` fixtures).
  - Change: confirm that an SSG showcase `Plan` whose `Services` omits `broker` emits a deterministic `EmitWorkspaceYAML`/`EmitDeliverableYAML` with no broker entry, **stable sort order**, and **no dangling `EnvComments`/`ProjectEnvVariables` keyed on the absent `broker` service** (`EnvComments` keyed by service key, `recipe.go:136-144`; `ProjectEnvVariables` keyed by env then var, `recipe.go:111`). The emitters are services-driven, so dropping the service from `Plan.Services` removes it from emission — but the comment/env-var maps are authored separately and can reference a hostname that no longer emits. Add a guard or test that an SSG plan carries no `broker`-keyed comment.
  - RED: `TestEmitWorkspaceYAML_ShowcaseStaticOmitsBroker` (yaml contains db/cache/storage/search, NOT broker; byte-stable) and `TestEmitDeliverableYAML_ShowcaseStatic_NoDanglingBrokerComment`. Regression: existing SSR showcase golden bytes unchanged.

### Phase 2 — Workflow-side gates G2 + G3 (the divergence fix — DO NOT SKIP)

- [ ] **2.1 — Mode-branch `validateShowcaseServices` (G2) + enforce the shared prod-`Type` contract.** `moderate`. **Deps: 0.1 (ported predicate).**
  - File: `internal/workflow/recipe_validate.go:331-376`.
  - Change: derive the track from the **app/UI target's prod `Type`** via `topology.RuntimeClassFor(t.Type)` over `RecipeTarget.Type` (`recipe.go:152`) — **NOT `t.RuntimeType` (no such field) and NOT the plan-level `RecipePlan.RuntimeType` (`recipe.go:90`, single-string, wrong for multi-target)**. The static-track signal is "the non-worker UI/app target whose `Type` classifies `RuntimeStatic`." When static-track: drop `kindMessaging` from `requiredKinds` (`:332-338`), drop the `hasWorker` requirement (`:367-368`), drop the `queue`-hostname mandate (`:357-361`), and **add a reject-if-broker-present** check. When dynamic: unchanged. Keep `ValidateRecipePlan`'s tier check intact. **Also add an agreement assertion**: the app target's prod `Type` classification must match what the recipe side would derive from `ProdRuntimeBase` — i.e. an SSG plan must carry prod `Type=static` on the UI/app target (so G1 and G2 read the same fact). Document in a doc-comment that `DevBase` (`recipe.go:171-177`) carries the compile-capable dev runtime; prod `Type` is the source of truth both gates classify.
  - RED: `internal/workflow/recipe_validate_test.go` — SSR showcase still requires all 5 kinds + worker + `queue` hostname (regression); SSG showcase (app target prod `Type=static`, `DevBase=nodejs@22`) passes with `{db,cache,storage,search}`, no worker, and FAILS if broker present. **Add the cross-gate agreement case**: a plan whose recipe `Codebase.ProdRuntimeBase=static` but workflow app `Type=nodejs@22` is rejected as contradictory (or normalized) — this is the test that *proves the two gates read the same fact* and kills the C4 desync.

- [ ] **2.2 — Mode-branch `validateShowcaseFeatureCoverage` (G3).** `moderate`. **Deps: 2.1.**
  - File: `internal/workflow/recipe_features.go:256-298`.
  - Change: derive track from app/UI target prod `Type` (same classifier as 2.1). When static-track: **do not** seed/require `FeatureSurfaceWorker` (skip the `IsWorker` branch effect at `:265-266`) or `FeatureSurfaceQueue` (the messaging kind won't map because broker is absent), and **forbid a declared `queue` surface** on an SSG showcase. Keep `api`+`ui` mandatory (`:258-259`) — even an SSG content platform has both. When dynamic: unchanged. The required-surface set for SSG = `{api, ui, db, cache, storage, search}` (the kinds present), for SSR = `{api, ui, worker, db, cache, storage, search, queue}`.
  - RED: `internal/workflow/recipe_features_test.go` — SSR showcase still requires `worker`+`queue` coverage (regression); SSG showcase passes covering only `{api,ui,db,cache,storage,search}` and FAILS if it declares `queue`/`worker`. **This is the second gate-agreement proof** (G3 ↔ G1/G2).

- [ ] **2.3 — Confirm `gateWorkerSubscription` (G4) self-excludes for SSG.** `trivial`. **Deps: 0.1.**
  - File: `internal/recipe/validators_worker_subscription.go:387-417` (assertion only; add a mode guard ONLY if a future SSG fixture declares a build-time worker).
  - Change: no production change expected — the gate's per-codebase loop (`:395-401`) is empty for a plan with no separate worker. Add a regression assertion that an SSG showcase plan (no worker codebase) yields zero violations from G4. If product later wants a build-time-generation worker on SSG, add `&& !isStaticTrack(ctx.Plan)` to the `:391` guard then.
  - RED: `internal/recipe/validators_worker_subscription_test.go` — SSG showcase plan (UI codebase only, no worker) ⇒ `gateWorkerSubscription` returns nil; SSR showcase with a separate worker still enforces queue-group/drain (regression).

> **Why 2.1+2.2 are mandatory and not "conditional":** `ValidateRecipePlan` fires on the v3 plan-complete path. `recipe_validate.go:367-368` *unconditionally* requires a worker target for any showcase, and `recipe_features.go:256-298` *unconditionally* forces `api`+`ui` coverage plus `worker` for any `IsWorker` target. An SSG showcase has no runtime worker, so without 2.1 AND 2.2 an SSG showcase passes G1 (Phase 1) and then fails G2 and/or G3 — the dual-gate divergence the maps flag, now correctly counted as a quad-gate. Confirmed live at `recipe_validate.go:157-158` (G3 dispatch) + `:364-368` (G2 mandates) + `recipe_features.go:258-266` (G3 mandates).

### Phase 3 — Brief trees (both) + shape classification

- [ ] **3.1 — SSG showcase scenario atom.** `moderate`. **Deps: 0.1, 1.1.**
  - Files: `internal/recipe/content/briefs/feature/showcase_scenario_ssg.md` (NEW); `internal/recipe/briefs.go:691-693`.
  - Change: at `:691`, `if plan.Tier==tierShowcase { if isStaticTrack(plan) { atoms=append(atoms,"briefs/feature/showcase_scenario_ssg.md") } else { atoms=append(atoms,"briefs/feature/showcase_scenario.md") } }`. The SSG atom: 4-card dashboard (Items/DB built-at-build-time, Cache-of-built-data, Storage, Search), **no Queue card**, plus a rebuild-trigger note (scheduled/triggered rebuild replaces the live worker). Keep the existing `showcase_scenario.md` as the SSR variant **byte-unchanged** (no rename → preserves any test string-anchors on its path and its Parts-list cardinality).
  - RED: `internal/recipe/briefs_test.go` — `BuildFeatureBrief(showcase+static)` body contains "rebuild" and lacks "broker"/"Queue card"; `BuildFeatureBrief(showcase+dynamic)` body contains the SSR Queue/Broker card (`showcase_scenario.md:25`). Assert `worker_subscription_shape.md` is **absent** for the SSG plan (it self-excludes via `HasWorkerCodebase()` at `briefs.go:709` — assert, don't re-gate).

- [ ] **3.2 — Research guidance edit (v3 authoritative): mode choice + shape-1 static-monolith admission.** `moderate`. **No code deps; gate-test covered.**
  - File: `internal/recipe/content/phase_entry/research.md:46-69`.
  - Change (two parts):
    1. Replace the "Frontend default (shape 2/3 only)" paragraph (`:65-69`) with a mode-choice paragraph: SSG-capable frameworks (Astro, Next `output:export`, SvelteKit `adapter-static`, Nuxt static preset) set the UI codebase prod `run.base` to `static` (record via `Codebase.ProdRuntimeBase="static"` on the recipe side and prod `Type="static"` + `DevBase="nodejs@22"` on the target) ⇒ static track ⇒ 0 runtime managed at minimal, no broker at showcase; node-server frameworks (Next standalone, Nuxt nitro node, SvelteKit `adapter-node`, Remix/RR7, Angular `@angular/ssr`) leave prod `Type` at the node runtime ⇒ node-server track ⇒ current mandates. State the keystone per-framework artifact is the node adapter/output config.
    2. **Amend the classification (`:46-63`)** so a **static content-platform frontend** (Astro flagship) is admitted as **shape 1 monolith** with prod `run.base=static`, alongside the existing view-rendering-server shape-1 entries — reconciling the §4 "shape-1 expresses a static frontend" claim with the current text that reserves shape-1 for view-rendering servers (`:50-52`). Without this edit the agent routes Astro to shape 2/3 and the showcase-as-monolith target fails the shape gate (which needs a `RoleAPI` sibling for shape 2/3).
  - **No enum mention, no invariant ID** (atom authoring contract, CLAUDE.md).
  - RED: research.md edits are **content-only and covered by the gate tests** (1.1 SSG-showcase-as-shape-1-monolith passes; `gateShapeClassificationConsistency` accepts shape-1 + `RoleMonolith` + `ProdRuntimeBase=static`). There is **no research.md frontend-vocabulary pin** in `content_lint.go`/`content_lint_test.go` to extend (verified absent), so no conditional lint edit is needed.

- [ ] **3.3 — Rewrite v2 kind-table rows 2a/2b (NOT just a deprecation note).** `moderate`. **Lowest priority but more than cosmetic.**
  - File: `internal/content/workflows/recipe.md:42-48`.
  - Change: rewrite rows `2a Frontend static` / `2b Frontend SSR` (`:45-46`) to reference the SSG/SSR minimal/showcase ladder rather than pinning both to hello-world, and keep the field-contract sentence (`:38`, "submit exactly these keys — the schema rejects unknown properties") as the canonical `recipePlan` field-list doc. Even though tree-1 (v3) is what agents read, this table is the canonical field-contract surface and actively contradicts the new ladder; leaving the contradiction risks agent confusion when the v2 doc is consulted.
  - RED: **grep first** for string-anchors on these rows. `grep -rn "Frontend static\|Frontend SSR" internal/` and `grep -rn "2a\.\|2b\." internal/` to confirm no test pins the literal row text before editing (the maps warn of 100+ files anchoring on `hello-world`/`minimal`/`showcase`, but those anchor the *tier constants*, not these row labels). If a test pins the rows, update it in the same commit.

### Phase 4 — Adapter knowledge (keystone)

- [ ] **4.1 — Per-framework adapter atom.** `moderate`. **Deps: 3.2.**
  - File: `internal/recipe/content/briefs/research/adapter_knowledge.md` (NEW) — table from §7 below; loaded into the research brief.
  - Change: teach the static-output vs node-adapter config per framework, framed as "default deploy target is serverless; Zerops runs it as a long-lived container, so for SSR you select the node adapter that keeps a server alive." **This is recipe-specific framework knowledge — it lives in the recipe content tree / Strapi, never in atoms** (CLAUDE.md "Recipe-specific findings go in recipes, not atoms"; MEMORY "no framework-specific atoms").
  - RED: brief-composition test asserting the research brief body for a frontend framework includes the adapter table.

### Phase 5 — Corpus predecessors (Strapi, out-of-repo)

- [ ] **5.1 — Author `{framework}-{mode}-minimal`/`-showcase` recipes in Strapi**, pull via `zcp sync pull recipes`. `trivial` (engine-side: loader is slug-string-driven, no code change). **Deps: 1.1, 2.1, 2.2, 3.1.**
  - Caveat: until Strapi has them, frontend showcase chain resolution returns "no parent" (`parentSlugFor`, `chain.go:146-152`, is mode-agnostic and off-slug — see "Explicitly NOT changed") — coordinate content + code. The `mailpit.md` stopgap pattern (`.gitignore` `!`-allowlist) is available if one framework needs an in-repo commit to unblock CI before Strapi lands.

### Explicitly NOT changed (proven untouched)
- `internal/topology/*` — no new enum, `architecture_test.go` layering rules unaffected (recipe→topology import in `recipe_mode.go` is permitted; topology itself gains nothing).
- `internal/recipe/chain.go` — slug format + `parentSlugFor` (`:146-152`) + `parentTierForSlug` (`:195-204`) unchanged (mode is **off-slug**; `{framework}-{mode}-{tier}` keeps the existing `-showcase`/`-minimal` suffix, so `parentSlugFor("astro-ssg-showcase")` correctly strips `-showcase` → `astro-ssg-minimal`). Pinned by `TestResolveChain_ShowcaseToMinimalParent` staying green.
- `internal/recipe/plan.go` struct + `internal/workflow/recipe.go` `RecipeTarget`/`RecipePlan` structs — no new field (reuses `ProdRuntimeBase` recipe-side, `Type`+`DevBase` workflow-side).
- `internal/recipe/yaml_emitter.go` logic — already services-driven + static-aware via `prodRuntimeType` (`:500-505`); the only emitter work is the byte-golden re-baseline in Phase 1.2, not new branching.
- `gateShapeClassificationConsistency` (`phase_entry.go:269-317`) — mode-blind by design; only its *input* (the research-phase shape choice for Astro) changes via the research.md edit in 3.2.
- `tiers.go` (6 env tiers), `runtime_class.go` predicates, `cmd/zcp-recipe-sim/gate.go` (sim driver) — the sim's gate-set parity (`TestGatesForPhase_SimProdParity`) holds **because no new gate function is added**; mode is a branch inside existing gates, so `researchGates()` slice membership is byte-identical and the sim needs no change.

## 7. Per-framework adapter matrix

The irreducibly per-framework artifact. **Authoring home: the recipe content tree (`internal/recipe/content/briefs/research/adapter_knowledge.md`) + the per-recipe `.md` in Strapi (`internal/knowledge/recipes/`)** — *not* `internal/content/atoms/` (framework knowledge would explode the corpus and break axis-filter meaning, per CLAUDE.md + MEMORY).

| Framework | STATIC (SSG/SPA) output config | NODE-SERVER (SSR) adapter/output | Natural showcase mode |
|---|---|---|---|
| **Next.js** | `output: 'export'` (`next.config.js`); serves `out/` on `static` | `output: 'standalone'`; runs `.next/standalone/server.js` on `nodejs` | **SSR** |
| **Nuxt** | `nitro.preset: 'static'` / `nuxt generate`; serves `.output/public/` | nitro `node-server` preset; runs `.output/server/index.mjs` on `nodejs` | **SSR** |
| **SvelteKit** | `@sveltejs/adapter-static`; serves `build/` on `static` | `@sveltejs/adapter-node`; runs `build/index.js` on `nodejs` | **SSR** |
| **Astro** | default static / `output: 'static'`; serves `dist/` on `static` | `@astrojs/node` (mode `standalone`); runs `dist/server/entry.mjs` | **SSG** (content platform) |
| **Remix / React Router 7** | n/a (SSR-only in practice — single track) | RR7 node server (`@react-router/node` / Remix express); runs on `nodejs` | **SSR** (single recipe, no pair) |
| **Angular** | `ng build` (CSR SPA); serves `dist/<app>/browser` on `static` | `@angular/ssr` (`ng build` SSR + `server.mjs`); runs on `nodejs` | **SSR** |

Notes that belong in the adapter atom: the **default deploy target for all of these is serverless** (Vercel/Netlify/edge) — the recipe's job is to select the node adapter that produces a long-lived process and a `run.start` Zerops can keep alive. SSG variants need none of this (Nginx serves files); their per-framework artifact is the *build output directory* the deploy lands. For every node-server target, the recipe sets prod `Type=nodejs@…`; for every static target it sets prod `Type=static` + `DevBase=nodejs@…` (the dev/build container the compile happens in — `recipe.go:171-177`).

## 8. Blast radius & backward compat

### Pinned tests that MUST change (add cases, don't flip existing)
- `internal/recipe/phase_entry_test.go` `TestResearchGate_*` (`:304-466`): **add** SSG-minimal (0 svc pass; +1db fails with the *new* `ssg-minimal-no-runtime-managed` code), SSG-showcase (`{db,cache,storage,search}` pass; +broker fails `ssg-showcase-no-broker`), and SSG-showcase-as-shape-1-monolith. The `showcase-service-set-incomplete` pin (`TestResearchGate_RejectsDogfoodPathology`, showcase-missing-half) stays green via the default/dynamic track. **No existing assertion value changes.**
- `internal/workflow/recipe_validate_test.go` (G2): **add** SSG-showcase (no-worker, no-broker, no-queue-hostname) cases + the cross-gate agreement case (recipe `ProdRuntimeBase` vs workflow `Type`); keep SSR cases (worker + queue + 5 kinds) as regression pins.
- `internal/workflow/recipe_features_test.go` (G3): **add** SSG-showcase coverage = `{api,ui,db,cache,storage,search}` passes, declares-`queue`/`worker` fails; keep SSR coverage (worker+queue mandatory) as regression.
- `internal/recipe/validators_worker_subscription_test.go` (G4): **add** SSG-showcase-no-worker ⇒ zero violations; keep SSR-separate-worker enforcement.
- `internal/recipe/briefs_test.go`: **add** mode-aware scenario-load assertions; the existing `worker_subscription_shape` backend-pass test stays green (SSG plans have no worker codebase; the `HasWorkerCodebase()` guard already excludes it).
- `internal/recipe/yaml_emitter_test.go` (Phase 1.2): **add** SSG-omits-broker byte-golden + no-dangling-comment cases; SSR goldens unchanged.
- New files: `recipe_mode_test.go`. New atom: `showcase_scenario_ssg.md`, `adapter_knowledge.md`.

### Pinned tests that must NOT change value (verify still green)
- `chain_test.go` `TestResolveChain_ShowcaseToMinimalParent` — mode off-slug, `-showcase`/`-minimal` suffix preserved ⇒ unchanged.
- `tiers_test.go` `TestTiers_Count` / `TestTiers_FolderAndSuffix` — 6 env tiers orthogonal ⇒ unchanged.
- `internal/topology/runtime_class_test.go` `TestRuntimeClassFor`, `internal/topology/architecture_test.go` `TestArchitectureLayering` — no topology edit ⇒ unchanged.
- `phase_entry_test.go` `TestGatesForPhase_SimProdParity` — `researchGates()` slice membership unchanged (no new gate fn; the sim driver `cmd/zcp-recipe-sim/gate.go` needs no edit **because** mode is a branch inside existing gates) ⇒ unchanged.
- `gate_named_constants_consistency_test.go` — SSR showcase still cites the same `NATS_QUEUE_GROUP`; SSG showcase simply has no queue to cite (its `EnvComments` carry no broker-keyed entry — enforced by Phase 1.2).

### What must NOT break
- Every existing backend minimal/showcase recipe (laravel, nestjs): the node-server/default branch of all four gates is byte-for-byte the current code path. SSR frontend showcase = same 5-service set + worker + queue. **Regression-free by construction** because the *only* new behavior is the static-track branch, which no existing committed recipe takes (none sets a UI-codebase `ProdRuntimeBase="static"` / app target prod `Type="static"` at showcase tier).

### Two-content-tree duplication tax
- v3 (`internal/recipe/content/`) is authoritative — edit `research.md` + add `showcase_scenario_ssg.md` + `adapter_knowledge.md` here. v2 (`internal/content/workflows/recipe.md`) gets the rows-2a/2b rewrite (§3.3) because it is the canonical field-contract surface, not merely a deprecated label. **Do not author SSG scenario content in both** — agents read v3 for briefs.

### Gitignored-corpus sync caveat
- Frontend `{framework}-{mode}-{tier}` recipes live in Strapi (`internal/knowledge/recipes/` is gitignored, `.gitignore:42-53`). Code can ship before content, but frontend showcase chain resolution returns no parent until Strapi has the minimal predecessor. The loader needs no change (slug-string-driven). Sequence: merge engine → author Strapi recipes → `zcp sync pull` → `cache-clear`.

## 9. Effort & sequencing (quality-not-walltime framing)

Phase the work so each increment is independently reviewable and proves a quality property:

1. **Increment 1 — Spine proof (Phases 0+1+2, ALL FOUR gates + YAML determinism).** The `recipeMode` predicate + G1/G2/G3 mode-branches + G4 self-exclusion assertion + the SSG-omits-broker golden. Reviewable as: "does the engine correctly *refuse* an SSG showcase with a broker, and *accept* one without, on **all four** validation paths, with the recipe gate and the two workflow gates reading the **same prod-`Type` fact**, with zero backend regression and byte-stable emission?" This is the load-bearing intelligence claim — it proves the **quad**-gate stays coherent and the cross-gate desync (C4) is structurally killed. **MVP first increment.**
2. **Increment 2 — One framework end-to-end (minimal-SSR, then minimal-SSG for a dual-mode framework, e.g. SvelteKit).** Author the Strapi recipe pair + adapter atom (Phases 3+4+5 for one framework). Proves the config-teaching pair teaches the actual platform boundary (static vs node `run.base`), verified by the emitted import.yaml differing exactly on `run.base`/`services[].type` and service set. Use browser verification at the close step (MEMORY: browser verification for showcases).
3. **Increment 3 — Natural-mode showcase for the same framework** (SvelteKit-SSR-showcase). Proves the mode-aware showcase gate + SSG scenario atom against a real recipe.
4. **Increment 4 — Fan out** to Next/Nuxt/Astro/Remix/Angular, one recipe per increment, each a content-only Strapi PR.

**Minimum viable first increment that proves the spine before fanning out:** Increment 1 (Phases 0+1+2) + a single `astro-static-showcase` synthetic plan fixture in the gate tests — **but only after §3.2's shape-1-static-monolith classification edit lands**, because Astro-as-shape-1-monolith is exactly the combination the current shape gate + research classification do not admit (without 3.2 the fixture survives G1/G2/G3 yet contradicts the shape-classification guidance, so it is not a clean spine proof). Sequence: land 0.1 + 1.1 + 2.1 + 2.2 + 3.2 together, then the Astro fixture exercises the new static-track branch on the showcase gate directly with no dual-mode ambiguity. Validate the quad-gate-coherence finding through `codex:rescue` before declaring it correct (MEMORY: validate findings with codex).

## 10. Open decisions for the owner

1. **Does "minimal" require a managed service for SSG, and is SSG-minimal distinct from hello-world?** Approach A makes **SSG-minimal require 0 managed services** (data baked at build, no runtime DB), which by *service count alone* is identical to hello-world (`phase_entry.go:233` vs the proposed SSG-minimal branch). The discriminator is therefore `(Tier==tierMinimal AND isStaticTrack)` + the slug suffix, **not** the count. Fork: (a) SSG-minimal=0 managed, distinct code `ssg-minimal-no-runtime-managed`, distinct slug `-minimal` (recommended — matches "no runtime DB" intent), or (b) collapse SSG-minimal into hello-world (no separate tier). This is the single decision that most shapes the gate. *Recommendation: (a).*

2. **One showcase per framework, or per-mode?** Target model says **one flagship in the natural mode**. Confirm we do *not* author both `next-ssg-showcase` and `next-ssr-showcase` — only the natural one (Next→SSR, Astro→SSG). The minimal *pair* still exists for dual-mode frameworks; the *showcase* is singular.

3. **Where does adapter knowledge live?** Recommended: recipe content tree + Strapi per-recipe `.md`, **not atoms** (CLAUDE.md + MEMORY "no framework-specific atoms"). Confirm.

4. **Mode-aware gate vs a new tier?** Recommended: **mode-aware gate** (Approach A), no new tier const, mode off-slug (suffix stays `-showcase`/`-minimal`). The alternative (new `tierStaticShowcase` etc.) touches `chain.go` parent resolution + ~120 grep sites and is rejected here. Confirm.

5. **Does SSG showcase forbid `broker` or merely not require it?** Recommended: **forbid** (fail closed — `ssg-showcase-no-broker` in G1, reject-if-present in G2, forbid-`queue`-surface in G3), because a broker on a build-time-only app is a teaching lie. Owner could choose "permit for experimentation," but that weakens the mode-as-config teaching and breaks gate symmetry.

6. **Per-codebase vs per-recipe mode for shape-2.** Approach A keys on the *first* UI codebase (recipe side) / app target (workflow side). A shape-2 recipe with a static frontend beside a node API is currently ill-defined. Recommended: declare shape-2 frontend-mode out of scope for the first cut (one mode per recipe); revisit only if a real recipe needs it.

7. **Cross-gate mode-derivation contract (the C4 fix).** The recipe gate reads `Codebase.ProdRuntimeBase`; the workflow gates read `RecipeTarget.Type`. They agree only if an SSG plan sets prod `Type=static` on the UI/app target (with `DevBase=nodejs@22`). Confirm the contract: **prod `Type` is the single source of truth both gates classify; `ProdRuntimeBase` mirrors it recipe-side.** §2.1 enforces the agreement assertion. Owner must accept that the agent declares the static signal in *both* fields at research time (engine re-derives `ProdRuntimeBase` at stitch as a backstop).

8. **Which shape does an SSG content-platform showcase take?** Target says **shape 1 monolith**. Current research classification (`research.md:50-52`) reserves shape-1 for view-rendering servers and routes frontends to shape 2/3. Recommended: edit research.md (§3.2 part 2) to admit a static frontend-monolith as shape 1; the shape gate (`phase_entry.go:289-295`) already accepts shape-1 + `RoleMonolith` mechanically, so the only change is the guidance. Confirm.

## 11. What this is NOT / non-goals

- **Not edge/serverless runtime.** Zerops runs long-lived containers; the node-server adapter selection is *specifically* to avoid the serverless default. No `RenderModeEdge`.
- **Not per-route SSG/SSR mixing.** Per-route hybrid (Next App Router partial prerendering, Astro islands at the route level) **hides the config boundary**, which defeats the recipe's teaching purpose. The recipe teaches the *binary* (one mode per recipe).
- **Not rebuilding the api/app split.** Shape 1/2/3 and the 4 roles (`roles.go`) are unchanged in *definition*; only the research-phase *classification guidance* for static content-platforms is edited (§3.2). Mode is a service-consumption/runtime axis, **not a new role** — adding a role would break the "4 platform contracts" invariant.
- **Not a new `topology.RenderMode` enum** (Approach B rejected). The static/node binary is already `RuntimeClass` (5-valued; the static predicate uses `RuntimeStatic`, the node predicate uses `!isStaticTrack`).
- **Not a slug-format change.** Mode stays off-slug; the `-showcase`/`-minimal` suffix is preserved so `chain.go` is untouched.
- **Not touching the 6 environment tiers** (`tiers.go`) — orthogonal to recipe tier and to mode.
- **Not a build-time worker as a runtime service.** SSG's "scheduled rebuild" is a build-phase concept (cron/trigger → rebuild), not a long-lived broker-consuming worker. The SSG scenario atom must not teach a runtime worker, and G4 stays inert for SSG (no separate worker codebase).

---

**Files central to execution:** `internal/recipe/recipe_mode.go` (new), `internal/recipe/phase_entry.go:239-264` (G1), `internal/workflow/recipe_validate.go:331-376` (G2), `internal/workflow/recipe_features.go:256-298` (G3), `internal/recipe/validators_worker_subscription.go:387-393` (G4, assert), `internal/recipe/yaml_emitter_test.go` (byte goldens), `internal/recipe/briefs.go:691-693`, `internal/recipe/content/briefs/feature/showcase_scenario_ssg.md` (new), `internal/recipe/content/phase_entry/research.md:46-69`, `internal/recipe/content/briefs/research/adapter_knowledge.md` (new), `internal/content/workflows/recipe.md:42-48`. **Read-only foundation reused:** `internal/topology/types.go:7-8`, `internal/topology/runtime_class.go:28-43`, `internal/recipe/plan.go:160-170,213-223`, `internal/recipe/yaml_emitter.go:500-505`, `internal/workflow/recipe.go:90,150-193`.

---
*Synthesized via multi-agent workflow; codebase facts grounded in parallel source reads, adversarially reviewed.*
