# Run 11 readiness — implementation plan

Run 10 (`nestjs-showcase`, 2026-04-25) was the third v3 dogfood and the second to reach `complete-phase finalize` green. All six run-10-readiness workstreams (L / M / N / O / P / Q1..Q4) shipped before the run closed and tranche 3's brief-hygiene fixes (Q1–Q4) held under load — verified directly against `build-brief` responses. Style fixes N (yaml-comment block-level rule) and O (KB `**Topic**` format) delivered at the byte level. But the rendered deliverable failed reference parity in two structurally distinct ways: a SourceRoot regression caused stitch to write per-codebase README + CLAUDE to `/var/www/<hostname>/` (no `dev` suffix) instead of `<cb.SourceRoot>/`, which silently no-op'd M's auto-embed of `<SourceRoot>/zerops.yaml` as IG item #1; and a content-quality audit against [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) found that 7 of 15 published codebase KB bullets fail the spec's DISCARD-class litmus tests, with only 3 of 15 (20%) unambiguously spec-compliant. The classifier ([internal/recipe/classify.go:60-89](../../../internal/recipe/classify.go#L60)) trusts agent-supplied `surfaceHint` blindly; the spec's classification rules exist as prose with no programmatic gate. Adjacent: a v2 fact tool stayed registered alongside the v3 action and out-competed it on description, routing 5 of run 10's hardest-won discoveries to `legacy-facts.jsonl` — which the v3 stitch pipeline doesn't read.

Run 11 ships the foundation bugs (gap U → V → M) plus the content-discipline tightening (N, O, P, R) and finalize-pipeline polish (Q, S) so the next dogfood produces a deliverable whose per-codebase content discipline is engine-enforced rather than agent-self-graded, and whose published KB carries hard-won platform teaching rather than scaffold-debugging forensics or paraphrased platform-doc content.

Reference material (all already written):
- [docs/zcprecipator3/runs/10/ANALYSIS.md](../runs/10/ANALYSIS.md) — run 10 verdict (PAUSE), 26-criterion scorecard, §3 gaps M, N, O, P, Q, R, S, U, V (no T — TIMELINE export-gating is by design). Every gap carries a "Fix direction" subsection naming files + LoC estimates — authoritative spec for what run 11 must ship.
- [docs/zcprecipator3/runs/10/CONTENT_COMPARISON.md](../runs/10/CONTENT_COMPARISON.md) — content-quality grade vs `/Users/fxck/www/laravel-showcase-app/`. §6.5 carries the bullet-by-bullet audit; honest aggregate **2-3/10**.
- [docs/zcprecipator3/runs/10/PROMPT_ANALYSIS.md](../runs/10/PROMPT_ANALYSIS.md) — timeline / sub-agent / smell catalog. §4 fix-stack ranking-by-cost-vs-value is the tranche structure's source.
- [docs/zcprecipator3/runs/10/TIMELINE.md](../runs/10/TIMELINE.md) — main-agent-authored run 10 build log. §6 catalogs five concrete engine-behavior surprises (record-fragment append vs overwrite, runtime-vs-managed import-comments, IG list-vs-headers contradiction, IG-first-item-mention rule, meta-agent-voice false-positive on "AI Agent").
- [docs/zcprecipator3/plans/run-10-readiness.md](run-10-readiness.md) — prior run's plan. §2 workstreams L–Q shipped in v9.10.0 and stay in force; criteria 1–26 carry over.
- [docs/zcprecipator3/CHANGELOG.md](../CHANGELOG.md) — top entry (2026-04-24, "run-10-readiness") describes v9.10.0 and what L / M / N / O / P / Q1..Q4 actually delivered.
- [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) — load-bearing for run 11. Carries the classification taxonomy (Platform-invariant / Platform×framework intersection / Framework-quirk DISCARD / Library-metadata DISCARD / Scaffold-decision / Operational / Self-inflicted DISCARD), the four classification rules, and the seven surface contracts. Run 11's gap V depends on this spec being engine-enforced rather than agent-self-graded.
- [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) — apps-repo shape + style + KB-discipline reference. Reference KB has 7 bullets, zero citation-boilerplate, every bullet hard-won from running that exact stack on Zerops. Run 11's KB-discipline target.

Run-10 artifacts: [docs/zcprecipator3/runs/10/](../runs/10/)

---

## 0. Preamble — context a fresh instance needs

### 0.1 What v3 is (one paragraph)

zcprecipator3 (v3) is the Go recipe-authoring engine at [internal/recipe/](../../../internal/recipe/). Given a slug (e.g. `nestjs-showcase`), v3 drives a five-phase pipeline (research → provision → scaffold → feature → finalize) producing a deployable Zerops recipe. The engine never authors prose — it renders templates, substitutes structural tokens, splices in-phase-authored fragments into extract markers, reads committed per-codebase `zerops.yaml` verbatim from the SSHFS mount, classifies facts, and runs surface validators. Sub-agents (Claude Code `Agent` dispatch) author codebase-scoped fragments at the moment they hold the densest context; the main agent authors root + env fragments at finalize (run 10 introduced a finalize sub-agent variant — see §2.S). The `<outputRoot>` tree lives under `/var/www/recipes/<slug>/` during a live run; per-codebase apps-repo content (README + CLAUDE + `zerops.yaml` + source) lives at `<cb.SourceRoot>` = `/var/www/<hostname>dev/` (the SSHFS-mounted dev slot). v2 — the older bootstrap/develop workflow engine at [internal/content/](../../../internal/content/) + [internal/workflow/](../../../internal/workflow/) — is frozen at v8.113.0; its `zerops_record_fact` MCP tool is still registered (see §2.U).

### 0.2 Two deliverable shapes, one engine

v3 produces two published artifacts from a single run, intended for two different GitHub repositories:

- **recipes-repo shape** — published to `zeropsio/recipes/<slug>/`. Shape: root `README.md` + 6 tier folders (`0 — AI Agent/` … `5 — Highly-available Production/`), each with `README.md` + `import.yaml`. Verified canonical across ~20 existing published recipes at [/Users/fxck/www/recipes/](/Users/fxck/www/recipes/). Does NOT contain a `codebases/` subdirectory.
- **apps-repo shape** — published to `zerops-recipe-apps/<slug>-<codebase>/` (one repo per codebase). Shape: `README.md` + `CLAUDE.md` + `zerops.yaml` + full source tree, ALL at repo root. Reference: [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) — a single-codebase Laravel monolith with source (`app/`, `bootstrap/`, `config/`, `database/`, `public/`, etc.) at root alongside the three metadata files. **The reference is authoritative for shape AND voice AND KB-discipline** — when a check or validator disagrees with the reference, the check is wrong.

**Critical constraint for run 11**: the apps-repo shape's source lives at `cb.SourceRoot = /var/www/<hostname>dev/`. Run 10 closed with the engine writing per-codebase README + CLAUDE to `/var/www/<hostname>/` (no `dev` suffix) — an undocumented synthetic location nothing reads. The empirical signature (validator violation paths report relative form `"path":"api/README.md"`; `wc -l /var/www/api/README.md` succeeds for the finalize sub-agent) requires `cb.SourceRoot` to have carried the bare hostname (`"api"`/`"app"`/`"worker"`) at finalize stitch time. Run 11 fixes the silent-failure paths so the same regression cannot recur invisibly.

### 0.3 Where run 10 stopped + classes of defect

Run 10 closed all five phases GREEN on the fourth `complete-phase finalize` call. Of run-10-readiness's 26 §4 criteria, 14 PASS, 4 PARTIAL PASS, 8 FAIL ([ANALYSIS.md §2](../runs/10/ANALYSIS.md)). Tranche 1 (L + M shape fixes) failed at delivery via gap M; tranches 2 + 3 mostly held. Beyond the planned workstreams, the run produced **nine new gaps** (M, N, O, P, Q, R, S, U, V — T is reserved-as-non-issue for the TIMELINE export-gating that's by design), none architectural but all together painting a picture of a content pipeline whose validators, briefs, embedded knowledge atoms, registered-but-orphaned legacy tools, and engine path-logic disagree in ways the live agent has to discover.

Honest content-quality grade: **~2-3/10 vs reference** ([CONTENT_COMPARISON.md §6.5](../runs/10/CONTENT_COMPARISON.md)). Validators all pass; yaml-comment shape and CLAUDE.md length now match reference (real wins). But the rendered KB has two structural defects:

**Foundation bugs** (the three fixes that determine whether the published KB carries hard-won platform teaching or scaffold-debugging forensics):
- **U** — v2 `zerops_record_fact` ([internal/tools/record_fact.go:70-71](../../../internal/tools/record_fact.go#L70)) stays registered alongside v3 `zerops_recipe action=record-fact`. The v2 description ("Record a structured fact discovered during deploy for the readmes sub-step writer to consume…") out-competes v3's terser invitation, and v2's schema (`failureMode` / `fixApplied` / `evidence` / `scope` / `routeTo`) fits the natural shape of a deploy-time discovery. Run 9: 0 / 1 / 1 / 8 v2-call counts across scaffold-api / scaffold-app / scaffold-worker / features. Run 10: **2 / 0 / 3 / 0** — scaffold-api + scaffold-worker went almost pure v2. Five hard-won discoveries (`npx ts-node` cache trap, `.deployignore` wipes `node_modules`, subdomain two-step, `.deployignore` bricks `dist`, NATS contract) routed to `legacy-facts.jsonl` which the v3 stitch pipeline doesn't read. Sub-agents who noticed the silent loss re-typed 4 of 5 manually as KB bullets; the 5th (api npx ts-node trap) is permanently lost from the deliverable.
- **V** — Classifier ([classify.go:60](../../../internal/recipe/classify.go#L60)) is a switch on agent-supplied `surfaceHint`. Spec's classification rules ("self-inflicted litmus: could this be summarized as 'our code did X, we fixed it to do Y'?") exist only as prose. 7 of 15 KB bullets fail spec rules: `Trust proxy is per-framework, not per-platform` (framework-quirk by its first 6 words), `Standalone context vs HTTP app` (framework-quirk), `SIGTERM drain ordering inside nc.drain()` (framework-quirk), `.deployignore filters the build artifact` (self-inflicted — recipe author wrote `dist`, fix was removing it), plus 3 misrouted scaffold-decisions. The user's framing names this exactly: a porter cloning a working recipe doesn't have the recipe-author's self-inflicted experiences and shouldn't see them in their copy of the README.
- **M** — `cb.SourceRoot` carried bare hostnames at finalize stitch. README/CLAUDE landed at `/var/www/<h>/` not `/var/www/<h>dev/`. Empirical evidence in [ANALYSIS.md §3 gap M](../runs/10/ANALYSIS.md). M's `injectIGItem1` silently no-op'd because `readCodebaseYAMLForHost` ([assemble.go:128-141](../../../internal/recipe/assemble.go#L128)) soft-fails to `""` on missing yaml — exactly the silent-failure shape that hid M's defect.

**Content discipline** (style + structural validators that now have evidence to push on):
- **N** — Engine accepted `codebase/appdev/*` (slot hostname) as fragment id; `codebaseKnown` ([assemble.go:440-450](../../../internal/recipe/assemble.go#L440)) has a hole. Main agent dispatched a sixth sub-agent (`fix-app-fragment-ids`) to re-record under correct ids — 2m37s + 8 zerops_knowledge re-queries.
- **O** — Every KB bullet ends in `Cited guide: \`<name>\``. Citation noise propagates into env import.yaml comments (`# # (cite \`init-commands\`)`). Brief wording ("cite by name") was taken literally.
- **P** — `.deployignore` abuse traced to [internal/knowledge/themes/core.md:252](../../../internal/knowledge/themes/core.md#L252) literal phrase "Recommended to mirror `.gitignore` patterns." All three scaffold sub-agents wrote `.deployignore`; worker bricked itself by listing `dist` (~20 minutes wall time burned).
- **R** — Codebase IG validator enforces ordered-list `^\d+\.\s`; scaffold brief instructs `### N.` headers. Finalize iterates to satisfy.

**Polish** (low-risk one-shots once the above land):
- **Q** — No `git init` in any codebase. Apps-repo deliverable doesn't even have a git history.
- **S** — Run 10 introduced a finalize sub-agent dispatch. Hand-typed wrapper carries math errors ("11 hostnames × 2 = 22 fragments each") and obsolete codebase paths. Engine-composed `briefKind=finalize` eliminates the wrapper-vs-engine drift.

### 0.4 Workstream legend (U / V / M / N / O / P / R / Q / S)

Each workstream maps to one class of defect above. Tranche structure sequences them by dependency.

| Letter | Scope | Tranche |
|---|---|---|
| U | Refuse v2 `zerops_record_fact` during v3 session + enrich v3 `FactRecord` schema with `failureMode` / `fixApplied` / `evidence` / `scope` | 1 |
| V | Classifier auto-detects self-inflicted from `fixApplied` + `failureMode` shape; four KB-bullet validators (`paraphrases-cited-guide`, `no-platform-mention`, `self-inflicted-shape`, plus the existing `triple-format-banned`); brief "Self-inflicted litmus" subsection | 1 |
| M | Stitch hard-fail on non-abs / non-`dev`-suffixed `SourceRoot`; `readCodebaseYAMLForHost` hard-fail on missing yaml; `zcp sync recipe export` reads README/CLAUDE from `<SourceRoot>/` | 1 |
| N | Tighten `codebaseKnown` to reject slot hostnames + name codebase list in error; tripwire entry in scaffold brief | 2 |
| O | Citations-live-in-prose rewrite of scaffold + finalize guidance; `kb-cited-guide-boilerplate` validator | 2 |
| P | Rewrite `internal/knowledge/themes/core.md:252` `.deployignore` paragraph; scaffold-brief tripwire forbids `.deployignore`; deploy-time warn on `dist`/`node_modules`/`.git`/etc. excludes | 2 |
| R | Pick `### N.` shape; port to validator + brief | 2 |
| Q | `git init` + first commit at scaffold close mandate; `zcp sync recipe export` warns on missing `.git/` | 3 |
| S | Engine-composed `briefKind=finalize` via `build-brief` action; document finalize-sub-agent option in `phase_entry/finalize.md` | 3 |

Tranches run by dependency: Tranche 1 unblocks the foundation (V depends on U-2's enriched schema; M is independent and can land in parallel with U/V); Tranche 2 tightens content discipline inside the now-correct routing; Tranche 3 polishes the finalize loop and apps-repo git-history precondition. Run 11 can ship Tranche 1+2 and be viable; Tranche 3 is strongly recommended but not structurally blocking.

---

## 1. Goals for run 11

A recipe run of `nestjs-showcase` (or fresh slug — see §6 risks) that, compared directly to `/Users/fxck/www/laravel-showcase-app/`:

1. **Every fact recorded during scaffold + feature lands in `facts.jsonl`**, not `legacy-facts.jsonl`. v2 `zerops_record_fact` refuses with a redirect when a v3 recipe session is open. The hard-won learnings (`npx ts-node` trap, `.deployignore`-bricks-`dist` trap, NATS contract, etc.) reach the stitch pipeline.
2. **Codebase KB contains zero bullets that fail the spec's DISCARD classes** (Self-inflicted, Framework-quirk, Library-metadata). Engine-side validators enforce the spec; the classifier auto-overrides agent `surfaceHint` when fact shape is unambiguous; the brief teaches the litmus with run-10 anti-patterns.
3. **KB bullets carry zero `Cited guide:` boilerplate suffix.** Citations integrate into prose where natural or are simply omitted. Env `import.yaml` comments contain zero `(cite \`x\`)` meta-talk.
4. **Per-codebase `README.md` + `CLAUDE.md` + `zerops.yaml` land at `<cb.SourceRoot>/`** = `/var/www/<hostname>dev/`. A porter running `git init && git add -A && git commit && git push` from `/var/www/apidev/` gets a shape-equivalent repo to `laravel-showcase-app/`. Stitch hard-fails on any non-abs or non-`dev`-suffixed SourceRoot — silent failures that hid run 10's M defect cannot recur.
5. **README Integration Guide item #1 is a fenced `yaml` code block** containing the committed `zerops.yaml` verbatim. `readCodebaseYAMLForHost` hard-fails when SourceRoot is non-empty AND the yaml is missing — no more silent injection skip.
6. **Engine `record-fragment` rejects slot hostnames.** `codebase/appdev/*` returns an error naming the Plan codebase list; sub-agent retries with `codebase/app/*`. No cleanup-sub-agent dispatch needed.
7. **Zero `.deployignore` files written by scaffold sub-agents** for the canonical case (no recipe-specific need). The knowledge atom no longer recommends mirroring `.gitignore`. `zerops_deploy` warns if a `.deployignore` excludes `dist`/`node_modules`/`.git`/etc.
8. **Codebase IG fragments use `### N.` headers** consistently — validator accepts, brief mandates, no finalize iteration on shape contradiction.
9. **Each codebase SourceRoot has `.git/` initialized + at least one commit.** `zcp sync recipe export` warns if missing.
10. **Finalize brief is engine-composed** via `zerops_recipe action=build-brief briefKind=finalize`. Math, codebase paths, and citation-noise instruction come from `Plan` not from a hand-typed wrapper.

Stretch: criterion 10 from run-10-readiness (click-deployable end-to-end) becomes directly testable because the SourceRoot regression is fixed AND each SourceRoot now has a clean git history — `zcp sync recipe export` produces apps-repo-shaped trees the publish path can push to `zerops-recipe-apps/<slug>-<codebase>` as-is.

---

## 2. Workstreams

### 2.0 Guiding principles

Four invariants the implementation session must hold:

1. **No architectural work.** Every gap below is a small patch (~5–150 LoC per workstream, matching ANALYSIS.md §3 estimates). None justifies redesigning state, renaming types, splitting packages, or reshaping the classification taxonomy in the spec.
2. **Reference is authority.** [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) is the shape + style + KB-discipline target. Reference KB has 7 bullets, zero citation-boilerplate, every bullet hard-won from running that exact stack. When a check, validator, or rule disagrees with the reference, the check is wrong and gets updated.
3. **Spec is enforced, not redefined.** [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) carries the classification taxonomy and the seven surface contracts. Run 11's gap V makes the spec's invariants programmatic — it does NOT alter the taxonomy. If a class boundary feels wrong during implementation, that's a separate spec PR, not a run 11 change.
4. **Fail loud at engine boundaries.** Run 10's silent failures (M's `injectIGItem1` no-op on missing yaml; v2 fact tool's "Recorded gotcha_candidate fact" success message that routes to a file v3 can't read; `codebaseKnown`'s slot-hostname acceptance) are the structural cause of every gap. Validators that should reject MUST reject; reads that should error MUST error; tools that should refuse MUST refuse with an actionable message.

### 2.U — refuse v2 `zerops_record_fact` during v3 session + enrich v3 `FactRecord` schema

**What run 10 showed** ([ANALYSIS.md §3 gap U](../runs/10/ANALYSIS.md)): per-sub-agent v2/v3 fact-tool counts:

| Sub-agent | v3 `record-fact` (via `zerops_recipe`) | v2 `mcp__zerops__zerops_record_fact` |
|---|---|---|
| scaffold-api | **0** | **2** |
| scaffold-app | 1 | 0 |
| scaffold-worker | **0** | **3** |
| features | 7 | 0 |

Run 9's same column was 0 / 1 / 1 / 8. Run 10 scaffold-api + scaffold-worker went almost pure v2. The 5 v2-routed facts in [EXTRA_FILES/legacy-facts.jsonl](../runs/10/EXTRA_FILES/legacy-facts.jsonl) are the most useful platform-trap discoveries of the entire run:

1. **`npx ts-node` resolves against `~/.npm/_npx` cache, not project `node_modules`** — api scaffold, ~14 minutes of debugging captured. Real run-time discovery. **Permanently lost from deliverable** — agent just changed yaml to `node dist/migrate.js` and moved on.
2. **`.deployignore` listing `node_modules` wipes them post-build** — worker scaffold.
3. **Subdomain L7 route activates only after `zerops_subdomain action=enable`** — api scaffold.
4. **`.deployignore` filters the build artifact, so `dist` listed there bricks cross-deploy** — worker scaffold, the **20-minute redeploy loop**.
5. **NATS article-event subject + queue group contract** — worker scaffold, cross-codebase coordination.

Facts (2)–(5) survived because the agent saw the v2 `routeTo: "claude_md"` field and re-authored the same content manually as KB bullets. Fact (1) is the lone permanent loss.

**Why agents reach for v2 over v3** — both surface to the agent via ToolSearch. The v2 tool description ([internal/tools/record_fact.go:70-71](../../../internal/tools/record_fact.go#L70)) literally invites the use case ("non-trivial issue", "non-obvious platform behavior", "cross-codebase contract") and promises "the writer subagent reads the accumulated log as pre-organized input." That promise was true in v2's writer-dispatch flow — the description was never updated when v3 replaced the writer. The v2 schema is richer too: v2 has `type` / `title` / `substep` / `codebase` / `mechanism` / `failureMode` / `fixApplied` / `evidence` / `scope` / `routeTo`; v3's `topic` / `symptom` / `mechanism` / `surfaceHint` / `citation` is terser and forces flattening.

**Courtesy routing makes the bug invisible** — [internal/tools/record_fact.go:130-145](../../../internal/tools/record_fact.go#L130) `resolveFactLogPath` writes v2 calls to `<outputRoot>/legacy-facts.jsonl` when a v3 recipe session is open, returning "Recorded gotcha_candidate fact" success. The sub-agent has zero signal the data won't reach the deliverable. Only the v3 stitch pipeline is blind to it.

**Root cause (named files)**:
- [internal/tools/record_fact.go:130-145](../../../internal/tools/record_fact.go#L130) — `resolveFactLogPath` silently routes to legacy file.
- [internal/tools/record_fact.go:70-71](../../../internal/tools/record_fact.go#L70) — v2 description out-competes v3 on the surface a sub-agent sees.
- [internal/recipe/facts.go](../../../internal/recipe/facts.go) — v3 `FactRecord` schema lacks `failureMode` / `fixApplied` / `evidence` / `scope`; agents flatten learnings into `symptom` and discard the fix.

**Fix direction** — three edits, ordered by load-bearing:

1. **U-1 — refuse v2 during v3 session, with redirect message.** [internal/tools/record_fact.go:138-141](../../../internal/tools/record_fact.go#L138). Replace the silent route-to-`legacy-facts.jsonl` with an error: `"Error: zerops_record_fact is the v2 fact tool. A v3 recipe session is open — use zerops_recipe action=record-fact slug=<slug> instead. v3 schema: topic, symptom, mechanism, failureMode, fixApplied, evidence, scope, surfaceHint, citation."` Sub-agent sees the error, retries with v3, fact lands in `facts.jsonl`. Without this, every other run-11 fix is downstream — facts evaporating at source dwarfs every classifier improvement V can deliver. ~10 LoC.

2. **U-2 — enrich v3 `FactRecord` schema** with `failureMode` / `fixApplied` / `evidence` / `scope`. [internal/recipe/facts.go](../../../internal/recipe/facts.go). The v2 schema captured the natural shape of a deploy-time discovery; v3's terser schema forced agents to flatten. Add the missing fields, optional, JSON-tagged. **These fields are LOAD-BEARING for V-1's auto-classification** (V reads them to detect self-inflicted shape). U-2 must land before V-1 can ship. ~30 LoC.

3. **U-3 — mark v2 description deprecated for v3 sessions.** [internal/tools/record_fact.go:70](../../../internal/tools/record_fact.go#L70). Prefix the description with `"DEPRECATED for v3 recipe sessions — use zerops_recipe action=record-fact instead."` Description-level fix doesn't break v2-only callers. ~3 lines.

**Changes**:
- [internal/tools/record_fact.go](../../../internal/tools/record_fact.go) — refuse-with-redirect at `resolveFactLogPath` + description prefix. ~13 LoC.
- [internal/recipe/facts.go](../../../internal/recipe/facts.go) — schema enrichment. ~30 LoC.

**Test coverage** (new tests in [internal/tools/record_fact_test.go](../../../internal/tools/record_fact_test.go) + [internal/recipe/facts_test.go](../../../internal/recipe/facts_test.go)):
- `TestRecordFact_RefusesDuringV3Session` — fixture opens recipe session, calls v2 tool, expects refusal + the suggested-call message naming the v3 schema fields.
- `TestRecordFact_AcceptsWithoutV3Session` — outside a v3 recipe session, v2 tool keeps working (no behavior change for v2-only callers).
- `TestFactRecord_AcceptsEnrichedFields` — v3 `record-fact` accepts `failureMode` / `fixApplied` / `evidence` / `scope`; round-trips through `facts.jsonl`.
- `TestFactRecord_OptionalEnrichedFields` — v3 `record-fact` still works without the new fields (backwards-compat — no existing call breaks).

**Watch**: U-1 is the most load-bearing single change of run 11. Without it, V's classifier improvements run against records that never arrive. Order U-1 + U-2 first commit; everything else depends on the schema and the routing being right.

### 2.V — engine-side enforcement of spec's classification taxonomy

**What run 10 showed** ([ANALYSIS.md §3 gap V](../runs/10/ANALYSIS.md), [CONTENT_COMPARISON.md §6.5](../runs/10/CONTENT_COMPARISON.md)): the spec at [docs/spec-content-surfaces.md §"Fact classification taxonomy"](../../spec-content-surfaces.md) names three classes that MUST be DISCARDED — Self-inflicted, Framework-quirk, Library-metadata — plus a fourth litmus: *"Could this observation be summarized as 'our code did X, we fixed it to do Y'? If yes, discard."* Run 10's published KB against that bar:

| Bullet | Spec class | Agent's `surfaceHint` | Spec verdict |
|---|---|---|---|
| api #1 — *"Trust proxy is per-framework, not per-platform"* (the bullet's first 6 words self-classify) | **Framework-quirk** | platform-trap | **DISCARD** |
| api #8 — *"initCommands paths must match the deployed layout"* — describes a recipe-specific `deployFiles` shape | **Scaffold-decision** | platform-trap | route to `zerops.yaml` comment, NOT KB |
| worker #2 — *"Standalone context vs HTTP app"* — pure NestJS framework difference | **Framework-quirk** | platform-trap | **DISCARD** |
| worker #4 — *".deployignore filters the build artifact"* — recipe-author wrote `dist` into deployignore, deploy bricked, fix was removing `dist` | **Self-inflicted** per rule 4 | platform-trap | **DISCARD** |
| worker #5 — *"SIGTERM drain ordering inside `nc.drain()`"* — NATS client library lifecycle | **Framework-quirk** | platform-trap | **DISCARD** |
| worker #6 — *"Worker has no HTTP probes by design"* — recipe-specific shape choice | **Scaffold-decision** | platform-trap | route to `zerops.yaml` comment |
| worker #1 — *"NATS contract: subjects + queue group"* — cross-codebase contract specific to THIS recipe | **Scaffold-decision** | platform-trap | route to CLAUDE.md or IG, NOT KB |

**7 of 15 codebase KB bullets fail the spec's classification rules.** All shipped because the agent labeled them `platform-trap` and the classifier didn't push back. Only **3 of 15 (20%)** are unambiguously spec-compliant.

**Why agents misclassify** — the user's framing names this exactly: a porter cloning a working recipe doesn't have the recipe-author's self-inflicted experiences. But the agent that just spent 14 minutes debugging `npx ts-node` rationalizes the experience as "this was a platform trap" because the suffering FELT like a trap. The spec's self-inflicted litmus (rule 4) requires a meta-cognitive step the agent skips under flow-state. Worker's `dist`-in-`.deployignore` mistake gets the same rationalization: "the platform filters dist, this is platform behavior worth teaching." But the platform's behavior is uninteresting unless someone wrote `dist` into `.deployignore` first — which a working-recipe-clone porter would never do.

**Root cause (named files)**:
- [internal/recipe/classify.go:60-89](../../../internal/recipe/classify.go#L60) — `Classify` is a switch on `r.SurfaceHint`. Whatever the agent labeled the fact as, that's the class it gets. Zero independent verification.
- [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go) — KB validators check format (`triple-format-banned` from O) but not content discipline against the spec's litmus.
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — KB guidance teaches `**Topic**` format but doesn't teach the self-inflicted / framework-quirk litmus with concrete anti-patterns.
- [docs/spec-content-surfaces.md §"Fact classification taxonomy"](../../spec-content-surfaces.md) — taxonomy + litmus rules exist as prose only.

**Fix direction** — five sub-fixes, all load-bearing for the §3 acceptance criteria. **V-1 depends on U-2's enriched schema** (it reads `failureMode` + `fixApplied`).

**V-1 — `Classify` auto-detects self-inflicted from `fixApplied` + `failureMode` shape**:
- [internal/recipe/classify.go:60](../../../internal/recipe/classify.go#L60) entry. If `fixApplied` describes a change to recipe source/yaml (regex over `fixApplied` matching e.g. `(removed|added|changed) .* from .*\.(yaml|ts|js|json)`, or "switched X to Y" on a recipe-relative path) AND `failureMode` describes a symptom raised only because of that source/yaml (no platform-side mechanism vocabulary), return `ClassSelfInflicted` regardless of agent's `surfaceHint`. Override the agent's hint and emit a one-line warning to the recipe session log naming the litmus the fact failed: `"fact F<n> auto-reclassified self-inflicted (rule 4): fixApplied describes recipe-source change without platform-side mechanism — discard, not KB."` Deterministic regex-based check; no LLM grading. ~30 LoC.

**V-2 — `kb-bullet-paraphrases-cited-guide` validator**:
- [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go). For each KB bullet with a `Cited guide:` footer (or in-prose citation matching the citation map's known guide IDs from spec §"Citation map"), fetch the cited guide's body via the embedded knowledge index, compute key-phrase Jaccard similarity against the bullet body, flag if overlap > 0.5. Spec rule 3: *"If a guide exists, the fact is probably a platform invariant the platform already documents — route as gotcha WITH citation, don't duplicate the guide's content."* Bullet must add new content beyond the cited guide. Deterministic Jaccard on tokenized stop-word-stripped phrase sets; no LLM grading. ~40 LoC.

**V-3 — `kb-bullet-no-platform-mention` validator**:
- Same file. Scan KB bullets for platform-side coupling vocabulary (hardcoded list: `Zerops`, `L7`, `balancer`, `subdomain`, `zerops.yaml`, `zsc`, `execOnce`, `${...}` env-ref pattern, `zeropsSubdomain`, `httpSupport`, `runtime card`, `managed service`, runtime hostnames present in `Plan.Codebases[*].Hostname` + `Plan.ManagedServices[*].Hostname`). A bullet with zero platform mentions and only framework concerns (`NestJS controller`, `Express middleware`, `Svelte mount`, `nc.drain()`) is framework-quirk per spec — reject. Phrase-list check; no LLM grading. ~30 LoC.

**V-4 — `kb-bullet-self-inflicted-shape` regex validator**:
- Same file. Regex-flag bullets containing first-person/recipe-author voice: `\b(we (chose|tried|fixed|discovered|added|switched)|I (added|switched|noticed)|the fix was|after running)\b`, `\b(my|our) (code|setup|scaffold)\b`. These phrase signatures are scaffold-debugging forensics, not platform teaching. Emit violation with message `"first-person/recipe-author voice — KB content speaks to porter, not from author. Move to commit message or discard. See spec §'How to classify' rule 4."`. ~20 LoC.

**V-5 — brief teaches the litmus with run-10 anti-patterns**:
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) + [internal/recipe/content/briefs/feature/content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md). New `### Self-inflicted litmus` subsection (tightly written, ~15 lines per brief):
  - One-line restatement of spec rule 4.
  - Three labeled anti-patterns from run 10:
    - "*'we chose `npx ts-node`, it failed, switched to `node dist/migrate.js`'* — **DISCARD**, code-fix not platform teaching."
    - "*'wrote `dist` into `.deployignore`, deploy bricked, removed `dist`'* — **DISCARD**, recipe-author error not porter trap."
    - "*'Trust proxy is per-framework, not per-platform'* — **DISCARD**, framework-quirk per spec, belongs in NestJS docs."
  - One-line operational rule: "Before recording a KB-eligible fact, ask: would a porter cloning this finished recipe (with the FIXED yaml + FIXED .deployignore) ever encounter this? If no, discard."

**Changes**:
- [internal/recipe/classify.go](../../../internal/recipe/classify.go) — V-1 auto-classification + warning emit. ~30 LoC.
- [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go) — V-2 + V-3 + V-4 validators. ~90 LoC.
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — V-5 anti-pattern subsection. ~15 lines.
- [internal/recipe/content/briefs/feature/content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — V-5 cross-reference. ~10 lines.
- Engine total: ~120 LoC + ~30 lines of brief content.

**Test coverage** (new tests in [classify_test.go](../../../internal/recipe/classify_test.go) + new test file `validators_codebase_kb_test.go` if needed):
- `TestClassify_SelfInflictedFromFixApplied` — fact with `fixApplied: "removed dist from .deployignore"` + `failureMode: "Cannot find module /var/www/dist/main.js"` + `surfaceHint: "platform-trap"` → returns `ClassSelfInflicted` + emits warning containing "rule 4" and the fact's index.
- `TestClassify_PlatformInvariantFromGenuineFix` — fact with `fixApplied: "set app.set('trust proxy', true)"` + `failureMode: "req.ip returned VXLAN peer"` + `surfaceHint: "platform-trap"` → keeps `ClassPlatformTrap` (the fix is correct framework usage of a platform invariant — the trust-proxy/L7 intersection IS platform-side teaching despite living in framework code).
- `TestValidateKB_RejectsCitedGuideParaphrase` — KB bullet whose body Jaccard-overlaps `http-support` guide body > 0.5 + cites `http-support` → flagged with `kb-bullet-paraphrases-cited-guide`.
- `TestValidateKB_AcceptsCitedGuideExtension` — KB bullet citing `http-support` but Jaccard < 0.5 → passes (genuinely new content beyond the guide).
- `TestValidateKB_RejectsNoPlatformMention` — KB bullet about pure NestJS controller behavior with zero platform vocabulary → flagged with `kb-bullet-no-platform-mention`.
- `TestValidateKB_AcceptsBulletNamingZeropsExplicitly` — bullet mentioning `subdomain` + `zerops_subdomain` → passes platform-mention check.
- `TestValidateKB_RejectsFirstPersonShape` — bullet with `"we tried npx ts-node, it failed"` → flagged with `kb-bullet-self-inflicted-shape`.
- `TestValidateKB_AcceptsPorterVoice` — bullet with `"the L7 balancer rewrites the Host header"` → passes self-inflicted-shape check.
- `TestBrief_Scaffold_ContainsSelfInflictedLitmus` — brief body contains `### Self-inflicted litmus` heading + the three anti-pattern phrases.

**Watch**: applying §V's rules to run 10 today would have cut the api KB from 9 bullets to 4–5 (subdomain-two-step, NATS Pattern A, forcePathStyle, scheme-vs-host, env-var injection). Worker KB would drop from 6 to 2. **That's the spec-aligned outcome, not a regression.** Reference Laravel KB has 7 hard-won bullets; what matters is per-bullet truth-density, not bullet count. Run-11-readiness §4 carries no "KB ≥ N bullets" criterion — that's the kind of presence-check that pressured run 9-10 into padding.

### 2.M — stitch hard-fail on bad SourceRoot; readCodebaseYAMLForHost hard-fail on missing yaml

**What run 10 showed** ([ANALYSIS.md §3 gap M](../runs/10/ANALYSIS.md)): finalize sub-agent's Bash at [agent-ad2a L139](../runs/10/SESSION_LOGS/subagents/agent-ad2acd2987b1f9ac6.jsonl) succeeded on `wc -l /var/www/{api,app,worker}/{README,CLAUDE}.md`. The legitimate SourceRoot directories `/var/www/{apidev,appdev,workerdev}/` carry source + `zerops.yaml` only; no README + no CLAUDE. `/var/www/<hostname>/` (no `dev` suffix) is NOT documented anywhere — a synthesized path the engine doesn't claim to write to.

**Two pieces of empirical evidence** require `cb.SourceRoot` to have carried bare hostnames at finalize stitch time:

- Validator violation paths report relative codebase-hostname-prefixed form: [agent-ad2a L99 first complete-phase response](../runs/10/SESSION_LOGS/subagents/agent-ad2acd2987b1f9ac6.jsonl) carries `"path":"api/README.md"` — exactly what `filepath.Join("api", "README.md")` returns. `codebasePaths` builds these via `filepath.Join(cb.SourceRoot, leaf)` ([validators.go:215](../../../internal/recipe/validators.go#L215)) — so `cb.SourceRoot` MUST have been the bare hostname.
- `wc -l` succeeded on `/var/www/{api,app,worker}/{README,CLAUDE}.md`. With `cb.SourceRoot = "api"` and the MCP server's process cwd at `/var/www/`, `filepath.Join("api", "README.md")` resolves writes to `/var/www/api/README.md` exactly.

This contradicts what HEAD's `DefaultSourceRoot(hostname) = "/var/www/" + hostname + "dev"` produces. The exact divergence between the running binary and committed source is **not run-11 scope to forensically re-litigate** ([ANALYSIS.md §3 gap M](../runs/10/ANALYSIS.md) names three plausible causes); run 11's job is to make the silent-failure paths fail loud so the same regression cannot recur invisibly. The user has the binary state to confirm which cause was real.

**Why M's `injectIGItem1` also failed**: same root cause. [assemble.go:131-141](../../../internal/recipe/assemble.go#L131) `readCodebaseYAMLForHost` reads `<cb.SourceRoot>/zerops.yaml`. If SourceRoot is `/var/www/api/`, it tries `/var/www/api/zerops.yaml` — which doesn't exist (file is at `/var/www/apidev/zerops.yaml`). Function returns `""` on read error (soft fail), causing [assemble.go:119-121](../../../internal/recipe/assemble.go#L119) to skip the injection entirely. No yaml block ever appears in the rendered IG. The validator `codebase-ig-first-item-not-zerops-yaml` was satisfied by ANY item-1 prose mentioning the string `zerops.yaml` — which the prose item-1 does, ambiently.

**Why the user saw "extra src folders inside `nestjs-showcase`"** ([ANALYSIS.md §3 gap M](../runs/10/ANALYSIS.md) "Why the user saw…" subsection): `zcp sync recipe export` ([sync/export.go:295-300](../../../internal/sync/export.go#L295)) calls `overlayStagedWriterContent(tw, stagedDir, archiveAppDir)` with `stagedDir = filepath.Join(recipeDir, appName)` = `/var/www/nestjs-showcase/apidev/` — a path that ALSO doesn't exist post-§L since stitch writes to SourceRoot. Export tool was pinned at run-9-shaped layout, never updated for §L.

**Root cause (named files)**:
- [internal/recipe/handlers.go:444-446](../../../internal/recipe/handlers.go#L444) — only checks `cb.SourceRoot == ""`. Doesn't check abs-ness or `dev` suffix. Relative path resolves through cwd silently.
- [internal/recipe/assemble.go:128-141](../../../internal/recipe/assemble.go#L128) — `readCodebaseYAMLForHost` soft-fails to `""` on missing file. Caller `assembleCodebaseREADME` skips IG-item-1 injection silently. No log, no warning, no error.
- [internal/sync/export.go:295](../../../internal/sync/export.go#L295) — `overlayStagedWriterContent` reads from `<recipeDir>/<appName>/` not `<SourceRoot>/`.

**Fix direction** — three edits + one diagnostic step:

1. **M-0 (diagnostic, optional pre-step)** — rebuild zcp from the run-10 working tree, fire `enter-phase scaffold`, dump `Plan.Codebases[*].SourceRoot`. Result determines which of the three causes [ANALYSIS.md §3 gap M](../runs/10/ANALYSIS.md) names was real. Not strictly needed before the fixes below ship — M-1 + M-2 force loud failure regardless of cause — but useful for post-mortem closure.

2. **M-1 — stitch hard-fail on non-absolute or non-`dev`-suffixed SourceRoot**. [internal/recipe/handlers.go:444-446](../../../internal/recipe/handlers.go#L444). Add `filepath.IsAbs(cb.SourceRoot)` check + a `strings.HasSuffix(cb.SourceRoot, "dev")` check. Errors loud naming the codebase + bad path: `"stitch refused: codebase %q has invalid SourceRoot %q (expected absolute path ending in 'dev'; got bare/relative form). This indicates the gap-M regression — see docs/zcprecipator3/runs/10/ANALYSIS.md §3 gap M."` ~5 LoC.

3. **M-2 — hard-fail `readCodebaseYAMLForHost` on missing yaml when SourceRoot is non-empty**. [internal/recipe/assemble.go:128-141](../../../internal/recipe/assemble.go#L128). Current soft-fail-to-empty-string is the reason §M's `injectIGItem1` silently no-op'd. Return `error` (not `(string, nil)`) when SourceRoot is non-empty AND yaml file is missing OR unreadable. Caller `assembleCodebaseREADME` either: (a) propagates the error and stitch fails loud, or (b) explicitly tolerates with a logged warning when the yaml hasn't been authored yet (early-phase render path). Pick (a) for finalize-time stitch where the yaml MUST exist; (b) only for genuinely pre-scaffold renders. ~10 LoC.

4. **M-3 — `zcp sync recipe export` reads README/CLAUDE from `<SourceRoot>/`**. [internal/sync/export.go:295](../../../internal/sync/export.go#L295). Replace `stagedDir = filepath.Join(recipeDir, appName)` with `stagedDir = cb.SourceRoot` (resolved per-codebase from the recipe session's Plan). The export tool was pinned at the pre-§L layout; align with stitch. ~5 LoC.

**Changes**:
- [internal/recipe/handlers.go](../../../internal/recipe/handlers.go) — SourceRoot validation. ~5 LoC.
- [internal/recipe/assemble.go](../../../internal/recipe/assemble.go) — hard-fail readCodebaseYAMLForHost. ~10 LoC.
- [internal/sync/export.go](../../../internal/sync/export.go) — read from SourceRoot. ~5 LoC.

**Test coverage** (new tests in [handlers_test.go](../../../internal/recipe/handlers_test.go) + [assemble_test.go](../../../internal/recipe/assemble_test.go)):
- `TestStitch_NonAbsSourceRoot_HardFails` — fixture seeds session with `cb.SourceRoot = "api"` (the relative bug-shape), triggers stitch, expects an error naming the codebase + the violation. The regression guard.
- `TestStitch_NonDevSuffixedSourceRoot_HardFails` — fixture with `cb.SourceRoot = "/var/www/api"` (absolute but no `dev`), expects the same error.
- `TestStitch_AbsoluteDevSourceRoot_Passes` — `cb.SourceRoot = "/tmp/test-apidev"`, README + CLAUDE land at `/tmp/test-apidev/README.md` + `/tmp/test-apidev/CLAUDE.md`.
- `TestReadCodebaseYAMLForHost_MissingYaml_ReturnsError` — SourceRoot points to a temp dir without `zerops.yaml`, expects error (not soft fail).
- `TestReadCodebaseYAMLForHost_PresentYaml_ReturnsBody` — SourceRoot has `zerops.yaml`, returns body.
- `TestStitch_IGItem1Injected_AfterMFix` — full assemble round, IG section opens with `### 1. Adding \`zerops.yaml\`` followed by fenced yaml block whose content matches `<SourceRoot>/zerops.yaml` byte-for-byte.

**Watch**: M-2's tightening could surface latent test fixtures that relied on the soft-fail behavior. Run `go test ./internal/recipe/... -count=1` after the change; expect 1–3 fixtures needing SourceRoot prefab (cheap to fix). Document the contract change in the assemble-package doc comment.

### 2.N — fragment-id validation rejects slot hostnames

**What run 10 showed** ([ANALYSIS.md §3 gap N](../runs/10/ANALYSIS.md)): scaffold-app recorded all 5 fragments under `codebase/appdev/*` (slot hostname) instead of `codebase/app/*` (codebase hostname). Engine returned `ok:true` on every call. Main agent noticed at scaffold-close, dispatched a sixth sub-agent (`fix-app-fragment-ids`, [agent-a5f6](../runs/10/SESSION_LOGS/subagents/agent-a5f64c5563660c498.jsonl)) which spent 2m37s + 8 zerops_knowledge re-queries to re-record under correct ids.

Same-codebase `apidev/intro` and `workerdev/intro` mistakes were both self-corrected inline (the agents wrote the slot form first, noticed, re-wrote with codebase form). For scaffold-app, all 5 fragments stayed under `appdev`; the agent never noticed.

**Engine code claims to reject this**: [handlers_fragments.go:131-148](../../../internal/recipe/handlers_fragments.go#L131) `isValidFragmentID` for `codebase/<host>/<tail>` calls `codebaseKnown(plan, host)`; [assemble.go:440-450](../../../internal/recipe/assemble.go#L440) iterates `plan.Codebases` and matches `c.Hostname == host`. With Plan codebases `api`, `app`, `worker`, `codebaseKnown(plan, "appdev")` SHOULD return `false`. Yet [agent-afd4 L168 record-fragment + L169 result `{"ok":true,"fragmentId":"codebase/appdev/intro","bodyBytes":594}`](../runs/10/SESSION_LOGS/subagents/agent-afd480b8d86f34284.jsonl).

The mid-flight engine swap that broke gap M ([ANALYSIS.md §3 gap M](../runs/10/ANALYSIS.md)) is the most likely cause — a pre-§L code path may have had a looser `codebaseKnown`. Run 11's job: tighten + add an actionable error message + pin a regression test against the live build.

**Root cause (named files)**:
- [internal/recipe/assemble.go:440-450](../../../internal/recipe/assemble.go#L440) — `codebaseKnown` returns `bool` only, no error context.
- [internal/recipe/handlers_fragments.go:131-148](../../../internal/recipe/handlers_fragments.go#L131) — `isValidFragmentID` for `codebase/<host>/<tail>` returns generic invalid-id error if `codebaseKnown` returns false; doesn't name the Plan codebase list or hint at the slot-vs-codebase distinction.
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — `## Validator tripwires` section doesn't name the slot-vs-codebase pitfall.

**Fix direction** — two edits:

1. **N-1 — tighten + actionable error message**. [internal/recipe/assemble.go:440-450](../../../internal/recipe/assemble.go#L440). Refactor `codebaseKnown(plan, host) bool` → `codebaseKnown(plan, host) error` returning nil on match, `fmt.Errorf("unknown codebase %q (Plan codebases: %v; if you used a slot hostname like 'appdev'/'appstage', use the bare codebase name 'app' instead)", host, hostnames(plan))` on miss. Update `isValidFragmentID` caller at [handlers_fragments.go:131-148](../../../internal/recipe/handlers_fragments.go#L131) to surface the error verbatim in the `record-fragment` response. ~10 LoC.

2. **N-2 — author-time tripwire**. [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md). Add to `## Validator tripwires`: *"Fragment IDs use `cb.Hostname` (the codebase name, e.g. `app`) — NEVER the slot hostname (`appdev` / `appstage`). The slot is the SSHFS mount; the codebase is the logical name. Engine rejects `codebase/appdev/intro`."* ~3 lines.

**Changes**:
- [internal/recipe/assemble.go](../../../internal/recipe/assemble.go) — `codebaseKnown` returns error. ~10 LoC.
- [internal/recipe/handlers_fragments.go](../../../internal/recipe/handlers_fragments.go) — surface the error.
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — tripwire entry. ~3 lines.

**Test coverage**:
- `TestRecordFragment_RejectsSlotHostname` — Plan with codebase `app`, request `record-fragment fragmentId="codebase/appdev/intro"` — expect error code + message naming `Plan.Codebases[*].Hostname` list AND naming the slot-vs-codebase distinction.
- `TestRecordFragment_AcceptsCodebaseHostname` — same Plan, `fragmentId="codebase/app/intro"` — `ok:true`.
- `TestBrief_Scaffold_ContainsSlotHostnameTripwire` — brief body contains the slot-vs-codebase tripwire phrase.

**Watch**: if a future Plan model introduces explicit slot fragment surfaces (`codebase/appdev/*` for slot-specific overrides), this validator becomes too strict. Not in run-11 scope; the slot-vs-codebase distinction stays binary.

### 2.O — citations live in prose; ban `Cited guide:` boilerplate

**What run 10 showed** ([ANALYSIS.md §3 gap O](../runs/10/ANALYSIS.md), [CONTENT_COMPARISON.md §5](../runs/10/CONTENT_COMPARISON.md)): every KB bullet ends with literal `Cited guide: \`<name>\`.`. Sample from [agent-a3c4 L298](../runs/10/SESSION_LOGS/subagents/agent-a3c422bac6f90c39c.jsonl):

> - **Trust proxy is per-framework, not per-platform** — Express (under NestJS) parses `X-Forwarded-For` only when `trust proxy` is enabled. The L7 balancer always sets the header, but Express defaults to ignoring it because most local environments do not have a proxy. Enable it explicitly via `app.set('trust proxy', true)` or `req.ip` keeps reporting the VXLAN peer. **Cited guide: `http-support`.**

The reference at [/Users/fxck/www/laravel-showcase-app/README.md:347-355](/Users/fxck/www/laravel-showcase-app/README.md) does NOT have a `Cited guide:` boilerplate at the end of each bullet — citations attach in prose ("Per the http-support guide…") or are simply omitted when the rule is platform-evident.

**Worse**: citation discipline propagates into env import.yaml comments. [environments/0 — AI Agent/import.yaml:72](../runs/10/environments/0%20—%20AI%20Agent/import.yaml): `# # (cite \`init-commands\` via the nodejs@22 hello-world guide).` — meta-talk about citations inside a yaml comment whose audience is the click-deploying porter.

**Source of the noise** — scaffold brief's Citation map section ("call `zerops_knowledge` on the matching guide id first and cite it by name") + finalize wrapper's "Cite guides by name in the prose" instruction. Sub-agents took these literally and produced citation boilerplate at every opportunity.

**Root cause (named files)**:
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — Citation map section reads as a render-time mandate.
- Finalize wrapper (today hand-typed by main agent — see §2.S) — same "Cite guides by name in the prose" line; gets re-typed every run with the same noise outcome.
- [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go) — no validator pushes back on the boilerplate shape.

**Fix direction** — two edits:

1. **O-1 — reword scaffold + finalize guidance**. [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md): rewrite Citation map section to *"Citations are signals to YOU at author-time — call `zerops_knowledge` BEFORE writing a KB bullet that touches `env-var-model` / `http-support` / etc. — but do NOT mention 'Cited guide: <name>' in the rendered output. The bullet's prose IS the citation; if you couldn't write the bullet without consulting the guide, the bullet correctly reflects the guide's content. Spec rule 3: don't duplicate guide content as paraphrase — add new intersection content beyond it (V-2 enforces)."* ~6 lines change.

   Same change in the finalize wrapper guidance — but since §2.S delivers an engine-composed finalize brief, the wording lands once in [briefs.go](../../../internal/recipe/briefs.go)'s `BuildFinalizeBrief` body (single source of truth) instead of in the hand-typed wrapper template.

2. **O-2 — `kb-cited-guide-boilerplate` validator**. [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go). Regex against KB bullet bodies: `\*\*Cited guide:\s*` or `\bCited guide:\s*\`` at end-of-bullet. Flag with message: *"Citations belong in prose, not as boilerplate. Restate the rule in the bullet's own words; if you couldn't, the rule isn't yours to write. See spec §'Citation map' — citations are author-time signals, not render output."* ~25 LoC. Also scan env `import.yaml` comments for `(cite \`x\`)` / `# # (cite ` patterns and flag with the same error class.

**Changes**:
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — Citation map rewrite. ~6 lines.
- [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go) — `kb-cited-guide-boilerplate` + `env-yaml-cite-meta` regex validators. ~30 LoC.
- (Finalize wrapper change rolls into §2.S's engine-composed brief.)

**Test coverage**:
- `TestValidateKB_CitedGuideBoilerplate_Flagged` — KB with `**Cited guide: \`http-support\`.**` at bullet-end → one violation per bullet.
- `TestValidateKB_InProseCitation_Passes` — bullet with "Per the http-support guide…" inline → passes.
- `TestValidateKB_NoCitation_Passes` — bullet with no citation at all → passes.
- `TestValidateEnvYAML_CiteMetaInComment_Flagged` — env import.yaml comment with `# (cite \`init-commands\`)` → flagged.

**Watch**: V-2's `kb-bullet-paraphrases-cited-guide` overlaps semantically — V-2 catches paraphrase WITH citation; O-2 catches citation-shape regardless of overlap. Both should fire; the messages must be distinct so authors can act on the right one. Different violation IDs; different remediation steers.

### 2.P — `.deployignore` knowledge atom rewrite + scaffold tripwire + deploy-time warning

**What run 10 showed** ([ANALYSIS.md §3 gap P](../runs/10/ANALYSIS.md)): all three scaffold sub-agents wrote `.deployignore` to their SourceRoot. Contents differed but all three include `.git`, `.idea`, `.vscode`, `*.log`. None are Zerops-relevant deploy concerns — `.git` is auto-excluded by the Zerops builder; `.idea`/`.vscode`/`*.log` are `.gitignore` territory. Worker also listed `dist/` (the deploy artifact!), bricking its cross-deploy for ~20 minutes:

- [agent-a2b6 L74 first write](../runs/10/SESSION_LOGS/subagents/agent-a2b6d52f23372ebef.jsonl): `node_modules\ndist\n.git\n.idea\n.vscode\n*.log\n` — listed BOTH `node_modules` AND `dist`!
- L111 (~20 minutes later): `.git\n.idea\n.vscode\n*.log\ndist\n` — fixed `node_modules`, kept `dist`.
- L251 (final): `.git\n.idea\n.vscode\n*.log\n` — fixed.
- TIMELINE §3 worker: "initial cross-deploy hit `Cannot find module '/var/www/dist/main.js'` looping every 2 seconds. Root cause: `.deployignore` listed `dist`. Fix: remove. Cost ~20 min of redeploy iterations to isolate."

**Root cause is in the embedded knowledge corpus** at [internal/knowledge/themes/core.md:252](../../../internal/knowledge/themes/core.md#L252):

> **`.deployignore`**: Place at repo root (gitignore syntax) to exclude files/folders from deploy artifact. NOT recursive into subdirectories by default. **Recommended to mirror `.gitignore` patterns.** Also works with `zcli service deploy`.

Sub-agents that consult `zerops_knowledge` for runtime guides hit this paragraph and follow the recommendation literally. Worker's `dist`-in-`.deployignore` mistake generalized: "mirror `.gitignore`" + "exclude build outputs from version control" → "exclude `dist/` from deploy" — which contradicts how Zerops cross-deploy works.

**Root cause (named files)**:
- [internal/knowledge/themes/core.md:252](../../../internal/knowledge/themes/core.md#L252) — the literal recommendation that sub-agents act on.
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — `## Validator tripwires` doesn't name the `.deployignore` pitfall.
- [internal/tools/deploy.go](../../../internal/tools/deploy.go) (or equivalent — verify path) — no warn on `.deployignore` content shape at deploy time.

**Fix direction** — three edits:

1. **P-1 — rewrite the knowledge atom**. [internal/knowledge/themes/core.md:252](../../../internal/knowledge/themes/core.md#L252). Replace the offending paragraph:

   > **`.deployignore`**: Most projects do NOT need this file. The Zerops builder already excludes `.git/`. Editor metadata (`.idea/`, `.vscode/`), log files (`*.log`), and OS junk belong in `.gitignore`, not `.deployignore`. Use `.deployignore` only for the narrow case where a path is committed to git (so `.gitignore` doesn't catch it) AND must NOT ship to the runtime — e.g. fixture data, test artifacts, build-tool config the runtime doesn't read. **Never list `dist/`, `node_modules/`, or anything `deployFiles` selects** — that filters the deploy artifact and bricks the runtime.

   ~5 lines.

2. **P-2 — scaffold-brief tripwire forbids `.deployignore` author-time**. [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) `## Validator tripwires`: *"Do NOT author `.deployignore` reflexively. Most recipes do not need it (the builder excludes `.git/`; editor metadata belongs in `.gitignore`). Author one only if the recipe has a specific reason — and NEVER list `dist`, `node_modules`, or anything in `deployFiles`. Worker run-10 burned 20 minutes on `dist`-in-`.deployignore`."* ~3 lines.

3. **P-3 — `zerops_deploy` warns on common-trap `.deployignore` content**. Locate the deploy handler ([internal/tools/deploy.go](../../../internal/tools/deploy.go) or [internal/ops/deploy.go](../../../internal/ops/deploy.go) — verify against current tree). If `<SourceRoot>/.deployignore` exists, parse it and warn (do not block) when any of these lines appear: `dist`, `dist/`, `node_modules`, `node_modules/`, `.git`, `.git/`, `.idea`, `.vscode`, `*.log`. Warning message names each line + asks rationale. Hard reject `dist` and `node_modules` (both are deploy-artifact-or-bundled-deps; including either is always wrong). ~30 LoC.

**Changes**:
- [internal/knowledge/themes/core.md](../../../internal/knowledge/themes/core.md) — `.deployignore` paragraph rewrite. ~5 lines.
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — tripwire entry. ~3 lines.
- Deploy handler — `.deployignore` lint + warn/reject. ~30 LoC.

**Test coverage**:
- `TestKnowledge_CoreThemes_DeployignoreParagraph_NoMirrorGitignore` — assert the atom body does NOT contain "Recommended to mirror `.gitignore`".
- `TestBrief_Scaffold_DeployignoreTripwire` — brief body contains the `.deployignore` tripwire phrase.
- `TestDeploy_DeployignoreContainsDist_Rejects` — fixture deploys SourceRoot with `.deployignore` containing `dist`, deploy returns error naming the line.
- `TestDeploy_DeployignoreContainsEditorMetadata_Warns` — `.idea`/`.vscode`/`*.log` lines → deploy proceeds with warning.
- `TestDeploy_NoDeployignore_Passes` — most-common case: no `.deployignore` exists, deploy proceeds quietly.

**Watch**: P-3's hard-reject on `dist`/`node_modules` could surface a recipe with a legitimate reason to exclude one (e.g. monorepo where `dist` is a sibling package's output, not this codebase's deploy artifact). Not anticipated for run 11's `nestjs-showcase` target; if surfaced later, add a `# zerops-deploy-acknowledged: dist` opt-out comment.

### 2.R — IG validator vs scaffold-brief shape contradiction

**What run 10 showed** ([ANALYSIS.md §3 gap R](../runs/10/ANALYSIS.md), [TIMELINE.md §6 finding 3](../runs/10/TIMELINE.md)): codebase IG validator enforces markdown ordered-list (`^\d+\.\s`); scaffold brief instructs `### N. <title>` headers. The scaffolds had to be rewritten at finalize. Final state in [agent-ad2a L149](../runs/10/SESSION_LOGS/subagents/agent-ad2acd2987b1f9ac6.jsonl) actually shows `### 1.` through `### 9.` headers, suggesting the validator may have been loosened mid-iteration or the agent's interpretation of the message was wrong — either way, **the brief and validator must agree**.

**Decision**: pick `### N. <title>` shape (matches reference's IG headers, more readable than ordered-list-only).

**Root cause (named files)**:
- [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go) — IG validator regex enforces `^\d+\.\s` (or accepts both unclearly).
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — IG guidance instructs `### N.`.

**Fix direction** — two edits:

1. **R-1 — port `### N.` to validator**. [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go). Update the IG-item regex to accept `^### \d+\.\s` headers as the canonical shape. Keep ordered-list `^\d+\.\s` as a deprecation-acceptable fallback for one release with a violation message: *"prefer `### N. <title>` headers over plain ordered-list — see scaffold brief"*. Or, more cleanly, require `### N.` and reject plain ordered-list (since the engine generates IG item #1 with the heading shape per §M, sub-agents' items must match). Pick the strict variant. ~15 LoC.

2. **R-2 — scaffold brief mandates `### N. <title>`**. Reword IG guidance to remove ambiguity: *"Each IG item is a `### N. <title>` heading followed by porter-facing prose. Item #1 is engine-generated (yaml block, see §M); your fragment authors items starting at `### 2. <title>`. Do NOT use plain ordered-list bullets — the validator rejects them. The reference at `/Users/fxck/www/laravel-showcase-app/README.md` uses this exact shape."* ~5 lines change.

**Changes**:
- [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go) — IG-item regex update. ~15 LoC.
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — IG mandate. ~5 lines.

**Test coverage**:
- `TestValidateCodebaseIG_HashHashHashItems_Pass` — IG body with `### 1.`, `### 2.`, `### 3.` items → no violation.
- `TestValidateCodebaseIG_PlainOrderedList_Rejected` — IG body with `1.`, `2.`, `3.` items (no `###` prefix) → violation per item.
- `TestValidateCodebaseIG_MixedShape_RejectsPlainItems` — IG with `### 1.` first then `2.` second → second item flagged.
- `TestBrief_Scaffold_IGMandateHeadings` — brief body contains `### 2. <title>` example phrasing.

**Watch**: §M's engine-generated item #1 must use `### 1. Adding \`zerops.yaml\`` shape — verify the assemble-side template ([assemble.go:131-141](../../../internal/recipe/assemble.go#L131) area) emits `###` not plain ordered-list. If it currently emits ordered-list, fix in this workstream; otherwise the validator will reject the engine's own output.

### 2.Q — `git init` + first commit at scaffold close

**What run 10 showed** ([ANALYSIS.md §3 gap Q](../runs/10/ANALYSIS.md)): scaffold sub-agents wrote source + `zerops.yaml` to `/var/www/<h>dev/` (correct path post-§L), but never ran `git init` or committed anything. The archived [runs/10/apidev/](../runs/10/apidev/) etc. directories carry no `.git/` directory.

**Why this is a gap**: the publish path (currently manual via `zcp sync recipe export` then push to GitHub) needs a clean `git init` + first-commit-with-all-source as a precondition. Doing it post-hoc loses the per-feature commit history a porter cloning the repo would normally see.

**Root cause (named files)**:
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — scaffold-close steps don't mandate `git init`.
- [internal/recipe/content/briefs/feature/content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — feature-close doesn't mandate per-feature commit.
- [internal/sync/export.go](../../../internal/sync/export.go) — export tool doesn't warn on missing `.git/`.

**Fix direction** — two edits:

1. **Q-1 — scaffold brief mandates `git init` at scaffold close**. [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md). Add to scaffold-close checklist: *"Run `git init && git add -A && git commit -m 'scaffold: initial structure + zerops.yaml'` from `<cb.SourceRoot>` (= `/var/www/<hostname>dev/`). The apps-repo publish path needs a clean git history; doing this post-hoc loses per-feature commit-shape."* ~5 lines.

2. **Q-2 — feature brief mandates per-feature commits**. [internal/recipe/content/briefs/feature/content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md). Add: *"Commit each feature extension separately with a descriptive message (`feat: add CRUD endpoints + Postgres wiring`). The per-feature shape is what a porter sees when scrolling git history."* ~3 lines.

3. **Q-3 — `zcp sync recipe export` warns on missing `.git/`**. [internal/sync/export.go](../../../internal/sync/export.go). Before exporting per-codebase content, check `<SourceRoot>/.git/` exists; emit a warning if not (do not block — the export can still produce the tarball; the warning is informational). ~10 LoC.

**Changes**:
- [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — `git init` mandate. ~5 lines.
- [internal/recipe/content/briefs/feature/content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — per-feature commit. ~3 lines.
- [internal/sync/export.go](../../../internal/sync/export.go) — missing-`.git/` warn. ~10 LoC.

**Test coverage**:
- `TestBrief_Scaffold_ContainsGitInitMandate` — brief body contains `git init` + `git add -A` + `git commit` phrases.
- `TestBrief_Feature_ContainsPerFeatureCommitGuidance` — brief body mentions per-feature commits.
- `TestExport_NoGitDir_EmitsWarning` — fixture SourceRoot without `.git/`, export emits warning to stderr/log naming the codebase.
- `TestExport_WithGitDir_NoWarning` — fixture SourceRoot with `.git/`, export proceeds quietly.

**Watch**: scaffold sub-agents' Bash ability covers `git init`; no new tool surface needed. Verify the scaffold-phase Plan grants Bash permission scoped to `<SourceRoot>` — should already, since scaffold writes source there.

### 2.S — engine-composed `briefKind=finalize`

**What run 10 showed** ([ANALYSIS.md §3 gap S](../runs/10/ANALYSIS.md), [PROMPT_ANALYSIS.md §2.6](../runs/10/PROMPT_ANALYSIS.md)): run 10 introduced a finalize sub-agent dispatch ([agent-ad2a](../runs/10/SESSION_LOGS/subagents/agent-ad2acd2987b1f9ac6.jsonl), 226 events) at main-session L137. This is NEW and not described in [phase_entry/finalize.md](../../../internal/recipe/content/phase_entry/finalize.md) (still says "There is no writer sub-agent. Fragments are authored by whoever holds the densest context — main agent for platform-narrative content (authored here)").

**The hand-typed wrapper has visible defects** ([PROMPT_ANALYSIS.md §2.6](../runs/10/PROMPT_ANALYSIS.md)):
- **Math errors**: "Envs 0, 1 × 11 hostnames = 22 fragments each → 44 total" — `22 fragments each` is wrong, it's 11 each. "Total = 89 fragments" — actual is 67.
- **Obsolete paths**: wrapper says "Read/Glob/Grep against `/var/www/nestjs-showcase/`". Sub-agent then used `/var/www/{api,app,worker}/{README,CLAUDE}.md` (gap-M-regressed paths) by inference, not because the wrapper said so.
- **Cite-guide instruction**: "Cite guides by name in the prose ('Cite `rolling-deploys`.')" — directly produces the citation noise from gap O.
- **Wrapper-vs-engine duplication**: ~80% of the wrapper repeats Plan-derivable info (slug, framework, codebase hostnames, managed services). Same smell as run-9 S10.

The dispatch itself is reasonable cost-cutting (67+ fragments mechanical authoring). But the wrapper isn't engine-composed — it's hand-authored and the math errors prove the failure mode.

**Root cause (named files)**:
- [internal/recipe/briefs.go](../../../internal/recipe/briefs.go) — `BuildScaffoldBrief` exists; no `BuildFinalizeBrief` equivalent. The finalize-side wrapper composition is left to the main agent's prose discretion.
- [internal/recipe/content/phase_entry/finalize.md](../../../internal/recipe/content/phase_entry/finalize.md) — phase entry doesn't name the dispatch option as a supported path.

**Fix direction** — two edits:

1. **S-1 — engine-composed `briefKind=finalize`**. [internal/recipe/briefs.go](../../../internal/recipe/briefs.go). Add `BuildFinalizeBrief(plan, sess) (string, error)` (or equivalent action wired through `zerops_recipe action=build-brief briefKind=finalize slug=<slug>`). The brief composes from `Plan`:
   - **Correct codebase paths** (`<cb.SourceRoot>` per codebase, post-§L AND post-§M's hard-fail).
   - **Correct math** (count fragments needed by walking `Plan.Fragments` requirements + `Plan.ManagedServices` + `Plan.Codebases`).
   - **No citation-noise instruction** (per §2.O — citations are author-time signals, not render output).
   - **Validator-tripwires section** mirrored from scaffold brief (§2.R IG-shape, §2.O citation discipline, §2.V self-inflicted litmus, plus finalize-specific rules — meta-agent-voice false-positive on "AI Agent" tier label per [TIMELINE §6 finding 5](../runs/10/TIMELINE.md), `env/<N>/import-comments/<hostname>` accepted-only-for-managed-services per [TIMELINE §6 finding 2](../runs/10/TIMELINE.md)).
   - **Engine-derived audience-paths block**: stitched output at `<outputRoot>`, per-codebase published content at `<cb.SourceRoot>` for each codebase (correct hostnames, correct paths).
   ~150 LoC. The brief body lives in `internal/recipe/content/briefs/finalize/` as discrete atoms (mirror scaffold/feature structure).

2. **S-2 — document the finalize-sub-agent option**. [internal/recipe/content/phase_entry/finalize.md](../../../internal/recipe/content/phase_entry/finalize.md). Update prose: *"Finalize fragments may be authored directly by the main agent (low fragment count, single-shot) or via a finalize sub-agent dispatch (high fragment count, mechanical). Use `zerops_recipe action=build-brief briefKind=finalize slug=<slug>` to compose the dispatch wrapper from `Plan`. Hand-typed wrappers are out — math errors and path drift compound."* ~10 lines.

**Changes**:
- [internal/recipe/briefs.go](../../../internal/recipe/briefs.go) — `BuildFinalizeBrief` + handler wire-up. ~150 LoC.
- [internal/recipe/content/briefs/finalize/](../../../internal/recipe/content/briefs/finalize/) — new directory with discrete atom files (intro / validator-tripwires / authoring-checklist / paths). ~3 atom files.
- [internal/recipe/content/phase_entry/finalize.md](../../../internal/recipe/content/phase_entry/finalize.md) — dispatch option doc. ~10 lines.
- Handler registration in [internal/recipe/handlers.go](../../../internal/recipe/handlers.go) — wire `briefKind=finalize` through the existing `build-brief` handler. ~15 LoC.

**Test coverage**:
- `TestBuildFinalizeBrief_CorrectCodebasePaths` — Plan with `cb.SourceRoot = /var/www/apidev`, brief body contains `/var/www/apidev` AND does NOT contain `/var/www/nestjs-showcase/api/` or other obsolete forms.
- `TestBuildFinalizeBrief_CorrectFragmentMath` — Plan with N codebases × M envs, brief body's fragment-count math equals the actual `Plan.Fragments` cardinality.
- `TestBuildFinalizeBrief_NoCiteGuideInstruction` — brief body does NOT contain the literal `Cite \`x\`` or `cite by name in the prose` phrasing.
- `TestBuildFinalizeBrief_ValidatorTripwires` — brief body contains the IG-shape tripwire (R) + citation-noise tripwire (O) + self-inflicted-litmus tripwire (V).
- `TestBuildFinalizeBrief_UnderCap` — brief body under 12 KB (matches scaffold-brief cap convention).

**Watch**: this is the largest single workstream by LoC (~150). Defer to Tranche 3; tranches 1+2 don't depend on it. If Tranche 3 doesn't land in run 11, finalize stays sub-agent-dispatched-with-hand-wrapper and the math/path errors recur — no worse than run 10, and the wrapper now references the corrected SourceRoot paths post-§M because the main agent has Plan visibility.

---

## 3. Ordering + commits

Dependencies:
- **U-1 + U-2** are foundational — without them, V's classifier improvements run against records that never arrive (U-1) or against records missing the `failureMode`/`fixApplied` fields V-1 reads (U-2). Land first.
- **V** depends on U-2's enriched schema.
- **M** is structurally independent — can land in parallel with U/V (different files, different test surfaces).
- **N, O, P, R** are independent of each other; all depend on Tranche 1 landing first (so the routing + content-discipline lattice is stable before adding more validators).
- **Q, S** are independent polish; can land alongside Tranche 2 or after.

### Commit order

Tranche 1 — foundation (must land first):

1. **fix(tools): refuse v2 `zerops_record_fact` during v3 recipe session with redirect message** (U-1) — `internal/tools/record_fact.go` `resolveFactLogPath` + description prefix + `TestRecordFact_RefusesDuringV3Session` + `TestRecordFact_AcceptsWithoutV3Session`.
2. **feat(recipe): enrich v3 `FactRecord` schema with `failureMode`/`fixApplied`/`evidence`/`scope`** (U-2) — `internal/recipe/facts.go` schema + `TestFactRecord_AcceptsEnrichedFields` + `TestFactRecord_OptionalEnrichedFields`. **Must land before commit 3.**
3. **feat(recipe): classifier auto-detects self-inflicted from `fixApplied`+`failureMode` shape; emits warning** (V-1) — `internal/recipe/classify.go` + tests. Reads U-2's schema fields.
4. **feat(recipe): `kb-bullet-paraphrases-cited-guide` validator** (V-2) — `internal/recipe/validators_codebase.go` + tests.
5. **feat(recipe): `kb-bullet-no-platform-mention` validator** (V-3) — same file + tests.
6. **feat(recipe): `kb-bullet-self-inflicted-shape` regex validator** (V-4) — same file + tests.
7. **docs(recipe): scaffold + feature briefs add "Self-inflicted litmus" subsection with run-10 anti-patterns** (V-5) — `content_authoring.md` + `content_extension.md` + `TestBrief_Scaffold_ContainsSelfInflictedLitmus`.
8. **fix(recipe): stitch hard-fails on non-abs / non-`dev`-suffixed `SourceRoot`** (M-1) — `handlers.go` + `TestStitch_NonAbsSourceRoot_HardFails` + `TestStitch_NonDevSuffixedSourceRoot_HardFails`.
9. **fix(recipe): `readCodebaseYAMLForHost` hard-fails on missing yaml when SourceRoot non-empty** (M-2) — `assemble.go` + tests + the soft-fail-tolerance carve-out for genuinely pre-scaffold paths.
10. **fix(sync): `zcp sync recipe export` reads README/CLAUDE from `<SourceRoot>/`** (M-3) — `internal/sync/export.go` + test pin.

Tranche 2 — content discipline (after Tranche 1):

11. **fix(recipe): tighten `codebaseKnown` to reject slot hostnames + actionable error message** (N-1) — `assemble.go` + `handlers_fragments.go` + `TestRecordFragment_RejectsSlotHostname`.
12. **docs(recipe): scaffold-brief tripwire — fragment id uses `cb.Hostname`, not slot hostname** (N-2) — `content_authoring.md`.
13. **docs(recipe): citations live in prose, not as boilerplate** (O-1) — `content_authoring.md` rewrite.
14. **feat(recipe): `kb-cited-guide-boilerplate` + `env-yaml-cite-meta` validators** (O-2) — `validators_codebase.go` + tests.
15. **docs(knowledge): rewrite `internal/knowledge/themes/core.md:252` `.deployignore` paragraph** (P-1) — knowledge atom + `TestKnowledge_CoreThemes_DeployignoreParagraph_NoMirrorGitignore`.
16. **docs(recipe): scaffold-brief tripwire forbids `.deployignore` author-time** (P-2) — `content_authoring.md`.
17. **fix(tools): `zerops_deploy` warns on `.deployignore` containing common-trap patterns; rejects `dist`/`node_modules`** (P-3) — deploy handler + tests.
18. **fix(recipe): IG validator + scaffold-brief unify on `### N.` header shape** (R-1 + R-2) — `validators_codebase.go` + `content_authoring.md` + tests.

Tranche 3 — polish (parallelizable with Tranche 2):

19. **docs(recipe): scaffold brief mandates `git init` + first commit at scaffold close; feature brief mandates per-feature commits** (Q-1 + Q-2) — `content_authoring.md` + `content_extension.md`.
20. **fix(sync): `zcp sync recipe export` warns on missing `<SourceRoot>/.git/`** (Q-3) — `internal/sync/export.go` + test.
21. **feat(recipe): `zerops_recipe action=build-brief briefKind=finalize` engine-composed wrapper** (S-1) — `briefs.go` + new `content/briefs/finalize/` atoms + handler wire-up + tests.
22. **docs(recipe): `phase_entry/finalize.md` documents the finalize-sub-agent dispatch option** (S-2).

Final milestone commit: **docs(recipe): run-11-readiness CHANGELOG entry** — update [CHANGELOG.md](../CHANGELOG.md) with the story, grouping foundation fixes (U, V, M) separately from content discipline (N, O, P, R) and polish (Q, S) so the narrative matches the tranche structure.

Between every commit: `go test ./... -count=1 -short` green + `make lint-local` green. CLAUDE.md's "Max 350 lines per .go file" still applies — `validators_codebase.go` and `classify.go` are the biggest growth surfaces in run 11; if either approaches 350 lines, split by classification class (Self-inflicted / Framework-quirk / Library-metadata each get a `validators_codebase_<class>.go`).

---

## 4. Acceptance criteria for run 11 green

Run 11 is "reference parity + spec-enforced classification" when, against a fresh `nestjs-showcase` (or fresh slug) dogfood:

### Inherited from run 10 (continue to hold — criteria 1–26)

1. Stage deploys green on every codebase.
2. Browser verification FactRecords recorded per feature tab.
3. Seed ran once; `GET /items` returns ≥ 3 items.
4. Stitched output has canonical structure — root `README.md` + 6 tier folders, each with `README.md` + `import.yaml`. Per-codebase files live at `<cb.SourceRoot>/{README.md, CLAUDE.md, zerops.yaml}`.
5. Factuality lint passes.
6. Fragments authored in-phase.
7. Citation map attachment on KB gotchas (note: §O changes the *form* — in-prose, not boilerplate — but citations still attach where the citation map mandates).
8. Cross-surface uniqueness.
9. Finalize gates all pass on the full deliverable.
10. Recipe click-deployable end-to-end.
11–16. (Tier 11–16 from run-9-readiness — scaffold yamls dev-vs-prod, no dividers, porter voice, parallel dispatch, record-fragment response echoes, feature facts in `facts.jsonl`.) All continue to pass.
17. Apps-repo shape at `<SourceRoot>/` matches reference.
18. `<outputRoot>/codebases/` does not exist.
19. README Integration Guide item #1 is a fenced yaml code block.
20. YAML inline comments multi-line natural prose.
21. README knowledge-base uses one consistent format (`**Topic**` + em-dash + prose; no `**symptom**:` triples).
22. Per-codebase CLAUDE.md is ≤ 60 lines.
23. Engine brief omits HTTP section for non-HTTP roles.
24. Engine brief uses `# Behavioral gate` header.
25. Author-time "Validator tripwires" section appears in scaffold brief.
26. Feature sub-agent makes at most 1 `zerops_knowledge` call for `execOnce` key shape.

### New for run 11

27. **Zero v2 `zerops_record_fact` calls during a v3 recipe session.** Sub-agent attempts to call v2 receive a refusal-with-redirect; subsequent retry uses v3. (gap U)
28. **All `record-fact` calls during scaffold + feature land in `facts.jsonl`; `legacy-facts.jsonl` is empty or absent under `<outputRoot>/`.** Verified by listing `<outputRoot>/` post-run + grepping its contents. (gap U)
29. **Codebase KB carries zero bullets that fail the spec's framework-quirk / self-inflicted / scaffold-decision tests.** Engine emits warnings for any auto-reclassified records via V-1; final stitch passes V-2 + V-3 + V-4 with zero violations on the published KB fragments. (gap V)
30. **KB bullets contain zero `Cited guide:` boilerplate suffix.** Citations integrate into prose where used. Env `import.yaml` comments contain zero `(cite \`x\`)` meta-talk. Verified by `kb-cited-guide-boilerplate` validator on stitched output. (gap O + V-overlap)
31. **Per-codebase README/CLAUDE land at `<cb.SourceRoot>/`** = `/var/www/<hostname>dev/`. Stitch refuses any non-abs or non-`dev`-suffixed `SourceRoot` with an error naming the codebase. No file ever lands at `/var/www/<hostname>/` (no `dev`). (gap M)
32. **README IG item #1 IS the engine-generated yaml block** — verified by string presence of `### 1. Adding \`zerops.yaml\`` followed by a fenced code block whose body matches `<SourceRoot>/zerops.yaml` byte-for-byte. (gap M, prerequisite for any IG-shape claim)
33. **No `.deployignore` author-time writes during scaffold** for the canonical case (no recipe-specific need). Worker scaffold's run-10 `.deployignore` pattern (with `dist`) does not recur. The knowledge atom no longer recommends mirroring `.gitignore`. (gap P)
34. **Each codebase `<SourceRoot>/` has `.git/` initialized + at least one commit.** `zcp sync recipe export` runs without warning on missing `.git/`. (gap Q)
35. **`record-fragment` rejects slot-hostname codebase ids** with an error naming the Plan codebase list AND the slot-vs-codebase distinction. Sub-agent attempting `codebase/appdev/intro` retries with `codebase/app/intro` on first try (no cleanup-sub-agent dispatch). (gap N)
36. **Codebase IG fragments use `### N. <title>` headers** consistently — validator accepts headings, rejects plain ordered-list. Brief mandates the heading shape. Finalize round 1 surfaces zero IG-shape violations. (gap R)
37. **Finalize brief is engine-composed via `zerops_recipe action=build-brief briefKind=finalize`.** No hand-typed wrapper with math errors or obsolete paths. The composed brief's codebase paths reflect post-§M `<cb.SourceRoot>` correctness; fragment-count math reflects the live Plan. (gap S — Tranche 3, recommended for run 11; if deferred, criterion 37 carries to run 12 and the main-agent-direct authoring path stays viable)

---

## 5. Non-goals for run 11

Keep out of scope, ship separately or deferred:

- **Refining the spec itself.** The spec at [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) is authoritative; run 11 enforces what's already there, doesn't redefine the taxonomy. If a class boundary feels wrong during implementation, that's a separate spec PR.
- **Chain-resolution redesign.** [chain.go:73](../../../internal/recipe/chain.go#L73) `loadParent` still looks for `<parentDir>/codebases/<h>/`; after L (run 10) shipped, no v3-produced recipe has that directory either. The chain resolver remains a no-op. Redesign deferred until `nestjs-minimal` gets a v3 re-run.
- **Automated click-deploy verification harness.** Criterion 10 stays manual; becomes testable post-§M but the harness is a separate workstream.
- **`verify-subagent-dispatch` SHA check.** Still deferred from run-8-readiness.
- **Per-surface `validate-surface` action.** Useful authoring affordance (collapses finalize "wall of red"); not blocking.
- **Auto-inject scaffold-phase facts into feature brief.** Hand-assembled by main agent in run 9–10; automatable but not blocking.
- **Validator message quality improvements** beyond what V/N/O/P/R/Q/S already touch. `factuality-mismatch` should name the offending substring; `env-readme-too-short` should say "add one paragraph". Mechanical; defer.
- **Source-log credential redaction.** Platform concern, not v3.
- **`build-subagent-prompt` action for scaffold + feature.** §S delivers it for finalize only; scaffold + feature wrapper composition stays in the main agent's prose discretion. Run 11+ optimization.
- **Forensic re-litigation of the run-10 SourceRoot regression's exact cause.** [ANALYSIS.md §3 gap M](../runs/10/ANALYSIS.md) names three plausible causes; M-1 + M-2 force loud failure regardless of which was real. The user has the binary state to confirm post-implementation.
- **Engine-side fragment-id rejection telemetry.** Useful audit affordance; not run-11 blocking.
- **Harness eager-load of `zerops_*` tool schemas in Agent sub-agents.** Out of v3 scope ([PROMPT_ANALYSIS.md §3 S1](../runs/10/PROMPT_ANALYSIS.md)).
- **`meta-agent-voice` whitelist for "AI Agent" tier label.** Operational; one-line fix; bundle in §S's tripwires-list rather than its own commit.
- **Double-`#` artifact in env import.yaml comments** ([ANALYSIS.md §4](../runs/10/ANALYSIS.md)). Cosmetic; defer.

---

## 6. Risks + watches

- **U-1 changes a tool's behavior visible to any v2-only caller.** v2 `zerops_record_fact` outside a v3 recipe session keeps working unchanged (verified by `TestRecordFact_AcceptsWithoutV3Session`). The only behavioral change is during an active v3 session, where the tool now refuses with a redirect. No persistent state change; safe to roll back.
- **V-1's regex on `fixApplied` may overfit run-10's specific shape.** "removed dist from .deployignore" / "switched X to Y" are clear cases; novel fix shapes ("rebuilt the workspace from scratch") may slip through or false-positive. Mitigation: the warning is informational only — the fact still records, it just gets reclassified to ClassSelfInflicted (which routes to discard). If a false-positive emerges, the agent can re-record with explicit `surfaceHint: "platform-trap"` plus a justification field; engine-side override only when shape is unambiguous.
- **V-2's Jaccard threshold (0.5) is empirical.** Tune against run-10's published KB: bullets api #2 / #3 / #4 / #6 / #7 / #8 should fail at 0.5 (paraphrase); bullet #9 should pass (genuinely new content beyond cited guide). If threshold needs adjustment, change in one place; don't redesign the validator.
- **V-3's platform-vocabulary list is a hand-curated phrase list.** New platform features added to Zerops may need additions; the failure mode is over-strict (a legitimately platform-side bullet flagged as framework-quirk for using novel vocabulary). Mitigation: the list lives in a single constant in `validators_codebase.go`; update as platform vocab evolves.
- **M-2's tightening could surface latent test fixtures that relied on soft-fail.** Run `go test ./internal/recipe/... -count=1` after the change; expect 1–3 fixtures needing SourceRoot prefab. Cheap to fix; don't roll back.
- **P-3's hard-reject on `dist`/`node_modules`** could surface a recipe with a legitimate reason to exclude one (monorepo, sibling package output). Not anticipated for `nestjs-showcase`; if surfaced later, add `# zerops-deploy-acknowledged: dist` opt-out comment.
- **R-1's strict `### N.` requirement** breaks any historical fragment using plain ordered-list. Search [internal/recipe/content/](../../../internal/recipe/content/) and live recipe sessions for the pattern during implementation; update if found. The engine's own item-#1 template (post-§M) must use `###` form — verify.
- **S-1's brief-cap pressure.** ~150 LoC of new finalize-brief atoms could push the engine total over the 12 KB cap. Pin `TestBuildFinalizeBrief_UnderCap` at 12 KB. If pressure surfaces, compress duplicate platform-principles content shared with scaffold brief.
- **Run 11 target slug.** Run 10 ran against `nestjs-showcase` (project `ohV2YD1KTym24YEtobAyAg`). Workspace state may persist. Either delete the workspace before run 11 (cleanest) or run against a fresh slug (e.g. `nestjs-showcase-run11`) or tear down first. Document the pre-run requirement.

**Informational only — KB sizes will shrink after V lands** ([ANALYSIS.md §3 gap V "Watch"](../runs/10/ANALYSIS.md)). Applying V's rules to run 10 today would have cut api KB from 9 bullets to 4–5, worker KB from 6 to 2. **That's the spec-aligned outcome, not a regression.** Reference Laravel KB has 7 bullets but every bullet is hard-won; per-bullet truth-density matters, not bullet count. §4 carries no minimum-bullet criterion — that's the kind of presence-check that pressured run 9–10 into padding.

---

## 7. Open questions

1. **U-3 description-prefix wording — "DEPRECATED for v3 recipe sessions" vs more neutral framing?** "DEPRECATED" reads as if v2 is going away; the truth is more nuanced — v2 stays for v2 callers, just refuses during v3 sessions. Alt: `"v2 fact tool — use zerops_recipe action=record-fact for v3 recipe sessions; this tool is for the legacy bootstrap/develop workflow only."` Both work; lean toward the more-neutral framing since it tells the agent WHEN each is right.

2. **V-1's auto-reclassification — silent override, log-only warning, or hard reject?** Three behaviors:
   - (a) Silent override: classifier returns ClassSelfInflicted, the fact still records but never reaches KB. Agent never sees the override.
   - (b) Log-only warning: classifier overrides + emits a warning to the recipe session log; agent can read the log but isn't blocked.
   - (c) Hard reject: classifier refuses the record-fact call with a message naming the litmus; agent must re-record with corrected shape or explicit acknowledgement.
   
   Lean (b) — actionable without blocking. (c) is too aggressive for an empirical heuristic; (a) gives the agent no chance to course-correct. Decide at implementation.

3. **V-2 — Jaccard against full guide body or guide summary?** Some `zerops_knowledge` guides are long (>5KB). Full-body Jaccard is noisy; summary-only Jaccard misses paraphrase of body content. Lean: tokenize both, drop stop-words, use top-100 keyword overlap. Tunable; pin in code constants.

4. **V-3 platform-vocabulary list — central constant or runtime-augmentable?** Current Plan exposes `Plan.Codebases[*].Hostname` + `Plan.ManagedServices[*].Hostname` — those should be added at validator-runtime so a recipe-specific service like `meilisearch1` counts as a platform mention. Lean: hardcoded base list + Plan-derived runtime extension.

5. **M-2's hard-fail — propagate to stitch (a) or warn-and-tolerate at early-phase paths (b)?** Run 10's silent failure happened at finalize stitch where the yaml MUST exist. Earlier phases (research, provision) may render templates before the codebase yaml is authored. Lean: hard-fail at stitch-time (caller `assembleCodebaseREADME` invoked from `stitchContent`); soft-fail with logged warning at any other entry point. Test both paths.

6. **R-1 — strict `### N.` only, or accept BOTH `### N.` AND plain ordered-list with deprecation message?** Strict is cleaner but breaks any historical fragment. Lean strict — there are no published v3 recipes to break, and the brief is the authoritative shape. If the engine's own item-#1 (per §M) emits `###`, the validator must require `###` to keep the IG visually uniform.

7. **S-1's tripwires section — duplicate scaffold's, or cross-reference?** Cross-reference is cleaner ("see scaffold brief §Validator tripwires") but sub-agents read prompts in isolation. Lean: duplicate the relevant rules verbatim in finalize brief — drift risk is low because both compose from the same `briefs.go` constants.

8. **Q-3 warn-on-missing-`.git/` — at export time only, or also at finalize close?** Earlier signal is better (agent can `git init` before close). Lean: warn at finalize close (or as part of `complete-phase finalize` validator surface) AND at export time. Two surfaces, one rule.

9. **Run 11 target slug — re-use `nestjs-showcase` (most directly comparable to run 10) or fresh slug?** Re-use makes the run-11-vs-run-10 diff tractable (same framework, same managed services, same KB topics). Fresh slug exercises the spec on a new substrate. Lean: `nestjs-showcase` again, after explicit workspace teardown — the comparability dominates.
