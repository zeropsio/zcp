# Implementation Plan: `shape: software` OSS-Recipe Authoring

> ⚠️ **SUPERSEDED (2026-06-09) — NOT the plan of record.** The plan of record for OSS recipes is
> **`plans/oss-recipe-port-flow-2026-06-09.md`** (autonomous port-and-debug flow, peer to `develop`).
> This document's core premise — extending the `internal/recipe` *scaffold engine* with a
> `shape: software` route — was rejected (see §0.5). Only **D4** (recipe-level presentation
> fragments) and **D6** (buildFromGit override) survive, reused by the port flow's Stage B capture.
> Kept for that design detail + the verified contract facts (paired markers, secret taxonomy); do
> not implement the phase-engine extension.

**Date**: 2026-06-09
**Task**: Add `shape: software` (self-hosted OSS) authoring to the ZCP recipe engine.
**Method**: team-analyze, Deep (2 KB agents → planner → adversarial → synthesis).
**Reference**: `docs/recipes/fe-handover.md` (frontend contract, authoritative for the seam),
`docs/recipes/recipe-taxonomy.md` (legacy 7-type model, OSS = types 6-7).

---

## 0. The reframe that drives everything

The naive framing — "software recipes have no authored source" — is **false**, and the
adversarial pass proved it from the platform contract:

- Every `buildFromGit` target **must contain a `zerops.yaml`** (fe-handover §11 line 356;
  `topology/repo_url.go` clone-preflight). Upstream OSS repos (`umami-software/umami`,
  `ghost/Ghost`) do **not** ship one.
- Therefore a software recipe **still needs a repo we control** (the `zeropsio/recipe-<slug>`
  glue repo) holding the `zerops.yaml` + build script (curl a prebuilt binary, or clone+build
  upstream) + config. `internal/knowledge/recipes/mailpit.import.yml:7` is the live proof:
  `buildFromGit: https://github.com/zeropsio/recipe-mailpit` — a glue repo, not the app's
  source.

**So `shape: software` means: the recipe repo is glue-config, not scaffolded app feature-code;
the thing the user owns is the DATA (managed DB / object-storage / shared-storage), not the
code.** [VERIFIED: mailpit.import.yml, repo_url.go, fe-handover §11]

This reframe changes the work from "skip authoring" to "author *different* content through a
*pruned* phase route":

| Dimension | `shape: app` (today) | `shape: software` (new) |
|---|---|---|
| Recipe repo holds | scaffolded app feature-code + `zerops.yaml` | glue `zerops.yaml` + build/config only |
| Feature phase | runs (crud/cache/queue/storage/search) | **SKIPPED** — nothing to author |
| Per-codebase surfaces | README + CLAUDE.md + IG + KB at SourceRoot | trimmed: no app-CLAUDE; KB→operate notes |
| Take-ownership framing | fork → CI/CD → push | platform `SOFTWARE_TAKEOVER_GUIDE` (persist→configure→domain→backups→upgrade→scale) |
| `buildFromGit` URL | derived `zerops-recipe-apps/<slug>-<role>` | author-supplied glue-repo URL |
| User owns | the code | the data |

---

## 0.5 — DECISIVE UPDATE (2026-06-09): there are TWO authoring paths, and software already has one

The premise of §1-§8 below — "extend the `internal/recipe` phase engine to author
`shape: software`" — is now in question. Evidence: the polished reference recipe (`fxck/zrno-shop`)
was **not** produced by the `internal/recipe` engine. It was authored by a **`recipe-authoring`
skill** (a markdown prompt that instructs an agent to compose a `.zerops-recipe/` folder;
`gist.github.com/fxck/198dfeaf1d40070727b5061885f8cb95`), then hand-corrected.

There are therefore **two authoring paths**, and they suit different recipe kinds:

| | `internal/recipe` PHASE ENGINE | `recipe-authoring` SKILL |
|---|---|---|
| Shape | built for `app` (scaffold + iterate real source in a live SSHFS container) | **shape-agnostic — already covers `app` AND `software`** (layout C "multi-service / OSS glue") |
| Model | research→scaffold→feature→…→finalize, gated, deterministic | "compose-by-presence; intent follows fragments" — content + glue, no source loop |
| Strength | provisioning, source scaffolding, feature showcases, **validated/golden multi-tier emit** | low-friction, flexible, **already produces shipping recipes incl. software** |
| Weakness | heavyweight; assumes app source at a `dev` SourceRoot | agent hand-writes 6 import.yamls **with no validation/consistency gate** |

**Why this matters for "will it break existing flows?":** a `software` recipe is a glue + prose
composition with **no source-iteration loop** — which is exactly the skill's wheelhouse and the
opposite of what the phase engine is for. If software authoring stays in the skill, the answer to
the regression question is trivial: **zero change to `internal/recipe` ⇒ zero risk to the app
flow.** The skill already does it.

**The real top-level decision (supersedes the D1-D6 mechanics below):**
- **(A) Skill owns software.** No engine change. Lowest risk, works today, but software recipes
  are unvalidated (the agent must get all 6 tiers' topology + the secret taxonomy right by hand).
- **(B) Extend the phase engine** (the §3 D1-D6 plan). Gives validation/determinism, but is the
  heavy, regression-surfaced path — and duplicates what the skill already does.
- **(C) Hybrid / fix-upstream.** Extract the genuinely *shape-agnostic* machinery the engine
  already has — multi-tier import.yaml topology generation, paired-marker assembly, the
  `<@generateRandomString>`/`#zeropsPreprocessor` + secret-taxonomy emit, schema validation —
  into primitives the **skill** can lean on. The heavy phase flow stays `app`-only (untouched);
  software is skill-authored but gains the engine's guarantees. No software branch threaded
  through the app engine ⇒ no app-flow regression.

Recommendation: **(A) now, evolving to (C)** if/when OSS recipe volume justifies validation —
**not (B)**. The §1-§8 plan below is retained as the (B) design *if* that path is chosen, but it
is no longer the default.

> Skill-confirmed contract facts (strengthen the §6 OQs): the skill teaches the **paired**
> `<!-- #ZEROPS_EXTRACT_START:NAME# -->` markers (OQ-1 ↑), **service-scope `envSecrets` with
> bare-`null` ⇒ wizard prompt** as a first-class idiom (OQ-2: service-scope IS part of the
> contract — `zrno-shop` simply chose project-only), `cover` = a committed real screenshot at a
> raw URL (OQ-4 ✓), the `name→tier` fuzzy match + "don't ship a tier whose guide you can't honor"
> (OQ-5 ✓), and the top-level repo `README.md` is **not** read (only `.zerops-recipe/README.md`).
> The skill is itself the closest thing to an authoritative client-side contract we have.

---

## 1. Goal

Enable `internal/recipe/` to author `shape: software` recipes end-to-end: a shape lever set at
research time, a pruned phase route (no feature authoring), engine-authored glue `zerops.yaml`,
the recipe-level presentation fragments the frontend reads (`name`/`cover`/`description`/
`features`/`shape`/`takeover-guide`/`knowledge-base`), an `envSecrets` secret taxonomy that
drives the deploy wizard, and a discovery signal so the agent can explain app-vs-software to the
user. The frontend already renders `shape: software`; the engine does not yet produce it.

---

## 2. Confidence summary

- **VERIFIED** (read in code this session): the 4 gaps, the SourceRoot invariant and its
  unconditional call sites, the hardcoded `buildFromGit`, the absence of a `Shape` field, the
  absence of a showcase-coverage gate, the paired-marker mechanics, mailpit as the glue
  precedent, the platform topology knobs.
- **VERIFIED against a real recipe** (`github.com/fxck/zrno-shop`, dissected 2026-06-09):
  paired recipe-level markers (8 fragments), the embedded-glue utility pattern (mailpit —
  `buildFromGit` on a *utility* service the engine cannot emit today), the multi-repo model
  (recipe ≠ source ≠ cover), a `project.envVariables`-only secret model with per-variable
  generated lengths + literal presets + omitted optionals, folder-name divergence tolerated by
  frontend fuzzy-match, and a coexisting legacy `recipe/` flat format proving the example is
  **hand-authored — a quality-bar reference, not engine output**.
- **LOGICAL**: the phase-route pruning and the glue-repo ownership model follow from the
  contract + mailpit.
- **UNVERIFIED / BLOCKING** (cannot be settled from this repo — see §6): the `/api/recipe/info`
  marker form for *recipe-level* fragments; whether `services[].envVariables` (vs `envSecrets`)
  is a valid import field; the glue-repo URL/commit ownership model.

---

## 3. Design decisions (reconciled with the adversarial pass)

### D1 — `shape` is a research-time decision, single-sourced, surfaced three ways
Add `Shape string` to `ResearchResult` (`plan.go:172-180`), values `app|software`, empty ⇒
`app` (matches frontend default, fe-handover §3a line 79). It is NOT derived from
`CodebaseShape` (which is app-codebase *topology*, an orthogonal axis). The same value surfaces
in three places, each its own change:
1. **authoring** — `ResearchResult.Shape`, gated.
2. **discovery** — `RecipeMatch.Shape` in `workflow/route.go` (read from corpus frontmatter), so
   the agent can explain "Umami (software: self-host) vs NestJS (app: fork & push)". [Fixes MF#1]
3. **emitted artifact** — the `<!-- shape -->` token in the recipe-repo root README, written by
   the engine, **not** authored by an LLM (see D4).

*Rejected*: deriving shape from codebase count / a remote flag — couples a recipe-level operation
choice to implementation detail and leaves discovery blind.

### D2 — Drop the "exactly 1 codebase" invariant; prune the phase route by shape
The planner's "shape=software ⟺ exactly 1 RemoteSourced codebase" is **wrong**: multi-runtime
OSS exists (app + worker), and managed deps are `Services[]`, not `Codebases[]`
(`plan.go:229-241`). [CH#1]

Correct model:
- `shape: software` ⟹ **all** `Codebases[]` are glue-sourced (no per-codebase flag to desync —
  derive `plan.IsSoftware()` from `ResearchResult.Shape`). No cardinality gate.
- **Skip the Feature phase entirely** for software. Feature authors app showcase code
  (`briefs.go:627-761`, kinds at `plan.go:41-46`); a glue recipe has none. mailpit has zero
  feature content — the canonical proof. [CH#2]
- Software route: `Research → Provision → Scaffold(glue) → CodebaseContent(trimmed) →
  EnvContent → Finalize → Refinement`. Phase dispatch branches on `plan.IsSoftware()`.

*Rejected*: a fully parallel phase enum — duplicates gates and the sync skeleton; the existing
phases already branch per-codebase, so a shape predicate at the dispatch sites is the smaller,
in-grain change.

### D3 — Scaffold authors GLUE, reusing SourceRoot; trim app-source surfaces, don't bypass them
The adversarial showed SourceRoot handling is unconditional at **three** sites, not one:
`validateCodebaseSourceRoots` (`handlers.go:2278-2293`), `populateSourceRootsForScaffold`
(`~handlers.go:785`), and `stitchCodebaseContent`'s unconditional `README.md`/`CLAUDE.md` writes
(`~handlers.go:2399`). [CH#4]

Because software still needs an authored glue `zerops.yaml` in a repo we control [§0], the
lower-risk cut is **keep the SourceRoot** (the glue repo's working dir) and reuse the
provision+scaffold+finalize machinery — but:
- Scaffold's brief authors a **glue `zerops.yaml`** (base + `build.prepareCommands`/
  `buildCommands` to curl/clone+build upstream + `deployFiles` + `run.start`; mailpit pattern)
  and config — **no app feature-code, no CLAUDE.md app framing**.
- `CodebaseContent` for software skips the app-CLAUDE surface and reshapes KB toward operate
  notes; per-codebase IG becomes optional.

*Rejected*: the planner's "skip SourceRoot validation, RemoteSourced=true codebases have empty
SourceRoot" — it leaves the glue `zerops.yaml` with nowhere to live and trips the phantom-write
paths CH#4 found. **This is the central open design question** — see OQ-3: v1 reuses the
workspace; a lighter "author-glue-without-a-dev-container" path is a possible v2.

### D4 — Recipe-level presentation fragments: new surfaces, but `shape` is engine-stamped
Add recipe-level fragment IDs + surface contracts for `name`, `cover`, `description`,
`features`, `takeover-guide`, `knowledge-base` (`surfaces.go`, `handlers_fragments.go:200-256`).
Each needs a registered `SurfaceContract` (line cap + validator) or `record-fragment` fails.
[MF#3] In particular `takeover-guide` must validate **FLAT — no `##`/`###` heading** (fe-handover
§7 line 206); a generous line cap that invites outline-packing would violate the contract.

`shape` is **engine-stamped** from `ResearchResult.Shape`, not an authored fragment — it is one
token with no prose, so a validator/voice-gate/line-cap is over-engineering. [Refines planner D4]
`cover` requires an image **asset**, which the engine has no path for today — treat the cover
image as an author/asset input, not generated prose (OQ-4).

### D5 — secret emit: enrich `project.envVariables` first; `envSecrets` second
**PRIMARY (blocking).** The engine emits exactly ONE generated secret at fixed length 32
(`yaml_emitter.go:138-140`). Real recipes need: (a) **multiple** generated secrets at
**per-variable lengths** (`zrno-shop`: `BETTER_AUTH_SECRET`=48, `ADMIN_PASSWORD`=16 dev / 20
prod), (b) **literal presets** (`ADMIN_EMAIL: admin@zrno.cz`), (c) **intentionally-omitted
optionals** (Stripe left unset). The `#zeropsPreprocessor=on` shebang is already emitted, gated on
`NeedsAppSecret` (`yaml_emitter.go:90-94`) — generalize the gate to "any `<@…>` present."

**SECONDARY (OQ-2-gated).** Service-scope `services[].envSecrets` with the bare-`null`⇒prompt /
`""`⇒optional / generated taxonomy + sensitive-name auto-masking (`PASSWORD|SECRET|TOKEN|…`). The
`recipe-authoring` skill teaches this as a first-class idiom (so it IS part of the contract), but
`zrno-shop` ships entirely on `project.envVariables` with **zero** bare-null prompts — so the
service-scope path is real but not the evidenced bottleneck. A validator pins both tiers.

### D6 — `buildFromGit` override (TWO emit sites)
Add `ResearchResult.GlueRepo string`; write `services[].buildFromGit` from it (canonicalized via
`topology.CanonicalRepoURL`) at **both** sites: (1) `writeRuntimeBuildFromGit` for runtime
codebases (`yaml_emitter.go:461`, today hardcoded `RecipeAppRepoBase`+slug+suffix); **and (2) the
`writeNonRuntimeService` `ServiceKindUtility` branch (`yaml_emitter.go:413-415`), which today
emits `zeropsSetup`+autoscaling but NO `buildFromGit` at all** — so the embedded-glue utility
pattern (mailpit: `type: alpine@3.20` + `buildFromGit: zeropsio/recipe-mailpit`, present in all 6
`zrno-shop` tiers) is **unemittable today**. This is the canonical micro-model for a software
recipe's primary service. For app recipes the derived runtime path is unchanged.

---

## 4. Phased steps (each independently green; TDD RED-first; verify before next)

> Phase 0 is a **blocking external spike** — do not build §D4/§D5 emit on an unconfirmed parser
> contract.

### Phase 0 — De-risk the seam (BLOCKING, no code)
Resolve OQ-1/OQ-2/OQ-3 with the frontend/platform owners + an empirical `/api/recipe/info`
probe (fe-handover §11 preview URL) on a hand-built `.zerops-recipe/` with recipe-level markers.
Confirm: (a) does the backend extract recipe-level `name`/`shape`/`cover` via the paired
`<!-- #ZEROPS_EXTRACT_START:NAME# -->` form, or single `<!-- name -->`? (b) is
`services[].envVariables` valid or only `envSecrets`? (c) the glue-repo ownership/commit model.
**Output: a confirmed marker form + secret-block decision.** Everything downstream keys off this.

### Phase 1 — `shape` field + research gates
- Files: `plan.go` (ResearchResult), `phase_entry.go`.
- RED: `TestGateRecipeShapeSet_Valid` (app/software/"" pass), `..._Invalid` (else fails);
  `TestResearchResult_Shape_DefaultsApp`.
- No behavior change for app recipes (empty ⇒ app).

### Phase 2 — `plan.IsSoftware()` + SourceRoot/stitch conditioning
- Files: `plan.go` (predicate), `handlers.go` (the three sites in D3).
- RED: `TestValidateCodebaseSourceRoots_SoftwareUsesGlueRoot`,
  `TestStitchCodebaseContent_Software_SkipsAppClaude`.
- Dep: Phase 1. Lands before emit so finalize doesn't reject software.

### Phase 3 — Skip Feature phase for software
- Files: phase dispatch (`workflow.go` / `phase_entry.go`), `briefs.go`.
- RED: `TestPhaseRoute_Software_SkipsFeature`, `TestBuildFeatureBrief_NotDispatchedForSoftware`.
- Dep: Phase 1.

### Phase 4 — Recipe-level surfaces + fragment IDs + contracts
- Files: `surfaces.go`, `handlers_fragments.go`.
- RED: `TestSurfaceFromFragmentID_RecipeLevel` (name/cover/description/features/takeover-guide/
  knowledge-base → surfaces), `TestValidateFragmentID_RecipeLevel`,
  `TestSurfaceContract_TakeoverGuide_RejectsHeadings` (flatness, fe-handover §7).
- Dep: Phase 0 (marker NAME tokens must match the confirmed backend vocabulary).

### Phase 5 — Root README assembler + engine-stamped `shape`
- Files: `assemble.go`, `handlers.go` (finalize root-assembly).
- RED: `TestAssembleRootREADME_SubstitutesRecipeFragments`,
  `TestAssembleRootREADME_StampsShapeToken`, `TestAssembleRootREADME_MissingFragmentGated`.
- Dep: Phase 4.

### Phase 6 — YAML emitter software branch: `buildFromGit` override + `envSecrets`
- Files: `yaml_emitter.go`, `plan.go` (ResearchResult.GlueRepo), new `validators_envsecrets*.go`.
- RED: `TestEmitDeliverable_Software_BuildFromGitOverride`,
  `TestEmitDeliverable_Software_EmitsEnvSecrets`,
  `TestEmitDeliverable_EnvSecretTaxonomy` (bare⇒prompt, ""⇒optional, generate/ref/literal⇒silent),
  `TestEmitDeliverable_PreprocessorShebangWhenGenerated`.
- Dep: Phase 0 (envSecrets vs envVariables decision), Phase 2.

### Phase 7 — Glue scaffold brief + discovery shape signal
- Files: `briefs.go` (glue-scaffold brief: build script / prebuilt-binary / clone-upstream
  patterns), `workflow/route.go` (`RecipeMatch.Shape`).
- RED: `TestBuildScaffoldBrief_Software_AuthorsGlueYaml`,
  `TestRecipeMatch_CarriesShape`, `TestRouteOptions_Software_ExplainsTradeoff`.
- Dep: Phases 1-3.

### Phase 8 — Integration + tier applicability
- Files: `integration/recipe_software_test.go`; tier-loop conditioning if OQ-5 says software
  drops `ai-agent`/`remote-cde`.
- RED: `TestRecipeSoftware_FullRun_ResearchToPublish` (research shape=software → glue scaffold →
  no feature → finalize emits per-tier import.yaml with buildFromGit + envSecrets + root README
  with all recipe-level fragments).
- Optional e2e: deploy a real OSS glue recipe (Umami) to dev Zerops; verify data survives a
  redeploy (the "you own the data" claim).

---

## 5. Risk assessment

| # | Risk | L | Impact | Mitigation |
|---|---|---|---|---|
| R1 | **Marker-parser acceptance at recipe level unconfirmed.** Both `zrno-shop` and the skill emit *paired* `#ZEROPS_EXTRACT_START#`; the single-marker contingency is **retired**. Residual risk is only whether the backend `/api/recipe/info` parser populates recipe-level `name`/`shape`/`cover` from the paired form (inferred from a hand-authored example + skill, not observed rendered). | M | CRITICAL | Phase 0 confirmation probe of `/api/recipe/info` on a hand-built `.zerops-recipe/`; no single-marker emit path needed. |
| R2 | **Glue `zerops.yaml` ownership undefined.** If `GlueRepo` points at upstream OSS, it has no `zerops.yaml`; the engine must author + commit one to a repo we control. | H | CRITICAL | D3 + OQ-3: v1 authors glue into the `zeropsio/recipe-<slug>` repo via the scaffold workspace, mailpit-style. Confirm the org/commit flow before Phase 7. |
| R3 | `services[].envVariables` may be invalid (docs show only `services[].envSecrets` + `project.envVariables`). | M | HIGH | Phase 0; validate test YAML through the live import schema. |
| R4 | SourceRoot unconditional at 3 sites — partial conditioning leaves phantom writes / false rejects. | M | HIGH | Phase 2 covers all three sites with explicit tests. |
| R5 | Feature-phase skip leaks (a gate still expects feature facts for software). | M | MEDIUM | `TestPhaseRoute_Software_SkipsFeature` + audit feature-fact gates for a shape branch. |
| R6 | Tier topology: `ai-agent`/`remote-cde` tiers are code-authoring loops, arguably meaningless for self-host OSS. | M | LOW-MED | OQ-5 product decision; emitting all 6 is *valid* (just topology variants), so this is polish, not a blocker. |

---

## 6. Open questions (need external confirmation — track in Phase 0)

- **OQ-1 (marker form)** — ✅ **RESOLVED / VERIFIED (2026-06-09 live-backend probe).**
  `GET api.app-prg1.zerops.io/api/rest/public/recipe/info?url=github.com/fxck/zrno@main` → HTTP 200,
  `errors: null`, `extracts` = all 8 recipe-level fragments (`name`, `shape`, `intro`, `cover`,
  `description`, `features`, `takeover-guide`, `knowledge-base`) parsed from the **paired**
  `#ZEROPS_EXTRACT_START:NAME#` markers (`shape: "app"`, all 6 em-dash tiers resolved). The backend
  parser accepts the paired form for recipe-level fragments — matching the engine's only marker
  constants (`assemble.go:31-36`). **The single-marker contingency is dead; the Phase-0 blocking
  spike is closed.** R1 no longer a blocker.
- **OQ-2 (secret block)** — is `services[].envVariables` a valid import field, or only
  `envSecrets` at service scope + `envVariables` at project scope?
- **OQ-3 (glue-repo ownership)** — where does the engine-authored glue `zerops.yaml` get
  committed, and under what org/naming? Reuse scaffold workspace (v1) vs container-less authoring
  (v2)?
- **OQ-4 (cover asset)** — software recipes need a cover image; the engine has no asset path.
  Author-supplied?
- **OQ-5 (tiers for software)** — do software recipes use all 6 env tiers or a subset
  (likely drop `ai-agent`/`remote-cde`)?
- **OQ-6 (`<@generateRandomString>`)** — undocumented in public Zerops docs; confirm directive
  signature + the `#zeropsPreprocessor=on` requirement against the live platform if recipes rely
  on it.

---

## 7. Change-impact (CLAUDE.md TDD tiers)

| Layer | Tests | Package |
|---|---|---|
| unit | shape gates, IsSoftware, SourceRoot/stitch conditioning, recipe surfaces + contracts, root assembler, emitter (buildFromGit + envSecrets taxonomy + shebang), takeover flatness | `internal/recipe` |
| unit | `RecipeMatch.Shape` + route options tradeoff copy | `internal/workflow` |
| tool/integration | full software recipe run research→publish; refinement re-stitch of recipe-level fragments | `integration/` |
| e2e (opt) | deploy real OSS glue recipe; data survives redeploy | `e2e/ -tags e2e` |

New invariants to pin (CLAUDE.md "new invariant → add bullet + test"): shape lever + default;
software prunes Feature; glue-`zerops.yaml` is engine-authored (software ≠ no source); recipe-level
fragment vocabulary + `takeover-guide` flatness; `envSecrets` taxonomy; `buildFromGit` override
canonicalization.

---

## 8. Effort shape (not wall-time)

Smallest/safest first: D1 shape field + gate, D6 buildFromGit override, D5 envSecrets are
mechanically small and isolated. The real lift is **D2/D3** (phase-route pruning + glue scaffold
without breaking the app path) and **D4/Phase 0** (recipe-level fragments gated on the
unconfirmed seam). Do not start D4 emit until Phase 0 resolves OQ-1. Quality bar over speed.
