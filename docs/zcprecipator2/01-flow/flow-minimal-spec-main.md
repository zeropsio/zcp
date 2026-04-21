# flow-minimal-spec-main.md

**Source**: reconstructed — **no session log available**
**Reference**: `<ref>=spec`  (reconstruction from `internal/content/workflows/recipe.md` + `internal/workflow/recipe_*.go` + published minimal deliverables)
**Scope**: main-agent expected flow for a Type 3 (backend framework) minimal-tier recipe run — `{framework}-minimal` (e.g. `nestjs-minimal`, `laravel-minimal`, `django-minimal`).

---

## Derivation sources

| Question | Authoritative source |
|---|---|
| Workflow step sequence (research → provision → generate → deploy → finalize → close) | [internal/workflow/recipe.go:15-17](../../internal/workflow/recipe.go#L15-L17) tier constants + `RecipeStep*` constants |
| Generate substeps | [internal/workflow/recipe_substeps.go:77-90](../../internal/workflow/recipe_substeps.go#L77-L90) `generateSubSteps()` (tier-invariant) |
| Deploy substeps (tier-branched) | [internal/workflow/recipe_substeps.go:92-124](../../internal/workflow/recipe_substeps.go#L92-L124) `deploySubSteps(plan)` — minimal drops `subagent`/`snapshot-dev`/`browser-walk` |
| Close substeps | [internal/workflow/recipe_substeps.go:139-150](../../internal/workflow/recipe_substeps.go#L139-L150) `closeSubSteps()` returns `nil` for minimal (no substep tracking) |
| Research guide | [internal/workflow/recipe_guidance.go:105-110](../../internal/workflow/recipe_guidance.go#L105-L110) — minimal gets `research-minimal` only; showcase gets `research-showcase` + `research-minimal` |
| Topic-per-substep mapping | [internal/workflow/recipe_guidance.go:540-595](../../internal/workflow/recipe_guidance.go#L540-L595) `subStepToTopic()` — branches on `isShowcase(plan)` at `app-code` and `readmes` |
| Dispatched sub-agent briefs in recipe.md (minimal-relevant) | `readme-with-fragments` (L2205) + `code-review-subagent` (L3050); `content-authoring-brief` (L2390) and `scaffold-subagent-brief` (L790) are showcase-only |
| Deliverable shape confirmation | [`/Users/fxck/www/zcprecipator/nestjs-minimal/nestjs-minimal-v3/TIMELINE.md`](/Users/fxck/www/zcprecipator/nestjs-minimal/nestjs-minimal-v3/TIMELINE.md) — one `appdev` codebase, no worker, no feature subagent, code-review subagent fires at close |

**Gaps relative to live-session evidence** (the reconstruction path carries these blindspots — flagged explicitly per decision #1):

| Gap | Consequence for step 2/3/4 |
|---|---|
| Main-agent inline feature-writing tool mix / time / errors | Step 2 cannot populate minimal-tier redundancy map for feature-phase knowledge deliveries with `result_size` bytes — use showcase deploy.subagent substep bytes as upper bound, document uncertainty |
| Actually-interpolated minimal writer dispatch prompt | Step 4 brief diff for the minimal writer uses the recipe.md block text rather than an observed dispatch; note "schematic — plan field values not substituted" in step 4's diff column |
| Live per-substep guidance_landed sizes | Step 2's knowledge-matrix rows for minimal carry the showcase v34 observed sizes as proxy + the fact that most topics resolve to the SAME recipe.md block (confirmed by `subStepToTopic` — minimal/showcase share the topic for `deploy-dev`, `start-processes`, `verify-dev`, `init-commands`, `feature-sweep-dev`, `cross-deploy`, `verify-stage`, `feature-sweep-stage`) |
| Minimal-tier TodoWrite / fact-record cadence | Cannot confirm or refute v34's 12-TodoWrite-rewrite class at minimal scale; step 2/3 carries the concern over as "both tiers likely" pending evidence |

---

## Step-by-step main-agent flow (expected shape)

Columns follow the showcase trace format for comparability. `result_size` estimates come from showcase v34 observed sizes where the topic is tier-invariant (confirmed from `subStepToTopic`). For showcase-only substeps, the row is omitted with a note.

### START → RESEARCH

| timestamp | phase/substep | tool | input_summary | result_size (est.) | result_summary (expected) | guidance_landed (expected) | next_tool (expected) |
|---|---|---|---|---|---|---|---|
| (n/a) | — | mcp:zerops_workflow | `action=status` | ~300B | initial status | — | `action=start workflow=recipe` |
| (n/a) | START | mcp:zerops_workflow | `action=start workflow=recipe intent="..."` | ~14–17KB | session started; delivers `research-minimal` guide | `~14KB phase=research(idx=0)` — **MINIMAL-only path** per [recipe_guidance.go:110](../../internal/workflow/recipe_guidance.go#L110) | `zerops_knowledge recipe={framework}-hello-world` (optional predecessor lookup) OR directly `complete step=research` |
| (n/a) | RESEARCH | mcp:zerops_knowledge | `recipe={framework}-hello-world` | ~5–8KB | predecessor platform constraints; agent SHOULD NOT load hello-world manually per recipe.md:78 but v3 TIMELINE confirms this is a common first call | — | — |
| (n/a) | RESEARCH | mcp:zerops_workflow | `action=complete step=research recipePlan={…}` | ~22KB | provision guide delivered | `~22KB phase=provision(idx=1)` | `zerops_import` |

**Key minimal-tier research decisions** (recipe.md:5, research-minimal section):
- `tier: "minimal"` (required)
- `slug: "{framework}-minimal"`
- `research.dbDriver: "postgresql"` (DATABASE name, NOT the ORM — recipe.md:40 rejection class for `typeorm`, etc.)
- One or two `targets`: `app` (framework runtime) + `db` (typically `postgresql@18`). **No worker** (recipe.md:20 "Minimal recipes have no worker").
- `features`: 1–3 entries covering dashboard + DB connectivity (recipe.md:48); no service-coverage mandate (that's showcase-only per [recipe_features.go:155](../../internal/workflow/recipe_features.go#L155))

### PROVISION

| timestamp | phase/substep | tool | input_summary | result_size | result_summary | guidance_landed | next_tool |
|---|---|---|---|---|---|---|---|
| (n/a) | RESEARCH | mcp:zerops_import | (implicit args — derives from plan) | ~2KB | 3 services created: `appdev`, `appstage`, `db` | — | `zerops_discover` |
| (n/a) | RESEARCH | mcp:zerops_discover | — | ~3–5KB | envs + services introspected | — | `zerops_env action=set` (optional) / `zerops_mount` |
| (n/a) | RESEARCH | mcp:zerops_mount | `action=mount serviceHostname=appdev` | ~130B | SSHFS mounted at `/var/www/appdev/` | — | — |
| (n/a) | RESEARCH | mcp:zerops_workflow | `action=complete step=provision attestation={…}` | ~40KB | **generate guide** delivered — same block text as showcase since scaffold/app-code/smoke-test/zerops-yaml topics are tier-invariant at the guide level, with two tier branches surfacing at SubStepAppCode (execution-order vs dashboard-skeleton) and one at SubStepReadmes (readme-fragments vs content-authoring-brief) | `~42KB phase=generate(idx=2)` | — |

**Single codebase** (minimal): one `zerops_mount` call; one SSHFS mount (`/var/www/appdev/`). Contrast showcase v34 which issued three mount calls for `appdev`/`apidev`/`workerdev`.

### GENERATE (4 substeps: scaffold → app-code → smoke-test → zerops-yaml)

| phase/substep | topic delivered | Dispatch? | Notes |
|---|---|---|---|
| generate.scaffold | `where-to-write` ([recipe_guidance.go:544](../../internal/workflow/recipe_guidance.go#L544)) | **No dispatch** per recipe.md:480 ("for showcase multi-codebase plans dispatch scaffold sub-agents in parallel; for everything else write yourself") — main runs `ssh appdev "cd /var/www && npx @nestjs/cli new --skip-git --skip-install"` or framework equivalent | v3 TIMELINE L20-22 confirms; one of the first observable deltas vs showcase (showcase dispatches 3 parallel `Agent` tool calls with the `scaffold-subagent-brief` at deploy-provision boundary — see showcase main trace events #21-23) |
| generate.app-code | **`execution-order`** ([recipe_guidance.go:550](../../internal/workflow/recipe_guidance.go#L550), contrast showcase → `dashboard-skeleton`) | No dispatch — main writes app files inline | Minimal writes a simple dashboard + DB-connectivity handler; no "dashboard skeleton with feature slots for a later subagent" (that's showcase-only, recipe.md:458) |
| generate.smoke-test | `smoke-test` | No dispatch — main runs `ssh appdev "<install>"` / `<build>` / `<start>` | Tier-invariant |
| generate.zerops-yaml | `zerops-yaml-rules` | No dispatch — main writes `zerops.yaml` | Tier-invariant; recipe.md:486 explicitly forbids sub-agent authoring of zerops.yaml regardless of tier |
| (generate completion) | `what-to-generate-minimal` pointer (vs `what-to-generate-showcase` at recipe.md:446) | — | Deploy guide delivered on `complete step=generate` call |

**No Agent dispatches during generate for minimal** — confirmed by code + deliverable.

### DEPLOY (9 substeps for minimal; showcase has 12)

| phase/substep | topic delivered | Dispatch? | Notes |
|---|---|---|---|
| deploy.deploy-dev | `deploy-flow` | No | Tier-invariant. Main runs `zerops_deploy serviceHostname=appdev` |
| deploy.start-processes | `deploy-flow` | No | Tier-invariant. Main SSHs the app container + starts the dev server |
| deploy.verify-dev | `deploy-target-verification` | No | Tier-invariant. `zerops_verify` + curl-level checks |
| deploy.init-commands | `deploy-flow` | No | Tier-invariant. Runs migration/seed |
| **deploy.subagent** | `subagent-brief` | **SHOWCASE ONLY** — [recipe_substeps.go:108](../../internal/workflow/recipe_substeps.go#L108) | **Minimal main agent WRITES FEATURES INLINE.** No dispatch to a feature sub-agent. Every feature wiring happens in the main agent's context — this is the only structural behaviour the minimal tier has that showcase does not. **This is the reconstruction gap documented above.** |
| **deploy.snapshot-dev** | `deploy-flow` | **SHOWCASE ONLY** | No snapshot: main's inline writes are already on disk; no subagent-authored code to re-deploy as an artifact snapshot |
| deploy.feature-sweep-dev | `feature-sweep-dev` | No | Tier-invariant. Every api-surface feature must respond 200 + application/json |
| **deploy.browser-walk** | `browser-walk` | **SHOWCASE ONLY** | No feature dashboard to walk for minimal (recipe.md:458 + recipe_substeps.go:139-142 comment — minimal/hello-world have no dashboard) |
| deploy.cross-deploy | `stage-deploy` | No | Tier-invariant. Promotes appdev → appstage |
| deploy.verify-stage | `deploy-target-verification` | No | Tier-invariant |
| deploy.feature-sweep-stage | `feature-sweep-stage` | No | Tier-invariant |
| **deploy.readmes** | **`readme-fragments`** ([recipe_guidance.go:591](../../internal/workflow/recipe_guidance.go#L591), contrast showcase → `content-authoring-brief`) | **Dispatch decision is main-agent discretion** per recipe.md:2211 ("When the main agent delegates README writing to a sub-agent…") | The recipe.md block `readme-with-fragments` (L2205-2388) is written as both a sub-agent prompt template AND a main-inline authoring reference. nestjs-minimal v3 TIMELINE has no separate writer subagent noted; main wrote README inline. v1/v2 of nestjs-minimal likely same shape. **Whether this is always main-inline or sometimes a subagent dispatch is a reconstruction gap.** |

### FINALIZE

No substeps for minimal (per `initSubSteps` returning nil for `RecipeStepFinalize`). Single `complete step=finalize` call with `generate-finalize` payload (envComments + projectEnvVariables). Finalize checks fire on the produced env tree — same check surface as showcase (env comment ratio/depth/cross-refs/factual-claims, min_containers, shared_secret, preprocessor, cross_env_refs).

### CLOSE (no substeps for minimal)

| Activity | Dispatch? | Notes |
|---|---|---|
| Static code-review | **Recommended dispatch** — `code-review-subagent` block at recipe.md:3050 | v3 TIMELINE L83 confirms "Verification sub-agent (NestJS expert) reviewed all files". No close-step ordering gate (recipe_substeps.go:139-150 returns nil for minimal — historical rubric, v18/v19 identified this as a gap for showcase and fixed via substep gates; minimal remains un-gated) |
| close.close-browser-walk | **SHOWCASE ONLY** — [recipe_substeps.go:143](../../internal/workflow/recipe_substeps.go#L143) | No dashboard to walk for minimal; single-endpoint validation already happened at feature-sweep-stage |
| `complete step=close` | — | Direct call; no substep enforcement |

### POST-CLOSE

Per v3 TIMELINE "Section 7. Post-close fix": main agent may do ad-hoc touch-ups after close (e.g. v3 discovered missing `.gitignore` because `--skip-git` suppressed it, added it post-close). This is unstructured and off-the-workflow-path.

---

## Agent dispatches expected — summary

Minimal tier makes **at most 2** `Agent` tool dispatches over the entire run:

| # | Substep | Brief template block in recipe.md | Confidence |
|---|---|---|---|
| 1 | deploy.readmes | `readme-with-fragments` (L2205-2388) — **main-agent discretion** whether to dispatch or write inline | Medium — nestjs-minimal v3 wrote inline; no session-log evidence for a minimal writer dispatch |
| 2 | close (no substep) | `code-review-subagent` (L3050-3159) | High — v3 TIMELINE confirms a "Verification sub-agent (NestJS expert)" fired |

Contrast showcase v34: **6** `Agent` dispatches (3 scaffold subagents + 1 feature + 1 writer + 1 code-review) — captured verbatim under [`flow-showcase-v34-dispatches/`](flow-showcase-v34-dispatches/).

---

## Flagged-event classes expected for minimal

| Class | Probability | Rationale |
|---|---|---|
| `is_error=true` | Moderate | Same substrate as showcase — ts-node `.js`-extension rejection (v3), `import type` trap (v3), missing `.gitignore` from `--skip-git` (v3) all shipped in minimal — these are scaffold-traps the author-side needs. Live tool-error rate unknown |
| Guidance mis-order (the v25 substep-bypass class) | Low | v8.90's de-eager fix + SUBAGENT_MISUSE error are tier-invariant; minimal's reduced subagent count means fewer dispatch-ordering failure modes |
| `scope=downstream` fact records | **Not applicable** | Theme B (v8.96 Scope=downstream) is cross-subagent knowledge routing; minimal has ≤2 dispatches, so downstream-scope routing is structurally minimal |
| TodoWrite full-rewrites | Likely same class as showcase | v34 showcase fired 12 full-rewrites; minimal has fewer substeps so the upper bound is lower but the per-step-entry rewrite pattern is the same root cause (recipe.md's step-entry guidance reads like fresh planning context regardless of tier) |
| Agent dispatches | ≤2 | See summary above |

---

## What to treat as load-bearing vs parked for step 2

**Load-bearing** (ready to feed step 2's knowledge inventory):
- All topics named by `subStepToTopic` — both tier-invariant and tier-branched
- Dispatch count differential (6 → ≤2)
- Substep count differential (close-step 2→0, deploy-step 12→9)
- Tier-invariant substeps' guide sizes (take from showcase v34 — same recipe.md block delivers)

**Parked as reconstruction gap** (explicitly named so step 2's artifacts can mark the corresponding matrix rows "proxy — showcase-derived"):
- Minimal main-agent inline feature-writing time/bytes/error-rate
- Whether a minimal writer is ever dispatched as a sub-agent (evidence to date: always main-inline)
- Exact `is_error` and `TodoWrite` cadence at minimal scale
