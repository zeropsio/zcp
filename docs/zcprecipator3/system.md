# zcprecipator3 — system overview

**Read this first.** Compact, current, anchored. A fresh instance reads
this in 5–8 minutes and has the whole north star — what the system is,
what it produces, how a run flows, and (crucially) which knowledge the
engine is allowed to know up-front vs which knowledge each run is
responsible for discovering.

This doc complements [plan.md](plan.md) (design history, week-by-week
delivery plan, what v3 is explicitly NOT doing) and
[../spec-content-surfaces.md](../spec-content-surfaces.md) (per-surface
content contracts + classification taxonomy). When this doc and an
older run-readiness plan disagree, this doc is authoritative.

---

## 1. What v3 is

A **recipe** is a click-deploy template published to `zerops.io/recipes`.
A porter lands on the page, picks their framework, clicks "Deploy on
Zerops," and gets a working application running on their own Zerops
project — with their own subdomains, their own secrets, and the
per-codebase apps repo cloned to their dev mount.

A successful recipe authoring run produces:

- A **6-tier deliverable** (`environments/0..5/import.yaml`) — the
  click-deploy bytes the platform reads.
- An **apps repo per codebase** — README, CLAUDE.md, `zerops.yaml`, and
  source code that the porter clones and runs.
- **Captured discoveries** routed to the right surface so the porter
  inherits per-recipe knowledge the agent learned during the run.

zcprecipator3 (v3) is the **engine** that drives the run: a typed
5-phase state machine in `internal/recipe/` that orchestrates a main
agent + sub-agents against the live Zerops platform. The engine is
deterministic. It knows platform invariants up-front and refuses to
proceed when its own contracts are violated. It does **not** know
framework specifics, library quirks, or per-recipe scenarios — those
are discovered by sub-agents during deploy iteration and recorded as
facts.

The reader of every published surface is a **porter**. Not a recipe
author. Not someone learning Zerops generally. Not the agent that ran
the recipe. Voice rules across every surface flow from this single
audience.

---

## 2. The output shape

Fixed. Engine job is to produce exactly this shape, byte-correct. The
**reference** is `zeropsio/recipes` (`/Users/fxck/www/recipes/`),
authoritatively `_template/` for the recipe-repo shape and any
published recipe (e.g. `laravel-minimal/`) for an instantiated example.
The apps-repo reference is `/Users/fxck/www/laravel-showcase-app/`.

### Recipe repo — the click-deploy template

Pushed to `zeropsio/recipes` under `<recipe-slug>/`. Tier folders sit
**directly at recipe root** — no `environments/` parent. Folder names
are the literal pretty strings (em-dash, with spaces) that the engine
defines in `internal/recipe/tiers.go::Tiers`.

```
<recipe-slug>/
├── README.md                              — root README; porter scans to
│                                            decide if this recipe deploys
│                                            what they need; carries deploy
│                                            buttons + per-tier links
├── 0 — AI Agent/
│   ├── import.yaml                        — click-deploy bytes (dev-pair:
│   │                                        apidev + apistage slots)
│   └── README.md                          — per-tier audience, outgrow
│                                            signal, what changes at next tier
├── 1 — Remote (CDE)/
│   ├── import.yaml                        — dev-pair
│   └── README.md
├── 2 — Local/
│   ├── import.yaml                        — single-slot (api + app)
│   └── README.md
├── 3 — Stage/
│   ├── import.yaml                        — single-slot
│   └── README.md
├── 4 — Small Production/
│   ├── import.yaml                        — single-slot
│   └── README.md
└── 5 — Highly-available Production/
    ├── import.yaml                        — single-slot
    └── README.md
```

### Apps repo — per-codebase, the cloned working application

One repo per codebase, pushed to
`zerops-recipe-apps/<recipe-slug>-app` (or `<recipe-slug>-<role>` for
multi-codebase recipes). The repo root **is** the application root —
README, CLAUDE.md, and `zerops.yaml` sit alongside the framework's own
files (Laravel's `artisan`, `composer.json`, `public/`, `routes/` —
NestJS's `nest-cli.json`, `src/`, `tsconfig.json` — etc.). There is
**no `source/` subdirectory**; the porter clones the repo and runs
`composer install` / `npm install` at root.

```
<recipe-slug>-app/                         (= <Codebase.SourceRoot> at runtime,
│                                             which is /var/www/<hostname>dev/
│                                             on the Zerops dev mount)
│
├── README.md                              — porter-facing integration guide
│                                            + knowledge base
├── CLAUDE.md                              — porter-facing dev guide (≤60
│                                            lines; no cross-codebase content)
├── zerops.yaml                            — runtime config the porter
│                                            actually deploys with
│
└── <framework files at root>              — the application itself, e.g. for
                                             Laravel: app/, artisan,
                                             bootstrap/, composer.json,
                                             config/, database/, package.json,
                                             public/, resources/, routes/,
                                             storage/, tests/, vite.config.js
```

### Runtime path note

During an authoring run, the engine writes the recipe-repo content to
`<outputRoot>/<tier.Folder>/...` (matching the published shape) and the
apps-repo content stays in place at each codebase's `SourceRoot` (the
SSHFS-mounted dev slot at `/var/www/<hostname>dev/`). `SourceRoot`
carries a `dev` suffix as an engine-enforced contract (M-1). Publish
takes both paths and pushes them to their respective GitHub repos.

### Templating contract (the deliverable is a template, not bytes)

The 6 import.yaml files are a **template**. Each end-user's click-deploy
creates their own project. That means:

- **Shared secrets emit as `<@generateRandomString(<32>)>`** — evaluated
  once per end-user at their import. The author's real workspace secret
  never reaches the deliverable.
- **URLs use `${zeropsSubdomainHost}` as a literal** — the platform
  substitutes the end-user's subdomain at click-deploy.
- **Per-env shape differs**:
  - Envs 0-1 (AI-Agent, Remote/CDE — dev-pair `apidev`/`apistage` slots
    exist): carry both `DEV_*` and `STAGE_*` URL constants.
  - Envs 2-5 (Local, Stage, Small-Prod, HA-Prod — single-slot `api`/
    `app`): carry `STAGE_*` only with single-slot hostnames.

### Workspace YAML vs deliverable YAML — two distinct shapes

Run-time provision uses a **workspace** yaml: services-only, no `project:`
block, no `buildFromGit`, no `zeropsSetup`, no preprocessor expressions,
dev runtimes carry `startWithoutCode: true`. Submitted inline to
`zerops_import content=<yaml>`. Never written to disk.

Run-time finalize emits **deliverable** yamls: full `project:` +
`envVariables` + `buildFromGit` + `zeropsSetup`. Written to
`<outputRoot>/environments/<N>/import.yaml`.

The two shapes are emitted by construction in `yaml_emitter.go`. There
is no post-hoc validator policing the difference; the workspace path
literally cannot emit deliverable-only fields and vice versa.

---

## 3. The runtime sequence

The 5 phases of `internal/recipe/workflow.go::Phase`. Each has an
adjacent-forward entry guard and a gate-set exit guard. Phases do not
skip; phases do not retreat.

### Phase 1 — Research (main agent only)

- **Action**: agent reads parent recipe (chain resolution, if any),
  composes a `Plan` (slug, codebases with roles, services, tier shape).
- **Engine role**: validates plan structure. No platform contact yet.
- **Output**: `Plan` populated and recorded.
- **Discovery surface**: framework + codebase shape decision (see
  plan.md §1 showcase table).

### Phase 2 — Provision (main agent + live platform)

- **Action**: emit workspace yaml → `zerops_import content=<yaml>`. Set
  workspace secrets via `zerops_env project=true action=set` with real
  values. Discover cross-service env keys via `zerops_discover
  includeEnvs=true`.
- **Engine role**: emits the workspace yaml shape; classifies provision
  facts; gates exit on import success.
- **Output**: live Zerops project with services in dev-pair shape;
  `Plan.ProjectEnvVars` populated.

### Phase 3 — Scaffold (parallel sub-agent dispatch)

- **Action**: main agent dispatches one **scaffold sub-agent per
  codebase** in a single message (parallel `Agent` tool calls — sub-
  agents queue at the recipe session mutex naturally; serializing
  dispatch loses 15-30 minutes of parallelizable wall time).
- Each sub-agent receives a brief composed by `BuildScaffoldBrief`
  carrying: codebase identity, role (monolith / api / frontend /
  worker), platform obligations (HTTP gated on `role.ServesHTTP`),
  citation map, writing contract.
- **Sub-agent**: writes minimal source + `zerops.yaml` to `<SourceRoot>/`,
  deploys via `zerops_deploy`, iterates against real platform errors,
  consults `zerops_knowledge` for managed-service connection idioms
  before writing client code.
- **Sub-agent authors codebase-scoped fragments in-phase** at densest
  context: integration-guide items #2+, knowledge-base bullets,
  CLAUDE.md notes, cited gotchas. Recorded via `zerops_recipe action=
  record-fragment`.
- **Sub-agent records facts** via `zerops_recipe action=record-fact`
  (camelCase schema). Engine classifies + routes per the taxonomy.
- **Engine role**: composes briefs; receives fragments + facts;
  classifies; does not author content.
- **Output**: every codebase has working dev-deploy + scaffold-authored
  fragments + recorded facts. At scaffold close, each `<SourceRoot>/`
  has `git init` + initial commit.

### Phase 4 — Feature (sub-agent dispatch, may serialize)

- **Action**: feature sub-agent extends each codebase with the scenario
  logic (the "thing this recipe demonstrates").
- **Sub-agent**: authors per-codebase content extensions (additional KB
  bullets, additional CLAUDE notes, per-feature commits). Browser-walks
  the running app via `zerops_browser`; records `browser_verification`
  facts (one per browser call).
- **Engine role**: composes feature brief; receives fragments + facts.
- **Output**: every codebase dev + stage green; scenario verified end-
  to-end; per-feature commits in apps repo.

### Phase 5 — Finalize (main agent, may dispatch)

- **Action**: author root + env fragments (intros, per-tier import-
  comments). Stitch every surface into `outputRoot`. Run validator
  gates. Iterate on failures via `record-fragment` → re-stitch.
- **Engine role**: stitches surfaces; runs gates; gates publication.
- **Output**: complete deliverable on disk; `zcp sync recipe export` →
  `publish` ready.

The **discovery loop** lives in phases 3 + 4. Phase 5 is assembly +
quality, not new discovery. Phase 5 is allowed to fail loudly; it is
not allowed to invent knowledge that earlier phases didn't capture.

---

## 4. The TEACH / DISCOVER line

> **Decision marker.** As of 2026-04-25 the cleanup pass has shipped:
> wrong-side artifacts have been demoted to Notice severity, deleted
> outright, or merged. The verdict table below reflects the post-
> cleanup state. The architectural reframe lives in [CHANGELOG.md
> entry "2026-04-25 — architectural reframe: catalog drift recognized,
> gates → notices/structural"](CHANGELOG.md); the cleanup itself is
> recorded in the cleanup CHANGELOG entry directly above it. The
> operational pause record is at [plans/run-11-pause.md](plans/run-11-pause.md).

This is the load-bearing section. It draws the line between what the
engine knows up-front (TEACH) and what each run is responsible for
discovering (DISCOVER). The line exists because v2 (and earlier
zcprecipator iterations before it) failed at exactly this boundary —
every dogfood run produced "the agent shipped X" → engine encoded an
X-detector → next run produced Y → engine grew a Y-detector. Catalogs
displaced discovery. The product became the catalog.

### TEACH side — engine knows up-front

These are platform invariants. They don't change run-to-run; they're
the same regardless of recipe. Delivered via three channels (see §5).

**Always-on platform mechanics** (every recipe, every codebase):
- Env-var model (three timelines: workspace secrets → scaffold cross-
  service refs → deliverable templates with `<@generateRandomString>`
  and `${zeropsSubdomainHost}` literals)
- Init-commands model (three static-key shapes; `execOnce` semantics)
- Mount-vs-container execution split (editor on SSHFS mount, framework
  CLIs over `ssh`)
- Dev-loop (`zsc noop --silent`, `zerops_dev_server`, dev-vs-prod
  process model)
- Yaml-comment style (block-mode causal comments, no decorative
  dividers)

**Role-conditional mechanics** (per-codebase by role):
- HTTP support (gated on `role.ServesHTTP`) — port + bind-address, L7
  balancer, subdomain
- Worker / static / database — different obligations, different brief
  sections

**Output-shape contracts** (engine generates by construction; no post-
hoc validator):
- IG item #1 is engine-generated from the codebase's `zerops.yaml`
  (`### 1. Adding zerops.yaml` + yaml verbatim with comments)
- Workspace yaml shape vs deliverable yaml shape
- Per-tier env shape (dev-pair vs single-slot)
- Apps-repo path layout — `<SourceRoot>/{README, CLAUDE, zerops.yaml,
  source}` with `<SourceRoot>` ending in `dev`. Engine refuses other
  shapes at stitch (M-1).

**Citation map** (knowledge that exists; sub-agents are pointed at it):
- Topic IDs: `env-var-model`, `init-commands`, `http-support`,
  `deploy-files`, `rolling-deploys`, `object-storage`,
  `readiness-health-checks`. The map names topics; actual guides live
  in the embedded knowledge corpus and are fetched on demand via
  `zerops_knowledge`.

### DISCOVER side — each run finds out

These are recipe-specific. The engine *cannot* know them ahead of time
without becoming a catalog-of-everything. Surfaced by sub-agents during
scaffold + feature against the live platform; recorded as `FactRecord`s;
routed to the right surface by classification.

- **Managed-service connection idioms** — NATS structured fields vs
  URL, S3 endpoint scheme, Meilisearch master-key shape, Postgres
  connection pooling. Discovered by sub-agent calling
  `zerops_knowledge runtime=<type>` before writing client code.
- **Framework-specific binding behavior** — bind address, middleware
  ordering, trust-proxy semantics, library version gotchas. Discovered
  by sub-agent during deploy iteration.
- **Per-recipe field usage** — which managed-service env keys *this*
  recipe consumes. Discovered by `zerops_discover includeEnvs=true`.
- **Cross-service contracts** — this recipe's broker connection string,
  this recipe's seed key, this recipe's queue topic. Discovered as the
  agent writes them.
- **Per-codebase causal rationale** — "we use port 3000 because…", "the
  worker uses `createMicroservice` because…". Discovered as the agent
  makes choices.

### The test that draws the line

A piece of knowledge belongs on the **TEACH** side iff:
- It is the same for every recipe regardless of framework, language,
  or scenario, **AND**
- It can be expressed as a positive rule (a *shape* the engine
  produces or requires), **not** as a negative pattern (a *string* the
  engine bans).

A piece of knowledge belongs on the **DISCOVER** side iff:
- It varies recipe-to-recipe based on framework / library / scenario
  choices, **OR**
- It can only be expressed as "we know X is wrong because we saw the
  agent ship X in run K."

**The catalog-drift signature** is exactly this: a piece of DISCOVER-
side knowledge gets reified as an engine-side ban-list because a
specific run produced a specific bad output. The fix preserves the
lesson per-run by expressing it as a TEACH-side positive shape (when
possible) or by leaving it on DISCOVER and letting the agent learn
through deploy iteration (when not).

### The test applied — current artifacts

| Artifact | Side | Status |
|---|---|---|
| `dev`-suffix on `Codebase.SourceRoot` (M-1) | TEACH | ✅ Engine refuses other shapes by construction |
| `### N.` heading shape for IG items (R-1) | TEACH | ✅ Engine generates item #1 in this shape |
| Workspace vs deliverable yaml shapes | TEACH | ✅ Emitted by construction; no post-hoc validator |
| Engine-emitted IG item #1 (run-10 M) | TEACH | ✅ Engine generates the shape from yaml body |
| Citation map atom | TEACH | ✅ Names topics; doesn't ban anything |
| `causalWords` allow-list (run-8 D) | DISCOVER | ✅ Notice — the agent sees the lesson; finalize doesn't block |
| `tierPromotionVerbs` (run-8 D) | DISCOVER | ✅ Notice |
| `metaVoiceWords` (run-8 D) | DISCOVER | ✅ Notice |
| `yamlDividerREs` (run-9 H) | — | ✅ Deleted (pure style, no semantic load) |
| `sourceForbiddenPhrases` (run-9 I) | DISCOVER | ✅ Notice |
| `kbTripleFormatRE` (run-10 O) | DISCOVER | ✅ Notice |
| `claudeMDForbiddenSubsections` (run-10 P) | DISCOVER | ✅ Notice |
| `templatedOpeningCheck` first-sentence similarity (run-8 D) | DISCOVER | ✅ Notice |
| `boldBulletRE` KB symptom contract (run-8 D) | DISCOVER | ✅ Notice |
| V-5 three run-10 anti-patterns in scaffold brief (run-11) | — | ✅ Deleted (rewritten as abstract litmus rule) |
| `PlatformVocabulary` (merged from `platformMechanismVocab` + `platformMentionVocabBase`) | TEACH (defensible) | ✅ Single shared list; V-1 keeps record-time auto-classify, V-3 is now Notice — both consume the same vocab |
| `kbCitedGuideBoilerplateRE` (run-11 O-2) | DISCOVER | ✅ Notice |
| `kbSelfInflictedVoiceRE` (run-11 V-4) | DISCOVER | ✅ Notice |
| `guideKnowledgeSources` map (run-11 V-2) | TEACH (defensible) | ⚠️ Hand-curated topic→source map; V-2 validator now flows as Notice |
| `LintDeployignore` artifact / redundant patterns (run-11 P-3) | DISCOVER | ✅ Warnings only — deploy never blocks; TEACH-side teaching lives in `themes/core.md` |
| `.deployignore` paragraph rewrite in `themes/core.md` (run-11 P-1) | TEACH | ✅ Positive teaching in atom |

### What "wrong side" means concretely

Every wrong-side artifact has the same lifecycle: dogfood run produces
output of class X → analysis names X → readiness plan encodes X-
detection as a finalize gate → next run's agent learns to avoid the
trigger string but not the underlying class → next run produces Y →
catalog grows. None of this teaches the agent the underlying skill;
it pressures the agent to evade the regex.

The corrective action depends on whether the underlying lesson is
recoverable as TEACH-shape or only sits as DISCOVER:

- **Recoverable as TEACH**: rewrite as engine-emitted shape. Example:
  IG item #1 was originally enforced by validator ("first item must
  embed yaml"); now engine *emits* the item from the yaml itself, so
  the validator is structurally unnecessary.
- **Belongs on DISCOVER**: demote to a record-time `Notice` (signal,
  not gate). The agent sees the lesson when it would have tripped the
  gate, but publication isn't blocked. V-1 already does this for self-
  inflicted classification; V-3 / V-4 / O-2 / P-3 should follow.
- **Pure style with no semantic content**: delete. Banning ASCII art
  in yaml comments doesn't move the recipe quality needle and isn't
  what the engine should spend complexity on.

---

## 5. How knowledge flows to the agent

Three legitimate channels:

1. **Always-on principle atoms** — injected into every brief by the
   composer. Defined under `internal/recipe/content/principles/`.
   Examples: `env-var-model.md`, `init-commands-model.md`,
   `mount-vs-container.md`, `dev-loop.md`, `yaml-comment-style.md`.

2. **Brief-conditional content** — included by the brief composer based
   on `Plan` structure. `BuildScaffoldBrief` includes the `## HTTP`
   section only when `role.ServesHTTP=true`. `BuildFinalizeBrief`
   derives codebase paths + fragment-count math from `Plan`. Phase-
   entry atoms (`content/phase_entry/*.md`) carry the phase's
   procedural recipe.

3. **Discovery channel** — `zerops_knowledge query=<topic>` and
   `zerops_discover includeEnvs=true`. Sub-agents call these on demand
   during scaffold + feature when they hit a managed-service connection
   or need to know what env keys the platform will inject. The engine
   *pre-points* sub-agents at this channel; it does not pre-fill
   answers from it.

A **fourth, illegitimate channel** has been accreting: knowledge the
engine teaches *post-hoc* by detecting its absence in stitched output
(the validator catalogs from §4). The §4 line says this channel
shouldn't exist. Anything that would go there belongs in (1)/(2) as a
positive rule, (3) as something the agent discovers per run, or
nowhere (deleted as out-of-scope).

---

## 6. Reading order for a fresh instance

1. This doc, top to bottom.
2. [`docs/spec-content-surfaces.md`](../spec-content-surfaces.md) —
   per-surface content contracts + classification taxonomy. The
   classification rules in §4 of THIS doc reference its taxonomy.
3. [`docs/zcprecipator3/plan.md`](plan.md) §1 (product), §2 (input
   formula), §7 (chain) — design history; skim for context, don't
   internalize delivery-phase content (§9 onward).
4. `internal/recipe/workflow.go`, `tiers.go`, `briefs.go`,
   `yaml_emitter.go`, `assemble.go`, `handlers.go` — code reflects
   current state.
5. `CHANGELOG.md` — read **only** the most recent entry to see
   what's freshest. Older entries are run-history; do not treat them
   as current spec.

---

## 7. What this doc deliberately does NOT cover

- Per-run gap lists (those live in `plans/run-N-readiness.md`)
- Engineering delivery phases (those live in `plan.md` §9)
- v2 archaeology (lives in `../zcprecipator2/`)
- Atom-corpus authoring contract (lives in
  `../spec-knowledge-distribution.md`)
- Detailed validator inventory (lives in code under
  `internal/recipe/validators*.go`)

When this doc is wrong, fix this doc. When this doc and a per-run
plan disagree, this doc wins — the run plan was correct at its time
and is now history. When this doc and the code disagree, fix whichever
is wrong; both are authoritative for their domain (this doc for
intent, code for behavior).
