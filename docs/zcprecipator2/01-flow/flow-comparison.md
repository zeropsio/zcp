# flow-comparison.md — minimal vs showcase flow diff

**Purpose**: enumerate every structural difference between the minimal-tier and showcase-tier flows, per RUNBOOK §6 success criterion 4 ("no hand-waving").

**Evidence base**:
- Showcase: v34 session log — [`flow-showcase-v34-main.md`](flow-showcase-v34-main.md) + 6 sub-agent traces + 6 dispatch captures
- Minimal: reconstruction per RESUME decision #1 — [`flow-minimal-spec-main.md`](flow-minimal-spec-main.md) + 2 dispatch templates. No live session log; reconstruction sources `internal/content/workflows/recipe.md` + `internal/workflow/recipe_*.go` + published deliverable `nestjs-minimal-v3`

---

## 1. Headline structural diffs

| Axis | Minimal | Showcase (v34) | Source |
|---|---|---|---|
| Codebases / SSHFS mounts | **1** (`appdev` only) | **3** (`appdev` + `apidev` + `workerdev`) | `research.targets` + worker decision in recipe.md:119-131 |
| Managed services provisioned | 1 (`db`) + 1 app + 1 stage = 3 total | 5 (`db` + `redis` + `queue` + `storage` + `search`) + 3 app-services + 3 stage-services + 1 worker + 1 workerstage = 14 total | recipe-taxonomy.md Type 3 vs Type 4; corrected to include NATS, drop mail |
| `zerops_mount` calls expected | 1 | 3 (showcase v34 main trace events #13-15) | `zerops_mount` per `sharesCodebaseWith`-derived mount set |
| Worker target | **none** (recipe.md:20) | 1 (separate-codebase default per recipe.md:121) | [recipe_features.go:260](../../internal/workflow/recipe_features.go#L260) |
| Feature-coverage mandate | off (validator stops at rule 6 per [recipe_features.go:141-144](../../internal/workflow/recipe_features.go#L141-L144)) | on (every managed-service kind must have ≥1 feature) | [recipe_features.go:155](../../internal/workflow/recipe_features.go#L155) |

## 2. Substep-count deltas

### generate step (4 substeps, tier-invariant)

Both tiers: `scaffold` → `app-code` → `smoke-test` → `zerops-yaml`. No count delta; topic branches only (see §3).

### deploy step (9 substeps minimal / 12 showcase — 3-substep gap)

| Substep | Minimal | Showcase | Source |
|---|---|---|---|
| deploy-dev | ✅ | ✅ | [recipe_substeps.go:94](../../internal/workflow/recipe_substeps.go#L94) |
| start-processes | ✅ | ✅ | L95 |
| verify-dev | ✅ | ✅ | L96 |
| init-commands | ✅ | ✅ | L97 |
| **subagent** | ❌ — **main writes features inline** | ✅ — feature sub-agent dispatch | L108 `if isShowcase` |
| **snapshot-dev** | ❌ | ✅ — re-deploy to bake subagent output into dev artifact | L108 |
| feature-sweep-dev | ✅ (fires right after init-commands) | ✅ (fires after snapshot-dev) | L108 vs L114 |
| **browser-walk** | ❌ — no dashboard to walk | ✅ | L108 |
| cross-deploy | ✅ | ✅ | L116 |
| verify-stage | ✅ | ✅ | L116 |
| feature-sweep-stage | ✅ | ✅ | L116 |
| readmes | ✅ (old `readme-fragments` topic) | ✅ (v8.94 `content-authoring-brief` topic) | topic branch at [recipe_guidance.go:588](../../internal/workflow/recipe_guidance.go#L588) |

### close step (2 substeps showcase, **none** for minimal)

| Substep | Minimal | Showcase | Source |
|---|---|---|---|
| code-review | no gate — discretionary dispatch per close-step prose | ✅ gated substep | [recipe_substeps.go:139-142](../../internal/workflow/recipe_substeps.go#L139-L142) — `closeSubSteps` returns nil for non-showcase |
| close-browser-walk | ❌ — no dashboard | ✅ | same |

**Total substep delta**: showcase has `4 + 12 + 2 = 18` gated substeps; minimal has `4 + 9 + 0 = 13`. Minimal is 5 substeps lighter.

## 3. Topic-delivery deltas (substep → topic branching)

Authoritative source: [`subStepToTopic()`](../../internal/workflow/recipe_guidance.go#L540-L595).

| Substep | Minimal topic | Showcase topic | recipe.md block(s) | Delta class |
|---|---|---|---|---|
| research (step-entry) | `research-minimal` only (~14KB) | `research-showcase` + `research-minimal` concatenated (~17KB) | recipe.md:5 + recipe.md:69 | showcase **extends** minimal |
| generate.scaffold | `where-to-write` (same block, minimal-aware "write yourself" framing at recipe.md:480) | `where-to-write` (same block, showcase path dispatches `scaffold-subagent-brief`) | recipe.md:411 + recipe.md:422 + recipe.md:790 | **same topic, different dispatch shape** |
| generate.app-code | **`execution-order`** (recipe.md:473) | **`dashboard-skeleton`** (recipe.md:762) | tier branch at [recipe_guidance.go:547-550](../../internal/workflow/recipe_guidance.go#L547-L550) | **topic swap** |
| deploy.init-commands | `deploy-flow` | `deploy-flow` + loaded `subagent-brief` delivery at complete-init-commands return | same block, but showcase's substep-return delivers the subagent-brief (v8.90 de-eager target — [recipe_guidance.go:556-563](../../internal/workflow/recipe_guidance.go#L556-L563)) | **same topic, showcase carries extra payload at substep return** |
| deploy.readmes | **`readme-fragments`** (recipe.md:2205 `readme-with-fragments` block, OLD v8 shape) | **`content-authoring-brief`** (recipe.md:2390, v8.94 fresh-context shape) | tier branch at [recipe_guidance.go:588-591](../../internal/workflow/recipe_guidance.go#L588-L591) | **topic swap — different block, different framing** |

**Observed guide sizes at substep returns (showcase v34 main trace)** — the following are tier-invariant topics and the sizes carry over as the minimal baseline (the block text is identical):

| Substep | v34 showcase observed size | Inherited for minimal? |
|---|---:|---|
| step=research return (→ provision guide) | 22822B | ✅ |
| step=provision return (→ generate guide) | 41942B | ✅ content-equivalent — minimal generate guide includes `what-to-generate-minimal` rather than `what-to-generate-showcase` pointer; close size |
| step=generate return (→ deploy guide) | 13843B | ✅ |
| complete deploy-dev (→ start-processes) | 8980B | ✅ |
| complete start-processes (→ verify-dev) | 1632B | ✅ |
| complete verify-dev (→ init-commands) | 8978B | ✅ |
| complete init-commands (→ subagent brief) | **21840B** | **❌ — minimal returns feature-sweep-dev guide directly, NOT the 21KB subagent-brief.** Corresponding minimal return is estimated ~3–4KB (feature-sweep-dev topic size on its own) |
| complete subagent (→ snapshot-dev) | 8977B | **❌ — substep doesn't exist for minimal** |

## 4. Sub-agent dispatch deltas

### Showcase v34 — 6 dispatches captured verbatim in [`flow-showcase-v34-dispatches/`](flow-showcase-v34-dispatches/)

| # | Dispatched at | Role | Prompt length | Source block in recipe.md |
|---|---|---|---:|---|
| 1 | 10:23:14 | scaffold-appdev-svelte-spa | 10459 | `scaffold-subagent-brief` (recipe.md:790) |
| 2 | 10:24:38 | scaffold-apidev-nestjs-api | 15627 | same block (adapted per codebase) |
| 3 | 10:25:27 | scaffold-workerdev-nestjs-worker | 8668 | same block |
| 4 | 10:46:08 | implement-all-6-nestjs-showcase-features | 14816 | `dev-deploy-subagent-brief` (recipe.md:1675) |
| 5 | 11:04:40 | write-per-codebase-readmes-claude-md | 11346 | `content-authoring-brief` (recipe.md:2390) |
| 6 | 11:20:21 | nestjs-svelte-code-review | 6256 | `code-review-subagent` (recipe.md:3050) |

### Minimal (reconstruction — no live dispatch data)

| # | Expected substep | Role | Dispatch confidence | Source block |
|---|---|---|---|---|
| 1 | deploy.readmes | readme-with-fragments | **Low** — observed minimal runs (nestjs-minimal v3) write README main-inline rather than dispatch | `readme-with-fragments` (recipe.md:2205) — captured in [`flow-minimal-spec-dispatches/readme-with-fragments.md`](flow-minimal-spec-dispatches/readme-with-fragments.md) |
| 2 | close (no gated substep) | code-review-subagent | **High** — v3 TIMELINE confirms "Verification sub-agent (NestJS expert)" fires; same block as showcase, single-codebase interpolation | `code-review-subagent` (recipe.md:3050) — captured in [`flow-minimal-spec-dispatches/code-review-subagent.md`](flow-minimal-spec-dispatches/code-review-subagent.md) |

**Net delta**: 6 dispatches (showcase) vs 0–2 dispatches (minimal). The scaffold-subagent-brief and dev-deploy-subagent-brief blocks are **showcase-only** — minimal main does all scaffold + feature work inline.

## 5. Flagged-event class deltas

| Flag class | v34 showcase count | Expected minimal | Basis |
|---|---:|---|---|
| `is_error=true` tool_results | 7 (all in sub-agents; 0 in main) | unknown — no live evidence; same substrate so class likely same | main trace shows 0 errored, subagent traces show 3/0/0/1/3/0 |
| `scope=downstream` or `scope=both` fact records | 13 records across 4 sub-agents | **structurally near-zero** — minimal has 0–1 non-main-agent sources; no cross-subagent routing to benefit from Theme B | [extract_flow.py flag list](flow-showcase-v34-main.md#L12-L29) |
| TodoWrite full-rewrites | 12 across main | **likely same class** — step-entry guidance renders as fresh planning context regardless of tier; upper bound scales with substep count (minimal: 13 substeps; showcase: 18 → 28% lighter ceiling) | [flow-showcase-v34-main.md flagged events](flow-showcase-v34-main.md) |
| Agent dispatches | 6 | 0–2 | §4 above |
| Out-of-order substep attestation (v25 class) | **0** — v8.90 held | **0 expected** — same substrate | v34 entry "substep attestations real-time in canonical order" |

## 6. Check-surface deltas

**Deferred to step 2.** The check inventory lives in [`internal/tools/workflow_checks_*.go`](../../internal/tools/); each check has a tier predicate (typically checks `plan.Tier == RecipeTierShowcase`). A full tier-delta check-surface table is step 2's `knowledge-matrix-{minimal,showcase}.md` scope. Step 1 notes only: showcase carries MORE checks (e.g. cross-codebase dedup, worker-codebase-specific assertions, showcase-only feature coverage).

## 7. What is NOT different between tiers

- Operational substrate — SSH boundary, `zerops_mount`, `zerops_deploy`, `zerops_dev_server`, git-config-mount, facts log, Read-before-Edit, env-README Go templates. All v8.78-v8.104 substrate fixes apply uniformly.
- Finalize-step shape (no substeps either side) — same `generate-finalize` + env-comment authoring + content-check pass.
- Research plan schema — `recipePlan` fields are the same; tier-specific fields (`cacheLib`, `sessionDriver`, etc.) are showcase-only per recipe.md:82 but all base fields apply to both.
- Content-surface rules — README fragment markers, zerops.yaml comment ratio/depth, env import.yaml WHY-style, CLAUDE.md byte floor, `dbDriver` database-vs-ORM rule. All apply equally per recipe-taxonomy.md and confirmed by `nestjs-minimal-v3` shape.
- Preprocessor directive `#zeropsPreprocessor=on` with `<@generateRandomString(<32>)>` — always emitted.

## 8. Reconstruction-path honesty statement

The minimal-tier evidence is spec-derived, not observation-derived. Specifically:

- **Confirmed from code** (high confidence): substep sequences, topic-per-substep mapping, tier-conditional branches, dispatch-count ceiling.
- **Confirmed from one deliverable** (medium confidence): `nestjs-minimal-v3` shape; v3 is a single run with its own agent-variance signature.
- **Inferred without observation** (low confidence): per-substep guide-size at minimal scale (assumed identical to showcase where topic is shared), inline-feature-writing tool mix, TodoWrite cadence, `is_error` frequency.

Step 2's knowledge matrix should carry these confidence levels through so that matrix cells reading "proxy from showcase v34" are explicitly distinguished from cells reading "observed from minimal session log".

If step 3 surfaces a brief-composition question that only live minimal evidence can resolve, the escalation rule triggers: commission a targeted `nestjs-minimal` run. Candidates that would trigger escalation:

1. Does main-inline feature-writing produce TodoWrite full-rewrites at step-entry, or does the narrower flow avoid them? (Bears on v34's 12-rewrite cost class.)
2. Does the OLD `readme-with-fragments` block ever get dispatched as a sub-agent, or always executed main-inline? (Bears on whether the rewrite's showcase writer atom needs a minimal counterpart or inherits from main-agent authoring atoms.)
3. Do any tier-invariant guide deliveries exceed 30KB at minimal scale, suggesting eager-topic overhead the de-eager path (v8.90) didn't anticipate? (Bears on principle #6 in the architecture rewrite.)
