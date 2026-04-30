# Eval Phase 1 — Bootstrap variants × Develop ending with deploy

> **Status**: Plan, awaiting approval before scenario authoring/execution.
> **Date**: 2026-04-30
> **Predecessor**: pre-internal-testing audit Rounds 1–3 (all archived). Most C/H/F findings landed; a few residue-level ones (F1 record-deploy gate scope, F5 mode-expansion hint, F6 prose drift) remain open and will surface naturally in this run as eval signal.
> **Scope IN**: bootstrap (classic / recipe / adopt) × mode (dev / simple / standard) × env (container / local) × runtime class (dynamic / static / implicit-webserver), each ending in a successful first deploy + verify, with the agent producing a structured `## EVAL REPORT` self-assessment.
> **Scope OUT**: recipe AUTHORING (`internal/recipe/` + `zerops_recipe` v3), git-push setup, build-integration, mode expansion, multi-deploy iteration loops — all Phase 2.

---

## 1. Why this phase exists

After three audit rounds the static analysis has hit diminishing returns. The remaining unknowns are dynamic — what *real LLM agents* do when the cleaned-up corpus lands in front of them. The Round 3 audit explicitly flagged the architectural risk of treating `record-deploy → events:ACTIVE → verify → auto-close` as convention rather than contract; Phase 1 is the first pass that exercises that contract under realistic agent latency.

Two questions this phase answers:

1. **Does the cleaned corpus actually drive the lifecycle correctly?** The retired-vocab sweep, async-build classifier, asset-pipeline rewrite, capability gating, and aggregate-axis fix all changed what the agent reads. We need to see what an agent does with the new wording.
2. **Where does the mental model still diverge from implementation?** The user's manual transcript surfaced friction that no atom or test caught (Razor view runtime compilation, GitHub Actions secret race, `EnsureCreated` vs existing tables). Real runs will surface the next layer of these.

---

## 2. The eval framework we're using (already shipped)

Read this section before designing scenarios — every recommendation below is grounded in code that already exists.

| Component | File | What it does |
|---|---|---|
| Scenario parser | `internal/eval/scenario.go` | Markdown + YAML frontmatter → `Scenario{ID, Description, Seed, Fixture, PreseedScript, Expect, FollowUp, Prompt}`. Validates `seed ∈ {empty, imported, deployed}` and that fixtures exist when needed. |
| Runner | `internal/eval/runner.go` + `scenario_run.go` | `Runner.RunScenario()` cleans + seeds project, runs Claude Code with the prompt, captures transcript + tool calls, calls `GradeWithProbe`. WorkDir defaults to `/var/www`. |
| Grader | `internal/eval/grade.go` | `GradeWithProbe` checks `mustCallTools`, `workflowCallsMin`, `mustEnterWorkflow`, `requiredPatterns`, `forbiddenPatterns`, `requireAssessment`, `finalUrlStatus` (HTTP probe). Returns `{Passed, Failures}`. |
| Self-eval prompt | `internal/eval/prompt.go` `assessmentInstructions` | Appended to every task. Asks for `## EVAL REPORT` with sections: Deployment outcome (SUCCESS/PARTIAL/FAILURE), Workflow execution stats, Failure chains (with `WRONG_KNOWLEDGE`/`MISSING_KNOWLEDGE`/`UNCLEAR_GUIDANCE`/`PLATFORM_ISSUE` taxonomy), Information gaps, Wasted steps, What worked well. |
| Assessment extractor | `internal/eval/extract.go` | `ExtractAssessment(log)` finds `## EVAL REPORT` block in the JSONL transcript. |
| Suite runner | `internal/eval/suite.go` | `Suite.RunAll(recipes []string)` runs sequentially, writes `suite.json` summary. |
| Cleanup | `internal/eval/cleanup.go` | `CleanupProject()` deletes all non-system services + cleans workdir (preserves `.claude`, `.mcp.json`, `.zcp`). Used between scenarios. |
| CLI | `cmd/zcp/eval.go` | `zcp eval scenario <path>`, `zcp eval suite <ids…>`, `zcp eval results <suite-id>`, `zcp eval cleanup`. |

**No new framework code is needed for Phase 1.** Everything below is content + curation work plus targeted gap-filling scenarios.

---

## 3. Coverage matrix — what to run

The combinatoric cross-product (3 routes × 3 modes × 2 envs × 3 runtime classes = 54 cells) is wasteful. We need REPRESENTATIVES that each cover multiple cells.

### 3.1 Tier-1 SMOKE (4 scenarios — must pass before anything else)

These are the load-bearing baseline. If any of these fail on the current main, fix BEFORE running tier-2.

| # | Scenario ID | Source | Route | Mode | Env | Runtime | Why this one |
|---|---|---|---|---|---|---|---|
| S1 | `weather-dashboard-bun` | EXISTING | recipe/classic | standard | container | dynamic | Newest dynamic runtime, all-in-one toolchain, dev/stage pair → exercises cross-deploy promote-stage atom (H1 + H2 fixes) |
| S2 | `weather-dashboard-php-laravel` | EXISTING | recipe | standard | container | implicit-webserver + managed | PHP omits `start:`, Laravel+Vite asset pipeline (C5 fix), DB env-var resolution |
| S3 | `greenfield-nodejs-todo` | EXISTING | recipe OR classic | simple | container | dynamic + managed | Greenfield REST API — agent picks route freely; covers two-call discovery + commit; merged dev+stage setup |
| S4 | `adopt-existing-laravel` | EXISTING | **adopt** | standard | container | (existing) | Only adopt-route full-flow scenario today; exercises discovery `routeOptions[]` ranking + adopt commit |

**Total smoke time**: ~4 × 12 min = 50 min one-shot.

### 3.2 Tier-2 COVERAGE (5 scenarios — broaden runtime + classic-override + collision)

After smoke is green. These fill specific gaps.

| # | Scenario ID | Source | Route | Mode | Env | Runtime | Why |
|---|---|---|---|---|---|---|---|
| S5 | `weather-dashboard-nextjs-ssr` | EXISTING | recipe | simple/standard | container | dynamic SSR | Asset-pipeline first-deploy atoms (`develop-first-deploy-asset-pipeline-container.md`); confirms C5 rewrite (Vite via dev_server primitive, no nohup SSH) actually drives correct agent behavior |
| S6 | `weather-dashboard-dotnet` | EXISTING | classic | simple/standard | container | dynamic (.NET) | Manual transcript showed real friction here (Razor runtime compilation, EF EnsureCreated vs existing tables) — eval should surface as `Information gaps` / `Failure chains` in EVAL REPORT, feeding back into recipe doc |
| S7 | `weather-dashboard-rust` | EXISTING | recipe/classic | simple | container | dynamic (compiled) | Compiled-binary deploy path (`./target/release/...` cross-deploy `deployFiles`), distinct from interpreted dynamic |
| S8 | `bootstrap-user-forces-classic` | EXISTING | classic (forced) | * | container | * | User explicitly refuses recipes — agent must honor the override (DM-equivalent for route choice) |
| S9 | `bootstrap-recipe-collision-rename` | EXISTING | recipe | standard | container | * | Hostname collision → recipe rewrite via plan rename (RCO-2 invariant) |

**Total tier-2 time**: ~5 × 12 min = 60 min.

### 3.3 Tier-3 GAP-FILLER (2 NEW scenarios — fill genuine matrix holes)

> **User decision 2026-04-30**: LOCAL env coverage explicitly OUT for Phase 1 (originally planned S11 dropped). LOCAL env is deferred without a specific re-pickup phase — flag during Phase 2 design if it becomes blocking.

Today's corpus has zero coverage in two cells worth filling. Each is a small markdown file modeled on `greenfield-nodejs-todo.md`. Naming follows the dimensional pattern `bootstrap-{route}-{runtime}-{mode}` so the matrix dimension is readable from the scenario ID.

| # | NEW Scenario ID | Route | Mode | Env | Runtime | Gap closed |
|---|---|---|---|---|---|---|
| S10 | `bootstrap-recipe-static-simple` | recipe (`vue-static-hello-world`) | simple | container | **static** | Static runtime class has ZERO greenfield coverage. Tests `develop-static-workflow.md` atom + `dist/`-pattern `deployFiles` (DM-3) |
| S11 | `bootstrap-classic-node-standard` | classic | **standard** | container | dynamic + managed | Today's standard-mode coverage rides on `weather-dashboard-*` which use recipes. NEW: a manual standard-pair plan exercises B-flow + cross-deploy + auto-close on the dev/stage pair via classic route. Catches H1 (Plan cross-deploy `sourceService`) + H2 (`setup="prod"`) regressions in the manual-plan path. |

**Per-scenario expectation skeleton** (Codex recommendation, applies to all three):
- `seed: empty`, `requireAssessment: true`, `finalUrlStatus: 200`
- `mustEnterWorkflow: [bootstrap, develop]`
- `requiredPatterns`: include `'"route":"<expected>"'` to pin route choice
- `forbiddenPatterns`: include the WRONG route to fail-fast on misclassification
- `workflowCallsMin`: 7-9 (calibrated post-first-run)
- `followUp`: 2-3 questions probing the route/mode decision and what specifically went right or wrong

**Total tier-3 time**: ~3 × 12 min = 35 min, plus ~2h author time per scenario.

### 3.4 What's intentionally OUT for Phase 1

- **`bootstrap-resume-interrupted`** — recovery edge case, not "first wave" baseline. Defer to Phase 1.5.
- **`develop-*`** scenarios — they assume an already-deployed service (`seed: deployed`). Phase 1 is the BOOTSTRAP→DEPLOY chain; develop-iteration scenarios run against post-deploy state. Some will be picked up implicitly by tier-2 (the agent iterates within S1-S9 to reach green).
- **`close-mode-git-push-setup`, `e2-build-fail-classification`** — Phase 2 (close-mode pivot, failure classification edges).
- **All weather-dashboard-* not in S1-S7** — already-tested runtimes, run them ad-hoc when their runtime class needs re-verification.
- **`export-deployed-service`** — Phase 2 (export workflow).

---

## 4. Self-eval design — use what's already there

The `assessmentInstructions` block already extracts everything we need. Per-scenario `followUp[]` already prompts targeted reflection. Phase 1 does NOT need a new mechanism.

What it DOES need: a downstream pass that AGGREGATES every scenario's `## EVAL REPORT` into a single triage doc keyed on `Root cause` taxonomy. The framework writes `suite.json` with raw transcripts; Phase 1 deliverable adds:

```
internal/eval/aggregate.go  (NEW, ~80 lines)
- ParseAssessment(log) → struct {Outcome, FailureChains[], InformationGaps[], WastedSteps[]}
- AggregateSuite(suiteResult) → triage report, grouped by RootCause
```

Output goes into `eval-results/<suite-id>/triage.md` — a human-readable summary of WHAT FAILED ACROSS SCENARIOS, ranked by frequency. This is the actual feedback loop the user asked for ("posbirat feedback z realnych behu").

**Scope decision**: write the aggregator as part of Phase 1 (it's the deliverable). Don't try to be smart about LLM-driven re-grading — the agent's own report + simple frequency aggregation is plenty for first-wave triage.

---

## 5. Run mechanics

### 5.1 Where it runs

- **Project**: `eval-zcp` (`i6HLVWoiQeeLv8tV0ZZ0EQ`, org `Muad`) per CLAUDE.local.md.
- **Host**: `zcp` container in eval-zcp project (where `zcli` is auth'd, `ZCP_API_KEY` is in env).
- **Workdir**: `/var/www` (default) — runner cleans + reseeds between scenarios.

### 5.2 Sequence

⚠️ **CLI gap discovered during planning**: `zcp eval suite` (`internal/eval/suite.go::Suite.RunAll`) calls `runner.Run(recipe, …)` — the RECIPE-suite runner, not the SCENARIO suite. There is no `zcp eval scenario-suite` command yet. Two options:

**Option A (preferred, ~30 min)** — add a thin scenario-suite mode:
- Extend `cmd/zcp/eval.go` with `case "scenario-suite":` that loops over scenario paths and calls `runner.RunScenario` for each, mirroring `Suite.RunAll`'s sequential + cleanup-between-runs structure.
- Single suite ID across all scenarios so triage aggregates per-suite.
- Land this as part of "Day 0" (~30 min effort, alongside the aggregator).

**Option B (zero code)** — shell loop:
```bash
for s in weather-dashboard-bun weather-dashboard-php-laravel greenfield-nodejs-todo adopt-existing-laravel; do
  ssh zcp "zcp eval scenario internal/eval/scenarios/$s.md"
done
```
Suite ID changes per scenario; aggregator must walk multiple result dirs. Slightly less convenient for triage but works today.

Recommend **Option A**. With it, the runs become:

```bash
# Smoke — must all pass before tier-2 starts
ssh zcp "zcp eval scenario-suite weather-dashboard-bun weather-dashboard-php-laravel greenfield-nodejs-todo adopt-existing-laravel"

# Coverage — after smoke green
ssh zcp "zcp eval scenario-suite weather-dashboard-nextjs-ssr weather-dashboard-dotnet weather-dashboard-rust bootstrap-user-forces-classic bootstrap-recipe-collision-rename"

# Gap-filler (authored as part of Phase 1, reuses the new CLI mode)
ssh zcp "zcp eval scenario-suite bootstrap-recipe-static-simple bootstrap-classic-node-local bootstrap-classic-node-standard"
```

Sequential within each suite — `CleanupProject` between scenarios is non-negotiable (shared project state). Across suites we could parallelize on multiple zcp containers, but Phase 1 doesn't need that.

### 5.3 Cleanup discipline

`CleanupProject` deletes everything matching `prefix=` plus everything not in the system list. The `ProtectedService = "zcp"` constant guards the runner itself. Confirmed safe to run in `eval-zcp`. NEVER run in any other project — there's no dry-run mode.

---

## 6. Acceptance criteria

Phase 1 ships when:

1. **Smoke (S1-S4) passes 4/4** on the current `main` (or with a small follow-up commit if 1-2 fail trivially).
2. **Coverage (S5-S9) passes ≥ 4/5**. One scheduled exception is fine; document the failure root cause.
3. **Gap-fillers (S10-S12) authored + run + each passes**.
4. **Aggregator (`eval-results/<suite-id>/triage.md`)** exists and groups failures by root cause taxonomy.
5. **Triage doc identifies ≥ 3 actionable backlog entries** (one per recurring `WRONG_KNOWLEDGE` / `MISSING_KNOWLEDGE` / `UNCLEAR_GUIDANCE` cluster). Backlog entries land under `plans/backlog/<slug>.md` per the global workflow.
6. **No tracked-OPEN F-finding (F1, F5, F6) is the cause of more than ONE scenario failure.** If F1's record-deploy-on-subsequent-call bug fails 4 scenarios, fix F1 BEFORE shipping Phase 1 — don't ship a baseline that the next person will inherit broken. (F1 is the most likely candidate.)

---

## 7. Anti-goals — what to actively NOT do

- **Don't add multi-deploy iteration scenarios** — that's Phase 2. Phase 1 is bootstrap → first deploy → verify → green.
- **Don't add git-push, mode-expansion, build-integration scenarios** — Phase 2.
- **Don't extend the framework** beyond the aggregator. The runner, grader, scenario format are mature; touching them invites yak-shaving.
- **Don't author scenarios for recipe USE outside the existing 11 weather-dashboard-* + 2 greenfield-* + adopt-existing-laravel.** Tier-3 adds exactly 3 new scenarios with clear gap closure rationale; resist the urge to add a 4th.
- **Don't try to grade the agent's REPORT itself with another LLM pass** — the structured taxonomy makes simple aggregation enough for first signal.
- **Don't fix every audit-residue finding before running** — let the eval surface which ones bite. F1 is high-suspicion; if it fires, fix it. F5 + F6 surface only on specific scenarios; let them.

---

## 8. Decisions confirmed (2026-04-30)

1. **Local env scenario** — DROPPED for Phase 1. No re-pickup phase yet defined; surface during Phase 2 design if it becomes blocking. Tier-3 reduced from 3 → 2 scenarios (S11 was the local-env scenario; S12 renumbered to S11).
2. **dotnet/Razor friction** — agent surfaces as Failure chain in EVAL REPORT → fold into `internal/knowledge/recipes/dotnet-hello-world.md` per CLAUDE.md "Recipe-specific findings go in recipes". NO preventive `forbiddenPatterns`.
3. **Smoke-as-gate vs parallel work** — TBD with user (clarification pending — see chat).
4. **Post-Phase-1 next** — Phase 1.5 = `bootstrap-resume-interrupted` + `develop-*` iteration scenarios. **Git-push / build-integration / mode-expansion scenarios are deferred without a specific phase number** ("upravime pozdeji"). Phase 2 scope to be redefined after Phase 1.5 triage.

---

## 9. Suggested execution order

1. **Day 0 (~30 min)**: add `case "scenario-suite":` to `cmd/zcp/eval.go` per §5.2 Option A. Mirror `Suite.RunAll`'s loop but call `runner.RunScenario`. Single suite ID across the run. Pin with a smoke test that runs two scenarios under a stub runner.
2. **Day 0 (~4h)**: write the aggregator (`internal/eval/aggregate.go`). Parses `## EVAL REPORT` per scenario, groups Failure chains by `Root cause` taxonomy, writes `eval-results/<suite-id>/triage.md`. Pin with one fixture-driven test. Add `zcp eval results <suite> --triage` flag to invoke it.
3. **Day 0 (~1h)**: scrub `develop-pivot-auto-close.md:56` "push-dev strategy" prose drift (Round 3 F6 — pre-eval cleanup so a Phase 1.5 pickup doesn't hit it). One-line change.
4. **Day 1**: run Tier-1 smoke (S1-S4) sequentially via the new scenario-suite mode. Triage failures.
5. **Day 1-2**: fix any blocker that breaks ≥ 2 smoke scenarios (likely candidate: F1 record-deploy gate; F5 `mode-expansion` hint may also surface if any scenario triggers the git-push path inadvertently).
6. **Day 2**: write Tier-3 (S10-S12) scenarios using the established markdown+frontmatter format.
7. **Day 2-3**: run Tier-2 coverage (S5-S9) sequentially.
8. **Day 3**: run Tier-3.
9. **Day 3**: triage doc → backlog entries → Phase 2 brief.
10. **Day 4** (Phase 2 prep): based on Phase 1 triage, scope Phase 2 (git-push, build-integration, mode-expansion).

Total: ~4 working days from approval to Phase 2 brief. Wall-clock dominated by ~12 × 12 min = 2.5h pure run time + authoring + triage.

---

## 10. Phase 1.5 preview (next phase — confirmed direction)

After Phase 1 triage closes, Phase 1.5 broadens the develop-side coverage with scenarios that already exist (no fresh authoring):

- `bootstrap-resume-interrupted` — recovery from dead-PID session (claim + continue)
- `develop-add-endpoint` — incremental work on already-deployed service
- `develop-ambiguous-state` — agent recovers from incomplete state
- `develop-close-mode-unset-regression` — `develop-strategy-review` atom firing path
- `develop-dev-not-started` — recovery from missed `zerops_dev_server action=start`
- `develop-dev-server-{container,local}` — dev-server lifecycle exercising
- `develop-first-deploy-branch` — second-pass on the never-deployed → deployed transition
- `develop-pivot-auto-close` — task-pivot triggering auto-close

Phase 1.5 leans entirely on existing `seed=deployed` / `seed=imported` scenarios, plus the new aggregator from Phase 1. No CLI changes needed.

## 11. Deferred — no scheduled phase

Per user decision 2026-04-30, the following are explicitly out of the current plan ("upravime pozdeji" — to be redefined later):

- `git-push` setup + async `record-deploy` lifecycle scenarios
- `build-integration` (webhook + actions) full flows incl. GitHub Actions secret wiring
- Mode expansion (dev → standard) exercising F5 fix
- Multi-runtime project cross-service env-var resolution
- Failure-recovery iteration tier ladder (DIAGNOSE → SYSTEMATIC → STOP)
- Export workflow scenarios

These will get a dedicated plan after Phase 1 + 1.5 triage shapes priorities.
