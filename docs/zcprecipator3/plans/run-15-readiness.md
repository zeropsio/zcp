# Run 15 readiness — implementation plan

Run 14 (`nestjs-showcase`, 2026-04-27) closed Cluster A.1's
in-memory validator plumbing cleanly. Tier surfaces lifted from run-13's
9-line collapse to 41-42 lines; tier yaml comments lifted from absent
to 75-83 indented lines per tier. §B2 dispatch composer saturated at
~0% wrapper share. Six SPA panels with stable selectors. Zero
defensive feature re-dispatch.

Run-14 self-graded **8.5/10 vs reference**. A post-run walk against
[docs/spec-content-surfaces.md](../../spec-content-surfaces.md) with
two reference recipes in hand
([laravel-jetstream](../../../../../recipes/laravel-jetstream/),
[laravel-showcase](../../../../../recipes/laravel-showcase/)) re-scored
to **6.5-7.0/10 honest**. The defect class is **surface-purpose
mismatch**: tier README extract markers wrap 35-line ladders where
both references put 1-2 sentences; KB sections carry 11-12 bullets
where references settle at 5-8; IG items describe recipe-internal
scaffold (server.js, /healthz, sirv config) instead of teaching
platform-portable principles. Brief preface teaches surfaces; agent
drifts at per-fragment authoring decisions because the spec contracts
roll out of working memory by then.

Run-14 also shipped two engine extensions that pass unit tests but
miss in production: R-14-1 (Cluster A.2 subdomain auto-enable; the
`client.GetService` fallback races L7 port-registration propagation)
and R-14-P-1 (Cluster B.3 reachable-slug list; CHANGELOG promised it
but the production brief carries zero matches). Both are stealth
regressions of run-14 readiness work — code shipped, production
surface unverified.

Run-15's bar is content-quality plateau. Five clusters; total
~200 LoC engine + ~140 lines spec/brief content + 3 new validators
+ 2 e2e production-surface tests:

- **A** — R-14-1 closure: defer subdomain eligibility to `ops.Subdomain` (plan-declared intent, not platform GetService).
- **B** — R-14-P-1 closure: reachable-slug list reaches dispatched brief; e2e test pins production surface.
- **F** — Content surface routing (5 sub-workstreams). The bulk of run-15.
- **D** — Operational preempts continuation (R-14-5 viewport; optional R-14-4 §B3 yaml-emit corrective patterns).
- **E** — Content-discipline tighteners (R-14-2/3, finalize brief density).

The proposal stays uniformly TEACH-side per
[system.md §4](../system.md): every workstream expresses positive
shape (line caps, char caps, classification compatibility table,
structural validators) — never a vocabulary ban.

**The Cluster F.1 spec edit landed before this plan.** See
[docs/spec-content-surfaces.md](../../spec-content-surfaces.md) — the
per-surface line-budget table, the rewritten §Surface 2 contract
(intro extract = 1-2 sentences ≤ 350 chars), the run-14 counter-examples,
the friendly-authority voice section, and the classification × surface
compatibility table. Cluster F.2-F.5 anchor to that spec; if it
disagrees with this plan, the spec wins.

References:
- [run-14/ANALYSIS.md](../runs/14/ANALYSIS.md) — run-14 forensic, R-14-1..R-14-5.
- [run-14/CONTENT_COMPARISON.md](../runs/14/CONTENT_COMPARISON.md) — surface-by-surface vs reference; honest re-score 6.5-7.0/10.
- [run-14/PROMPT_ANALYSIS.md](../runs/14/PROMPT_ANALYSIS.md) — R-14-P-1..P-4.
- [run-15-prep.md](run-15-prep.md) — handoff doc (superseded by this plan).
- [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) — the spec; F.1 deliverable.
- [system.md](../system.md) §1, §4.
- [docs/spec-workflows.md §4.8](../../spec-workflows.md) — subdomain L7 activation.

---

## 0. What changed since run 14

Three things. Each shapes one or more clusters.

**Cluster A.1 took.** Validators consume in-memory bodies derived from
`Plan.Fragments` + assembler outputs; per-codebase scoped close ≡
matching slice of full-phase close by construction. The
stitch-vs-read-coherence pattern that drove run-13's 21 defects is
structurally closed for the SSHFS surface.

**Cluster A.2 + B.3 shipped, but stealth-regressed in production.**
Both ship engine code, both have unit tests that pass, both miss the
production surface ([§0.4 of run-14](../runs/14/ANALYSIS.md#64-the-stealth-regression-class)).
A.2's `client.GetService` races L7 port-registration propagation;
B.3's `BuildScaffoldBriefWithResolver` doesn't reach the production
dispatch path. The structural lesson: **unit tests that exercise the
code path are not the same as e2e tests that observe the production
output.** Cluster A and B fix the symptoms; Run-15's preflight
([§9](#9-pre-flight-verification-checklist)) encodes the e2e-observation
discipline as a checklist item to prevent recurrence.

**The reference walk produced a content contract.** Both
laravel-jetstream (human-authored) and laravel-showcase (early-flow)
agree on per-surface line budgets within ±20%. Run-14 violated the
budgets by 2-3× on three surfaces. The
[spec edit](../../spec-content-surfaces.md) ships the budgets as hard
caps in the per-surface contracts; Cluster F operationalizes them at
the engine boundary (record-time delivery, classification refusal,
structural validators).

### Workstream legend

| Cluster | Theme | Tranche | Engine LoC | Content lines | New validators / tests |
|---|---|---|---|---|---|
| **A** | R-14-1 — subdomain eligibility deferred to ops.Subdomain | T1 | ~25 | 0 | 3 unit tests |
| **B** | R-14-P-1 — reachable-slug list reaches dispatched brief; audit-driven | T1 | ~10 | 0 | 1 e2e test |
| **F** | Content surface routing (5 sub-workstreams) | T2 | ~110 | ~80 (spec already shipped) + ~60 brief | 3 new validators + 2 unit tests |
| **D** | R-14-5 viewport (engine fix); R-14-4 §B3 stretch | T3 | ~10 (50 stretch) | 0-5 | 1 unit test |
| **E** | R-14-2 phase-advance, R-14-3 audience-rule sweep, R-14-P-4 finalize density | T3 | ~15 | ~25 | extends existing |

T1 closes the two run-14 stealth regressions; T2 is Cluster F (the bulk);
T3 is operational + content discipline; T4 is sign-off.

---

## 1. Goals for run 15

1. **Stage subdomains materialize on FIRST cross-deploy** without
   manual `zerops_subdomain action=enable`. Zero manual enable calls
   in any sub-agent. (Cluster A)

2. **Dispatched scaffold briefs carry
   `## Recipe-knowledge slugs you may consult`** verbatim. End-to-end
   test pins the production surface. (Cluster B)

3. **Tier README intro extract markers wrap 1-2 sentences (≤ 350 chars).**
   Both reference recipes settle at this; run-14 shipped 35-line ladders.
   ([spec §Surface 2](../../spec-content-surfaces.md#surface-2--environment-readme)) (Cluster F.4)

4. **KB sections cap at 5-8 bullets per codebase; IG sections cap at
   4-5 items per codebase including engine-emitted IG #1.** Validators
   block above the caps. (Cluster F.5)

5. **No fabricated yaml field names in env import.yaml comments.**
   Structural validator parses yaml AST + comment tokens; refuses
   comment-named field paths absent from the yaml. (Cluster F.5)

6. **`record-fragment` response carries the per-fragment-id surface
   contract.** Agent reads spec §Surface-N test verbatim at authoring
   decision time, not just at brief-preface time. (Cluster F.2)

7. **`record-fragment` accepts `classification`; engine refuses
   incompatible (classification × fragmentId) pairs** per
   [spec §Classification × surface compatibility](../../spec-content-surfaces.md#classification--surface-compatibility).
   (Cluster F.3)

8. **Apps-repo IG/KB/yaml-comments zero `zerops_*` tool name leaks.**
   R-14-3 sweep extension. (Cluster E.2)

9. **Phase-advance asymmetry teaching reaches `phase_entry/research.md`
   + `phase_entry/provision.md`.** R-14-2 closure. (Cluster E.1)

10. **Browser-walk verification doesn't burn time on viewport-clipped
    clicks.** `zerops_browser` Click handler scrolls into view OR D.4
    atom names the constraint. (Cluster D.1)

11. **Honest content grade ≥ 8.5/10 vs reference.** Cluster F's
    record-time contract delivery converges auto-grade and honest
    grade.

---

## 2. Workstreams

### 2.0 Guiding principles

1. **Cluster A and B are gates.** If T1 fails, abort dogfood before T2.
2. **Spec ([F.1](../../spec-content-surfaces.md)) shipped before this plan.** F.2-F.5 anchor to it; if F implementation disagrees with the spec, the spec wins.
3. **TEACH side stays positive** per [system.md §4](../system.md). Every workstream's classification is stated; no vocabulary bans land in this run.
4. **Preflight observes production surfaces** per [§0 / §9](#9-pre-flight-verification-checklist). Every brief / record-fragment-response / validator extension has an e2e test that observes the dispatched output.

### 2.A — R-14-1 closure: defer subdomain eligibility to ops.Subdomain

**Gap.** [`R-14-1`](../runs/14/ANALYSIS.md#r-14-1) — Cluster A.2's
fallback via `client.GetService` races L7 port-registration
propagation; FIRST cross-deploy of every stage slot returns
`HTTPSupport=false`. Three manual `zerops_subdomain action=enable`
calls in run-14 scaffold-app.

**Classification.** TEACH (engine resolves runtime state by
construction, no platform-state read in the eligibility decision).

**Fix shape.**
[`internal/tools/deploy_subdomain.go::maybeAutoEnableSubdomain`](../../../internal/tools/deploy_subdomain.go)
gates auto-enable on the plan's `enableSubdomainAccess: true` field
(read from in-memory plan, not platform state). When the plan declares
intent, dispatch `ops.Subdomain` unconditionally; the existing
check-before-mutate inside `ops.Subdomain` handles idempotency. Add
bounded backoff retry inside `ops.Subdomain.enable` for the platform's
"service not yet HTTP-supporting" response so the propagation window
is absorbed at the right layer.

**Files.** `internal/tools/deploy_subdomain.go`, `internal/ops/subdomain.go`, plus tests.

**Acceptance.**
- Zero manual `zerops_subdomain action=enable` calls in scaffold-api/app/worker session jsonls.
- Run-15 deploy result for every stage slot first deploy carries `SubdomainAccessEnabled: true`.
- Three unit tests (plan-declared-intent dispatches; plan-not-declared skips; ops.Subdomain retry on service-not-yet-HTTP).

**Cost.** ~25 LoC + 3 unit tests.

### 2.B — R-14-P-1 closure: reachable-slug list reaches dispatched brief

**Gap.** [`R-14-P-1`](../runs/14/PROMPT_ANALYSIS.md#r-14-p-1) — the
run-14 CHANGELOG promised
`BuildScaffoldBriefWithResolver` emits a
`## Recipe-knowledge slugs you may consult` section when resolver is
present. Production briefs (all four scaffold + feature + finalize)
carry zero matches for the section header. Unit test passes; production
wire-up unverified.

**Classification.** TEACH (engine emits known truth from the recipes mount).

**Fix shape.** Audit-driven; three viable gap shapes:
- `Session.MountRoot` propagation broken (composer receives empty resolver).
- Production composer call uses non-`-WithResolver` entry point.
- Resolver-presence check incorrectly skips when MountRoot is set.

The e2e test below fails on run-14's current code regardless of the
gap shape; iterate on the production path until it passes. Commit
message names the actual gap.

**Files.** Likely
[`internal/recipe/briefs.go`](../../../internal/recipe/briefs.go) or
[`internal/recipe/briefs_subagent_prompt.go`](../../../internal/recipe/briefs_subagent_prompt.go);
`Store` / `Session` instantiation path.

**Acceptance.**
- New e2e test
  `TestScaffoldBrief_DispatchedToProductionAgent_CarriesReachableSlugList`
  in `integration/recipe_brief_test.go` simulates the production path
  (Store + OpenOrCreate + Plan + dispatch composer) and asserts
  `## Recipe-knowledge slugs you may consult` + at least one slug
  bullet appears in the dispatched bytes.
- Run-15 live dogfood: dispatched scaffold briefs carry the section.

**Cost.** ~10 LoC engine + 1 e2e test. **Establishes the e2e
production-surface precedent for §0.**

### 2.F — Content surface routing (5 sub-workstreams)

[F.1 already landed](../../spec-content-surfaces.md). F.2-F.5 are
engine + brief work that operationalizes the spec.

#### F.2 — Surface contract delivered at record-time

**Gap.** Brief preface teaches surfaces once at boot; agent drifts at
per-fragment authoring decisions
([spec §Why this exists](../../spec-content-surfaces.md#why-this-exists--the-content-quality-failure-mode)).

**Classification.** TEACH (positive shape: engine emits the contract; agent reads + authors against it).

**Fix shape.**
1. Extend
   [`internal/recipe/surfaces.go::SurfaceContract`](../../../internal/recipe/surfaces.go)
   with fields: `Reader`, `Test`, `LineCap`, `ItemCap`, `IntroExtractCharCap`. Populate per-surface from
   [spec line-budget table](../../spec-content-surfaces.md#per-surface-line-budget-table).
2. New exported function `SurfaceFromFragmentID(fragmentID string) Surface` mapping the fragment-id schema in [`handlers.go:118`](../../../internal/recipe/handlers.go#L118) to the surface enum.
3. Extend
   [`internal/recipe/handlers.go::RecipeResult`](../../../internal/recipe/handlers.go)
   with `SurfaceContract *SurfaceContract` field; populate in the
   `record-fragment` branch from `ContractFor(SurfaceFromFragmentID(in.FragmentID))`.
4. Brief teaching update in
   [`content/briefs/scaffold/content_authoring.md`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md)
   pointing at the response payload + the spec reader/test fields.

**Acceptance.**
- `TestSurfaceFromFragmentID` table-tests every fragment-id shape from the schema.
- `TestSurfaceContract_HasCaps` verifies every writer-authored surface has at least one structural cap populated.
- `TestRecordFragment_ResponseCarriesSurfaceContract` verifies the response payload carries the contract for the resolved surface.
- Brief atom changes reach dispatch (covered by §B's e2e precedent).

**Cost.** ~30 LoC engine + ~50 LoC populating contract values + ~25 lines brief content + 3 unit tests.

#### F.3 — Classification × surface compatibility refusal

**Gap.** Agent classifies facts at record-time today via `surfaceHint`
([`classify.go::Classify`](../../../internal/recipe/classify.go)) but
the engine doesn't refuse incompatible (classification × fragmentId)
pairs at `record-fragment` time. The compatibility table from
[spec §Classification × surface compatibility](../../spec-content-surfaces.md#classification--surface-compatibility)
isn't operationalized.

**Classification.** TEACH (positive compatibility table; structural refusal at engine).

**Fix shape.**
1. Extend
   [`RecipeInput`](../../../internal/recipe/handlers.go) with optional
   `Classification Classification` field (back-compat: empty value
   accepts).
2. New function
   `classify.classificationCompatibleWithSurface(c Classification, s Surface) error`
   reading from the spec compatibility table.
3. In
   [`handlers.go::handleRecipe`](../../../internal/recipe/handlers.go)
   action=record-fragment branch: if classification provided, call the
   compatibility check; on incompatible pair, return refusal with the
   spec-defined redirect teaching message.
4. Brief teaching update teaching the classification field + the
   compatibility table verbatim.

**Acceptance.**
- `TestRecordFragment_RefusesIncompatibleClassification` table-test covers every compatibility-table entry.
- Brief reaches dispatch.

**Cost.** ~30 LoC engine + ~15 lines brief.

#### F.4 — Tier extract char-cap split

**Gap.** Run-14 tier README intro markers wrap 35-line ladder content;
[spec §Surface 2](../../spec-content-surfaces.md#surface-2--environment-readme)
contract is 1-2 sentences ≤ 350 chars. The existing `env-readme-too-short`
validator was on the wrong target (body line count); both reference
recipes leave the body empty.

**Classification.** TEACH (structural cap on extract content).

**Fix shape.**
1. New validator `tier-readme-extract-too-long` registered to
   `SurfaceEnvREADME` in
   [`validators_root_env.go`](../../../internal/recipe/validators_root_env.go).
   Extracts content between `<!-- #ZEROPS_EXTRACT_START:intro# -->`
   markers; refuses content > `IntroExtractCharCap` (350 from spec).
2. Delete the `env-readme-too-short` validator. Reference recipes
   leave body empty; the validator drove run-14's ladder padding.
3. Update
   [`content/templates/env_readme.md.tmpl`](../../../internal/recipe/content/templates/env_readme.md.tmpl)
   to pre-render the extract markers around a 1-2-sentence slot per
   spec; body content optional after closing marker.
4. Update
   [`content/briefs/finalize/validator_tripwires.md`](../../../internal/recipe/content/briefs/finalize/validator_tripwires.md)
   to teach the extract char cap; remove the old "Env READMEs target
   45+ lines" teaching (R-14-P-4 closure).

**Acceptance.**
- `TestEnvREADME_ExtractCharCap` covers 1-sentence (pass), 2-sentence (pass), 35-line ladder (block).
- Run-15 tier README intro extracts wrap 1-2 sentences across all six tiers.

**Cost.** ~30 LoC engine (validator + extract-between-markers helper) + ~10 lines brief + ~5 lines template.

#### F.5 — IG/KB caps + fabricated-field-name validator

**Gap.**
- IG sections in run-14 ship 8-10 items per codebase; spec cap is 4-5.
- KB sections ship 11-12 bullets per codebase; spec cap is 5-8.
- Tier import.yaml comments name fabricated `project_env_vars` (snake_case) when yaml uses `project.envVariables`.
- Audience-voice leaks (`recipe author`, `during scaffold`) in tier import.yaml comments — `validators_source_comments.go` patrols apps-repo zerops.yaml but not env import.yaml.

**Classification.**
- IG/KB cap validators: TEACH (positive structural caps).
- Fabricated-field validator: TEACH (yaml-AST cross-check, structural).
- Audience-voice extension: DISCOVER → Notice (extends existing pattern).

**Fix shape.**
1. New validators in
   [`validators_codebase.go`](../../../internal/recipe/validators_codebase.go):
   `codebase-ig-too-many-items` (count IG numbered items, refuse > 5
   incl. engine-emitted IG #1), `codebase-kb-too-many-bullets`
   (count KB bullets, refuse > 8).
2. New validator file
   `internal/recipe/validators_import_yaml.go` registered to
   `SurfaceEnvImportComments`. Parses yaml AST; extracts field-shaped
   tokens from comments via heuristic (lowercase + underscore OR dot-separated path); refuses comments naming paths
   absent from the yaml. Heuristic skips English prose (no
   underscore/no dot tokens) to avoid false positives.
3. Extend
   [`validators_source_comments.go`](../../../internal/recipe/validators_source_comments.go)
   to also patrol `SurfaceEnvImportComments` for the existing
   audience-voice vocabulary (Notice severity).
4. Brief teaching updates: finalize `validator_tripwires.md` +
   scaffold `content_authoring.md` teach IG cap = 5, KB cap = 8, and
   "no fabricated yaml field names; if you reference a yaml field in
   a comment, that path must exist in the yaml below."

**Acceptance.**
- `TestImportYamlComments_FabricatedFieldName` covers `project_env_vars` (block) + `project.envVariables` (pass).
- `TestCodebaseIG_ItemCap` blocks at 6 items.
- `TestCodebaseKB_BulletCap` blocks at 9 bullets.
- Run-15 deliverable: zero fabricated yaml field names; IG ≤ 5 / KB ≤ 8 across every codebase.

**Cost.** ~50 LoC + 3 validators + 4 unit tests + ~20 lines brief.

### 2.D — Operational preempts continuation

**D.1 (R-14-5).** [`R-14-5`](../runs/14/ANALYSIS.md#r-14-5) —
`zerops_browser` Click handler dispatches at element-center
coordinates without scroll-into-view; headless Chrome viewport ~577px
clips clicks below the fold.

Recommended fix: engine-side. `internal/tools/browser.go::Click`
issues CDP `Element.scrollIntoView` before
`Input.dispatchMouseEvent`. Permanent fix; benefits every future
recipe.

Alternative: D.4 atom extension in `content/briefs/feature/showcase_scenario.md`
naming the constraint and prescribing tabbed/collapsed layouts for >3
panels. Cheaper but recipe-author burden.

**Acceptance.** Run-15 zero `agent-browser-viewport-clipping` facts.

**Cost.** ~10 LoC engine + 1 unit test (recommended path).

**D.2 (R-14-4 stretch).** [`R-14-4`](../runs/14/ANALYSIS.md#r-14-4) —
§N execOnce + §U Vite-bake traps refired at deploy time despite brief
teaching. §B trajectory continuation (run-12 §B → run-13 §B2 →
run-15 candidate §B3). Engine pushes Plan-derivable corrective
patterns into yaml emit:
- Multi-step `initCommands` → engine emits `${appVersionId}-<step>` suffixes verbatim.
- `VITE_*` env pointing at sister-service alias → engine emits a loud "ordered first-deploys required" yaml comment.

**Stretch.** ~40 LoC. Defer to run-16 if Cluster F surface expands.

### 2.E — Content-discipline tighteners

**E.1 — Phase-advance asymmetry teaching.** Add to
[`content/phase_entry/research.md`](../../../internal/recipe/content/phase_entry/research.md)
+ `provision.md` a short section explaining that `complete-phase`
marks `Plan.Completed` but does NOT advance `Plan.Current`; explicit
`enter-phase phase=<next>` is required. ~10 lines per atom.

**E.2 — Audience-rule sweep extension.** Extend the existing
`zcli`/`zerops_*`/`zcp ` audience-voice patrol from CLAUDE.md to also
cover apps-repo README + apps-repo zerops.yaml comments (Notice
severity per existing pattern). ~15 LoC validator extension.

**E.3 — Finalize brief density rewrite.** Now mostly obsolete after
F.4 deletes the body cap. Replace the "Env READMEs target 45+ lines"
teaching with extract-char-cap teaching. ~5 lines change.

**Acceptance.** Run-15: zero phase-advance rediscovery in main
session jsonl; zero `zerops_*` mentions in published apps-repo README
+ apps-repo zerops.yaml; zero `env-readme-too-short` validator firings
(validator deleted in F.4).

---

## 3. Tranche ordering + commits

### Tranche 1 — gate closures (must-ship)

- **Commit 1**: §2.A — R-14-1 closure. ~25 LoC + 3 unit tests.
- **Commit 2**: §2.B — R-14-P-1 closure. ~10 LoC + 1 e2e test.

### Tranche 2 — Cluster F (the bulk)

- **Commit 3**: F.2 — surface contracts at record-time.
- **Commit 4**: F.3 — classification × surface refusal.
- **Commit 5**: F.4 — tier extract char-cap; delete `env-readme-too-short`.
- **Commit 6**: F.5 — IG/KB caps + `import-yaml-fabricated-field-name` + audience-voice sweep extension.

### Tranche 3 — operational + content discipline

- **Commit 7**: §2.D.1 — `zerops_browser` scroll-into-view.
- **Commit 8**: §2.E — phase-advance teaching + finalize brief density.

### Tranche 4 — sign-off

- **Commit 9**: CHANGELOG entry + system.md §4 verdict-table updates + spec edit cross-reference.

### Fast-path / stretch

D.2 (§B3 yaml-emit corrective patterns) inserts as Commit 7.5 if
T2 closes within budget; defer to run-16 otherwise.

---

## 4. Acceptance criteria for run 15 green

### Inherited from run 14 (carry; numbers continue from 51)

- 29-49 (carry from run-14 plan §4) — five phases close ok:true; per-codebase + full-phase verdict-equivalent; zero git-identity / vite-allowlist / defensive re-dispatch / `zcli`-in-CLAUDE leaks; engine-composed dispatch wrapper < 15%.

### New for run 15

- **52** — Stage subdomains materialize on FIRST cross-deploy; zero manual enable calls. (Cluster A)
- **53** — Every dispatched scaffold brief carries `## Recipe-knowledge slugs you may consult`. (Cluster B)
- **54** — Tier README intro extracts ≤ 350 chars across all six tiers. (Cluster F.4)
- **55** — KB sections ≤ 8 bullets per codebase. (Cluster F.5)
- **56** — IG sections ≤ 5 items per codebase including engine-emitted IG #1. (Cluster F.5)
- **57** — Zero fabricated yaml field names in env import.yaml comments. (Cluster F.5)
- **58** — Apps-repo IG/KB/yaml-comments zero `zerops_*` tool name leaks. (Cluster E.2)
- **59** — `record-fragment` response carries `surfaceContract`. (Cluster F.2)
- **60** — `record-fragment` accepts `classification`; engine refuses incompatible pairs. (Cluster F.3)
- **61** — Zero phase-advance rediscovery in main session jsonl. (Cluster E.1)
- **62** — `zerops_browser` Click scrolls into view OR D.4 atom prescribes tabbed/collapsed layouts. (Cluster D.1)

### Stretch

- **63** — Honest content grade ≥ 8.5/10 vs reference (auto-grade and honest grade converge).
- **64** — Honest content grade ≥ 9.0/10 vs reference (engine reaches content-quality plateau).
- **65** — D.2 §B3 yaml-emit corrective patterns ship; zero operational-trap rediscovery facts.

---

## 5. Non-goals for run 15

- No new managed-service category support.
- No parent-recipe chain validation.
- No additional bundler atoms beyond Vite (Webpack / Rollup defer to run-16+).
- No engine-side automated content grader; honest grade stays human-judged.
- No template restructuring beyond F.4's env-readme template tweak.
- No `verify-subagent-dispatch` reactivation (E.2 retired in run-14).
- No persistence design for `start attach=true` (C.1 deferred per [run-14 §7 q2](run-14-readiness.md#7-open-questions)).

---

## 6. Risks + watches

### Risk 1 — Cluster A's plan-declared-intent shape misses an edge

If a service declares `enableSubdomainAccess: true` but the platform
refuses (e.g. yaml omits the ports block), the auto-enable call fails
where today's code skips silently.

**Mitigation.** `ops.Subdomain` returns a structured error on the "service
not eligible" platform response; deploy_subdomain.go soft-fails on
that specific error. Existing manual recovery path remains valid.
Unit test exercises the soft-fail.

### Risk 2 — Cluster B's audit doesn't surface the actual gap

Three viable gap shapes named in §2.B; the actual gap might be a
fourth.

**Mitigation.** The e2e test
`TestScaffoldBrief_DispatchedToProductionAgent_CarriesReachableSlugList`
fails on run-14's current code regardless of the gap shape; iterate
until it passes. Don't paper over with workaround.

### Risk 3 — Cluster F.2's response-payload growth crowds context

`SurfaceContract` per `record-fragment` response adds ~300-500 bytes
per call × ~84 calls per run = ~30-40 KB.

**Mitigation.** Brief teaches "read the contract once per fragment-id;
it doesn't change between calls of the same id." If context pressure
surfaces in run-15, ship suppress-on-repeat optimization in run-16.

### Risk 4 — Cluster F.5's fabricated-field-name validator over-flags prose

Heuristic might flag English prose tokens that look like field paths.

**Mitigation.** `looksLikeYamlField` requires underscore + lowercase
OR dot-separated path; English prose words fail the heuristic. Tests
cover signal + noise. If false positives surface in run-15, soften
heuristic OR demote to Notice for the false-positive paths.

### Risk 5 — I/O boundary recurrence (carries forward from run-14 §6 Risk 1)

> "Every engine extension a future readiness plan ships must list its
> I/O boundary explicitly."

Run-15 boundaries:
- Cluster A: `ops.Subdomain` enable RPC; coherence model = platform's idempotency check.
- Cluster B: brief composer reads in-memory `Resolver`; single-process Go map.
- Cluster F.2: `surfaceContracts` map + `Plan.Fragments`; single-process Go map.
- Cluster F.5: on-disk yaml file; read once at validate-time, no concurrent write.

No race-prone read-after-write across this run.

### Risk 6 — Catalog drift via per-fragment-id contract values

Cluster F.2 ships `surfaceContracts` values per surface. Risk: future
plans add hardcoded "ban this string" lists under cover of the
contract structure.

**Mitigation.** Spec
[§Maintenance](../../spec-content-surfaces.md#maintenance) explicitly
forbids per-fragmentId banned-string lists. Code review every PR
extending `SurfaceContract`; if a new field's purpose is "string the
agent can't write", that's the catalog-drift signature — raise
immediately.

---

## 7. Open questions

1. **Should Cluster F.4 delete the body cap entirely or repurpose to refuse padding?** Both reference recipes leave body empty; recommend delete (option a). Revisit if a porter case surfaces in run-16.
2. **Does Cluster F.3's `classification` field become mandatory in run-16+?** Ships optional in run-15 for back-compat; if dogfood adoption is broad, refuse records without classification in run-16.
3. **Should F.5's fabricated-field validator extend to apps-repo zerops.yaml comments?** Likely yes — same AST cross-check applies. Defer to implementation choice.
4. **Should friendly-authority voice teaching be made operational at record-time?** Spec teaches it; voice drifts more than structural rules. If voice drifts in run-15, ship in run-16 by adding example phrases to per-fragment-id contracts.
5. **D.2 §B3 ships in run-15 or defers?** Decision at T2 close: if F.1-F.5 green and engine LoC budget remaining > 50, ship; else defer.

---

## 8. After run 15 — what's next

If criteria 52-62 hold:
- Honest grade should hit ≥ 8.5/10. If 64 also holds (≥ 9.0): engine reaches content-quality plateau.
- Run-16+ shifts to recipe breadth (non-Laravel/non-NestJS), bundler breadth (Webpack/Rollup), parent-recipe chain validation, recipe-root README cross-codebase runbook content (open question from runs 11/12), R-14-4 §B3 if not shipped in run-15, F.3 mandatory classification.

If 52-62 close RED:
- ANALYSIS names the structural cause. Most likely:
  - **A partial**: A.1.b's plan-declared-intent shape misses an edge (Risk 1).
  - **B partial**: audit surfaces a deeper gap (Risk 2).
  - **F.2 partial**: agents ignore the per-fragment surface contract despite brief teaching. Promote to in-stitched-content validator that anchors structural caps; brief loses informational role and gains enforcement role.
  - **F.5 over-flags**: heuristic false positives. Soften OR demote to Notice.

The whole-engine path forward stays:
- Tighter audience boundary per [system.md §1](../system.md).
- TEACH-side positive shapes per [system.md §4](../system.md).
- Engine pushes resolved truth into briefs + record-fragment responses.
- Sub-agent self-validate before terminating; main only handles phase-state transitions.
- Every engine extension audited against its I/O boundary before ship.
- **Every brief / record-fragment-response / validator extension has an e2e test that observes the production output, not just an isolation test.**

---

## 9. Pre-flight verification checklist

Before run-15 dogfood:

- [ ] All 9 commits land cleanly (Tranches 1-4).
- [ ] `make lint-local` passes.
- [ ] `go test ./internal/recipe/... -count=1 -race` passes.
- [ ] `go test ./internal/tools/... -count=1` passes (deploy_subdomain.go).
- [ ] `go test ./internal/ops/... -count=1` passes (subdomain.go retry).
- [ ] No `replace` in `go.mod`.
- [ ] CHANGELOG entry summarizes Cluster A-E with file:line for key changes.
- [ ] system.md §4 verdict-table updated for Cluster A.1, B (stealth-regression closure), F.1-F.5.
- [ ] Spec
      [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md)
      surfaces_test
      [`TestSurfaceContract_FormatSpecAnchorsExist`](../../../internal/recipe/surfaces_test.go#L50)
      passes (heading anchors preserved).
- [ ] **End-to-end production-surface audit (per §0 / Risk 2)**:
      every brief / record-fragment-response / validator extension has
      an e2e test that observes the production output. List the e2e
      test name + the dispatched output it observes. The two new e2e
      tests:
  - [ ] `TestScaffoldBrief_DispatchedToProductionAgent_CarriesReachableSlugList` (Cluster B)
  - [ ] `TestRecordFragment_ResponseCarriesSurfaceContract` (Cluster F.2)
- [ ] **I/O boundary audit (per Risk 5)**: for each Cluster A/B/F/D
      workstream, list the read source and write destination. Confirm
      no read crosses a network filesystem from a fresh write OR a
      propagation-race-prone REST surface.

When all green: dogfood `nestjs-showcase` (replay).

If any check fails or surfaces new questions, surface immediately —
do not paper over. Run-14's R-14-1 + R-14-P-1 were both discoverable
in production-surface review; the readiness plan didn't surface them
because the e2e production-surface step was not yet a checklist item.
Run-15 readiness encodes it.
