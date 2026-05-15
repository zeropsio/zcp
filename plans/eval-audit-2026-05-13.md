# Eval Ecosystem Audit — 2026-05-13

> **Scope**: e2e tests, `internal/eval/`, `eval/`, behavioral runner (flow-eval),
> closed-loop knowledge runner, integration tests. Goal: identify orphans, dead
> code, era overlap, and stale plans. **Not** asking permission to act — this
> document maps problems and proposes order, the user decides what ships.
>
> **Scope exclusion**: recipe-related code (Aleš's surface — `internal/recipe/`,
> `internal/tools/workflow_recipe.go`, `internal/content/workflows/recipe/`,
> `docs/zcprecipator*`). Findings adjacent are mentioned, not actioned.

## TL;DR

The eval surface contains **three temporally-stacked eras** that were added
incrementally and never reconciled:

1. **Closed-loop knowledge eval** (Feb-Apr 2026) — `eval/AGENT_PROMPT.md` +
   `eval/run.sh` + 13 Python/shell scripts in `eval/scripts/` + `eval/functional/`
   + 60 weather audit dumps in `eval/results/`. **Dormant since Apr 25**. The
   self-improving knowledge loop is no longer the testing strategy.
2. **Substring-grader scenario runner** (Apr) — `Runner.RunScenario`, `grade.go`,
   plus 40 scenarios under `internal/eval/scenarios/` (11 of them are
   "weather-dashboard-*" residue). Surfaced via `zcp eval scenario|run|suite|triage`.
   **Still wired**, but no live invocation in plans/CI since flow-eval landed.
3. **Behavioral C4 eval** (May 3+) — `Runner.RunBehavioralScenario`,
   `eval/behavioral/`, flow-eval.sh + flow-eval-local. **Active, maintained,
   the current testing strategy** for cross-cutting agent behavior.

E2e cleanup landed last commit (40→27 files). The next-leverage cleanup is
**era-1 amputation** (~3.6k LoC of pohrobci) and **deduping era-2 scenarios
against behavioral** (40 substring-grader scenarios with significant intent
overlap to the 26 behavioral scenarios).

Foundation (Runner, scenario.go, grade.go, seed.go, cleanup.go,
behavioral_run.go) is sound. The mess is at the edges.

---

## Map: what's alive vs dead

### `internal/eval/` — Go runner package (3,576 LoC non-test)

| File | LoC | Era | Callers | Verdict |
|---|---|---|---|---|
| **runner.go** | 249 | 3 | `cmd/zcp/eval*.go` init path | ALIVE — orchestrator |
| **scenario.go** | 215 | 2 | RunScenario + RunBehavioralScenario | ALIVE — DSL parser |
| **scenario_run.go** | 233 | 2 | `cmd/zcp/eval.go::runEvalScenario` | ALIVE — single-shot path |
| **behavioral_run.go** | 458 | 3 | `cmd/zcp/eval_behavioral*.go` | ALIVE — two-shot path |
| **behavioral_assets.go** | 49 | 3 | behavioral_run.go | ALIVE — embeds retrospective_prompts/ |
| **cleanup.go** | 514 | 3 | runEvalCleanup + run/suite seed paths | ALIVE — Cleaner abstraction |
| **seed.go** | 223 | 3 | scenario_run.go, local_setup.go | ALIVE — fixture seeder |
| **local_setup.go** | 161 | 3 | `cmd/zcp/eval_behavioral_local.go` | ALIVE — Mac-side workdir glue |
| **suite.go** | 167 | 2 | `cmd/zcp/eval.go::runEvalRun` | ALIVE — batch orchestrator |
| **grade.go** | 187 | 2 | scenario_run.go + behavioral_run.go (24 grep hits) | ALIVE — substring grader |
| **probe.go** | 130 | 2/3 | scenario_run.go (HTTP final URL probe) | ALIVE |
| **extract.go** | 177 | 1/2 | scenario_run.go (ExtractToolCalls, 7 hits) | ALIVE — transcript reader |
| **eval.go** | 88 | 1 | shared types used everywhere | ALIVE — type defs |
| **prompt.go** | 325 | 1 | runner.go::initEval (recipe metadata, 3 hits) | ALIVE — recipe bootstrap helper |
| **markdown.go** | 80 | 1 | prompt.go internal only | ALIVE — but tied to prompt.go |
| **usersim.go** | 840 | 3 | behavioral_run.go::userSimRunner | ALIVE — multi-turn user simulator |
| **recipe_create.go** | 259 | 3 | `cmd/zcp/eval.go::runEvalCreate` | ALIVE — but **Aleš scope adjacent** |
| **aggregate.go** | 707 | 1 | `cmd/zcp/eval.go::runEvalTriage` (`Triage` type) | **ZOMBIE-ish** — see Finding 2 |
| **instruction_eval.go** | 320 | 2 | **0 external callers** | **ZOMBIE** — see Finding 1 |
| **instruction_variants.go** | 194 | 2 | only by instruction_eval.go (orphan tree) | **ZOMBIE** — see Finding 1 |

### `eval/` — non-Go assets (~2.3k LoC + 60 results dumps)

| Artifact | LoC | Era | Live? | Verdict |
|---|---|---|---|---|
| **AGENT_PROMPT.md** | 219 | 1 | `eval/run.sh` only | **ARCHIVE** — dormant since May 3 |
| **run.sh** | 25 | 1 | manual invocation only | **ARCHIVE** — coupled to AGENT_PROMPT |
| **taxonomy.yaml** | 95 | 1 | AGENT_PROMPT.md only | **DELETE** — sole consumer is dormant |
| **functional/prompt-dev.md** | 41 | 1 | nobody — never referenced | **DELETE** |
| **functional/run-dev.sh** | 138 | 1 | manual only — invokes generate.py + extract-tool-calls.py | **DELETE** |
| **functional/cleanup-dev.sh** | 21 | 1 | manual only | **DELETE** |
| **scripts/build-deploy.sh** | 83 | 2/3 | flow-eval.sh + functional/run-dev.sh | **KEEP** — live edge |
| **scripts/cleanup.sh** | 24 | 1/2 | `internal/eval/cleanup.go` comment ref only | **VERIFY** — possibly stale ref |
| **scripts/generate.py** | 248 | 1 | AGENT_PROMPT.md | **DELETE** |
| **scripts/score.py** | 221 | 1 | functional/run-dev.sh (dies with it) | **DELETE** |
| **scripts/extract-tool-calls.py** | 268 | 1 | AGENT_PROMPT.md + functional/run-dev.sh | **DELETE** |
| **scripts/timeline.py** | 307 | 1 | docs/zcprecipator2/* archived | **ARCHIVE** if anyone wants it; else **DELETE** |
| **scripts/analyze_bash_latency.py** | 159 | 1 | docs archive only | **DELETE** |
| **scripts/aggregate-weather-audit.py** | 403 | 1-weather | one-shot Apr 24-25 sweep | **DELETE** |
| **scripts/autonomous-weather-sweep.py** | 234 | 1-weather | one-shot | **DELETE** |
| **scripts/run-multi-runtime-weather.sh** | 63 | 1-weather | one-shot | **DELETE** |
| **scripts/run-batch3-followup.sh** | 22 | 1-weather | one-shot | **DELETE** |
| **scripts/run-scenario.sh** | 74 | 1 | nobody live | **DELETE** |
| **scripts/version_metrics.sh** | 32 | 1 | docs archive only | **DELETE** |
| **results/** (60 files) | n/a | 1-weather | gitignored (`eval/results/*` in root) | **DELETE working-tree** — `git clean -fX` style |

### Scenarios — three populations

| Where | Count | Used by | Status |
|---|---|---|---|
| `eval/behavioral/scenarios/` | 26 | `RunBehavioralScenario` via flow-eval.sh | **ACTIVE** — single source of truth for agent-behavior coverage |
| `eval/behavioral/scenarios-local/` | 5 | `RunBehavioralScenario` via flow-eval-local | **ACTIVE** — local-mode sibling |
| `internal/eval/scenarios/` | 40 | `RunScenario` substring grader | **HEAVY OVERLAP** — see Finding 3 |

---

## High-signal findings

Ordered by leverage × confidence × low-risk.

### Finding 1 — `instruction_eval.go` + `instruction_variants.go` are pure zombies (514 LoC, 0 callers)

**What**: `RunInstructionEval`, `InstructionEvalConfig`, `EvalScore`,
`InstructionEvalResult`, `InstructionVariants`, `InstructionScenario`,
`InstructionVariant` — none of these are called from anywhere outside their own
test files and each other. No CLI subcommand wires them up. Tests
(`internal/eval/eval_test.go` ~16.9 KB) exercise the orphan tree in isolation.

**Why it matters**: 514 LoC + ~17 KB test file all maintained as-if-live. Every
refactor that touches `internal/eval/` invariants is paying tax on dead code
(the recent `EnvPlan` work in commit `536f7bbc` is one example — it had to
satisfy strict linters on a code path nobody runs).

**Origin**: A/B instruction-surface test infra introduced Apr 21 (commit
`d174b4be`, "feat: add service identity to MCP instructions"). The
instruction-surfaces work shipped via a different mechanism (description-drift
lints + per-render goldens) and this runner was never wired into the new path.

**Action**: Delete `instruction_eval.go`, `instruction_variants.go`, and the
two impacted test files (`eval_test.go` and any `instruction_*_test.go`).
Verify nothing else imports the types via `grep -rn "InstructionVariant\|RunInstructionEval"`.

**Blast radius**: 514 LoC + ~17 KB of test code. No production callers.

### Finding 2 — `aggregate.go` + `zcp eval triage` is a quiet orphan (963 LoC, last invocation Apr-era)

**What**: 707 LoC `aggregate.go` (with 256 LoC test) implements
`ParseAssessment`, `AggregateScenarioSuite`, `RenderTriageMarkdown` — a suite
result aggregator that consumes scenario-run outputs and renders an `EVAL
REPORT` Markdown triage. Wired to `cmd/zcp/eval.go::runEvalTriage`.
Introduced commit `de9d0437` (`feat(eval): scenario-suite CLI + EVAL REPORT
triage aggregator`).

**Why it matters**: The behavioral path replaced "triage suite output" with
"read self-review.md verbatim and discuss." There is no flow-eval analogue of
`zcp eval triage`. Plans reference `eval/results/audit-multi-weather-*.md` (the
triage output format), but those plans are archived/closed.

Per Agent A's call-graph: 2 cross-file callers from `cmd/zcp/eval.go` only.
Per Agent B's status check: no live invocation in plans, CI, or scripts since
the Apr 24-25 weather sweep concluded.

**Action**: Two-step decision required from user:

- **Option A** (aggressive): Delete `aggregate.go`, `aggregate_test.go`, and
  remove the `triage` case from `cmd/zcp/eval.go` + the supporting CLI plumbing
  (~750 LoC reduction). Era-1 surface fully gone.
- **Option B** (conservative): Keep aggregate.go but ARCHIVE the `zcp eval
  triage` CLI subcommand path (i.e. mark deprecated in help text, dead-code
  the dispatcher). Reassess in 30 days; if still no live invocation, delete.

Recommendation: **Option A**. The behavioral runner's `self-review.md` plus
ad-hoc Read+jq replaced the aggregator's role; keeping it is dead weight.

**Blast radius**: ~1 KLoC reduction if Option A. `cmd/zcp/eval.go` shrinks by
~60 LoC (the `triage`, `results`, `cleanup` subcommand handlers all share
era-1 lifecycle; `results` and `cleanup` are also probably dead — verify).

### Finding 3 — `internal/eval/scenarios/` (40 scenarios) overlaps heavily with `eval/behavioral/scenarios/` (26 scenarios)

**What**: Substring-grader scenarios under `internal/eval/scenarios/` cover
intents that the behavioral suite now owns:

| substring-grader scenario | behavioral counterpart | overlap |
|---|---|---|
| `bootstrap-classic-node-standard.md` | `classic-*.md` (6 of them) | full |
| `bootstrap-simple-node-api.md` | `classic-go-simple.md` (and intent matches) | full |
| `bootstrap-dev-only-bun-health.md` | `classic-bun-simple.md` + `classic-python-postgres-dev-only.md` | full |
| `bootstrap-git-init.md` | `delivery-git-push-actions-setup.md` | partial |
| `adopt-existing-laravel.md` | `existing-*.md` (3 of them) | full |
| `bootstrap-resume-interrupted.md` | (none) | unique |
| `develop-*` (10 scenarios) | partial behavioral coverage via greenfield+launch-production | partial |
| `weather-dashboard-*` (11 scenarios: bun, deno, dotnet, go, java, nextjs-ssr, nodejs, php-laravel, python, ruby, rust) | NONE — these are residue of the closed Apr 24-25 weather audit sweep | none |
| `bootstrap-recipe-collision*` (3) | covered by Aleš recipe tests | partial — recipe scope |
| `e2-build-fail-classification.md` | (none) | unique — but `TestClassifyDeployFailure_*` unit tests cover it |
| `export-deployed-service.md` | `export-buildfromgit-self-snapshot.md` | full |
| `verify-rendered-text.md` | `verify-subdomain-recovery-before-browser.md` | partial |
| `close-mode-git-push-setup.md` | `delivery-git-push-actions-setup.md` | full |
| `delivery-git-push-actions-e2e.md` | `delivery-git-push-actions-setup.md` | full |

**Why it matters**:

1. **Weather residue** (11 scenarios) — pure cruft from the closed Apr sweep,
   nobody is going to invoke `zcp eval scenario weather-dashboard-rust` again.
   Plans referencing the weather audit are all in `plans/archive/` or closed.
2. **Bootstrap/adopt/develop overlap** (~12 scenarios) — same intent as
   behavioral, but tested with the **substring grader that plans/test-suite-fixes
   §4.3 explicitly flags as producing false-greens**. We're paying maintenance
   tax on two parallel scenario corpora where one (behavioral) is the
   authoritative test of agent behavior and the other (substring grader)
   gives weaker signal.
3. **No live invocation path** — substring-grader scenarios have no
   `flow-eval.sh`-style wrapper. Manual `zcp eval scenario <id>` works but
   isn't called in any plan/CI since `c5ab2296` (May 8) which last touched
   their authoring contract.

**Action — three-phase**:

- **Phase 1** (delete): All 11 `weather-dashboard-*.md` scenarios + their
  fixtures in `internal/eval/scenarios/fixtures/`. Pure cleanup.
- **Phase 2** (consolidate): For each substring-grader scenario whose intent
  is fully covered by a behavioral counterpart, delete from
  `internal/eval/scenarios/`. Behavioral is the canonical author-facing layer
  for agent-behavior tests; substring grader is the false-green-prone layer.
- **Phase 3** (decide): For scenarios with no behavioral counterpart
  (`bootstrap-resume-interrupted`, weather-residue tier scenarios that pin
  specific platform contracts) — either port to behavioral, replace with unit
  tests in `internal/ops` / `internal/tools`, or accept the substring-grader
  signal and leave them. ~5-8 scenarios in this tier.

Net: `internal/eval/scenarios/` shrinks from 40 → ~5-8 (or zero if all are
ported).

**Blast radius**: ~30 scenario `.md` files + their `fixtures/` and `preseed/`
counterparts. If Phase 3 takes the whole runner offline,
`scenario_run.go::RunScenario` + `suite.go::RunAll` + the substring grader
become dead code candidates (Finding 4 below).

### Finding 4 — Substring-grader runner (`zcp eval scenario|run|suite`) is a dead-or-dying mid-layer

**What**: `Runner.RunScenario` (scenario_run.go, 233 LoC), `Suite.RunAll`
(suite.go, 167 LoC), and the supporting grading pipeline
(`grade.go::GradeWithProbe`, 187 LoC; with 24 callers but all within
internal/eval itself) form a complete single-shot scenario runner with
substring-based assertions.

**Why it matters**: Per `plans/test-suite-fixes-2026-04-25.md §4.3` (which is
itself stale, see Finding 7), the substring grader produces false-greens.
Behavioral runner (open-loop, agent observation + retrospective) replaced the
grading concept entirely — there is no automated grader on the behavioral
path because the user IS the grader. So `RunScenario`'s value is purely:

- The 40 scenarios under `internal/eval/scenarios/` (mostly redundant per
  Finding 3)
- The CLI ergonomics of `zcp eval scenario <id>` for fast iteration

**Action**: Decision-gated by Finding 3 outcome.

- If Finding 3 phase 3 ports/removes the last scenarios, then RunScenario +
  Suite + most of grade.go go dead. Net ~600-700 LoC reduction.
- If Phase 3 keeps a handful of scenarios for unit-test-like assertions,
  consider migrating those to actual unit tests (`internal/ops`,
  `internal/tools`) rather than scenario YAML — unit tests are faster, more
  precise, and don't need the grader machinery.

**Blast radius**: ~600-700 LoC if fully retired. Touches `cmd/zcp/eval.go`
significantly (loses 5 of its 8 subcommands).

### Finding 5 — `eval/` root is a closed-loop museum (~2.3k LoC dormant)

**What**: The entire `eval/AGENT_PROMPT.md` + `eval/run.sh` +
`eval/taxonomy.yaml` + `eval/scripts/{generate,score,extract-tool-calls,...}.py`
+ `eval/functional/` ecosystem is the closed-loop self-improving knowledge
agent that predates and was replaced by the behavioral approach. Per
`eval/behavioral/README.md` line 24-26: "Not the self-improving knowledge-eval
loop in `eval/AGENT_PROMPT.md`. That loop is closed-loop ... This is
open-loop."

**Why it matters**:

- 2.3 KLoC of Python + shell + Markdown that nobody invokes.
- 60 `eval/results/audit-multi-weather-*` dumps in working tree (gitignored
  but on disk).
- `eval/run.sh` last edited Feb-Mar 2026; no commits since.
- `eval/AGENT_PROMPT.md` last edited May 3, but only via the `zcp → zcpx
  rename` commit (cosmetic); no semantic update since the closed-loop strategy
  was abandoned.
- The `taxonomy.yaml` is referenced from 9 archived plans and the dormant
  `AGENT_PROMPT.md` — zero live consumers.

**Why this is real cruft** (not paranoia): per Agent B, the only "live" edge
into `eval/` is `eval/scripts/build-deploy.sh` (called by
`eval/behavioral/flow-eval.sh`). Everything else in `eval/scripts/`,
`eval/functional/`, `eval/results/`, and `eval/AGENT_PROMPT.md` is unreached
by any current testing strategy.

**Action**:

1. Delete `eval/AGENT_PROMPT.md`, `eval/run.sh`, `eval/taxonomy.yaml`. Era-1
   closed-loop is dead.
2. Delete `eval/functional/{prompt-dev.md,run-dev.sh,cleanup-dev.sh}`.
3. Delete weather sweep scripts (`aggregate-weather-audit.py`,
   `autonomous-weather-sweep.py`, `run-multi-runtime-weather.sh`,
   `run-batch3-followup.sh`) and Feb-era Python (`generate.py`, `score.py`,
   `extract-tool-calls.py`, `analyze_bash_latency.py`, `version_metrics.sh`,
   `run-scenario.sh`).
4. **Keep** `eval/scripts/build-deploy.sh` (flow-eval needs it) and
   `eval/scripts/cleanup.sh` (referenced from `internal/eval/cleanup.go` —
   verify it's still a live reference before deleting).
5. Decide `eval/scripts/timeline.py` (only referenced from
   `docs/zcprecipator2/*` which itself is Aleš's archived recipe runbook). If
   recipe team wants it, leave; else delete.
6. `git rm -r eval/results/*` working-tree (already gitignored).

Net `eval/` collapses from 9 files + 3 subdirs + 60 results dumps to:
`eval/behavioral/` + `eval/scripts/build-deploy.sh` (+ maybe cleanup.sh).

**Blast radius**: ~2.3 KLoC removed. No production impact. `flow-eval.sh`'s
sole upstream dependency (`build-deploy.sh`) preserved.

### Finding 6 — `plans/test-suite-fixes-2026-04-25.md` is stale and misleading

**What**: 18-day-old test-suite audit plan with **zero phases shipped**,
authored against repo state that no longer exists (its §5.1 cites a
"5-parallel-tests" state and several files that have been deleted, refactored,
or replaced). Codex v3 review reconciliation embedded in the plan body adds
further confusion (multiple "DROPPED" / "DONE" markers without git evidence
backing them).

**Why it matters**: This document keeps surfacing in conversations as a
roadmap. Phase 4.2b ("scenarios_test.go migration to atom-ID assertions") was
the only Codex-recommended starting point, and it was never executed. Phase
4.3 ("eval scenarios actually executed") is the source of the
"substring-grader false-greens" claim that Findings 3 + 4 build on — but the
audit plan itself never proved it; it was theoretical.

**Action**: Archive — `git mv plans/test-suite-fixes-2026-04-25.md
plans/archive/test-suite-fixes-2026-04-25.md`. The valid concerns from it
(substring-grader weakness, eval scenarios not executed end-to-end) are now
captured in Findings 3 + 4 of this audit with concrete proposed actions.

Open question worth surfacing back to the audit-plan author (likely Karel):
phase 4.3's intent was to make `internal/eval/*_test.go` actually execute one
scenario end-to-end, not just verify parseability. Behavioral runner moots
this for the C4 layer, but for the substring grader layer (if Finding 4 keeps
it alive), the gap remains.

### Finding 7 — `plans/backlog/e2e-bootstrap-helper-two-phase-wiring.md` Part B is outstanding

**What**: Part A landed in commit `609b1227` (mechanical single-phase →
two-phase bootstrap helper rewrites across 8 e2e test files). Part B (live
full-suite verification of the rewrites against eval-zcp) was deferred.

**Why it matters**: 8 bootstrap-touching e2e tests have un-verified
Phase-2-response-shape parsing (e.g. `bootstrapProgress` unmarshal expectations
may have drifted from what Phase 2 actually returns). Build + vet are clean,
but the failure mode is "test runs against live platform and panics on
JSON parse" — only catchable by actually running them.

**Action**: Run `make e2e-zcp` against eval-zcp (full suite, not -fast),
review failures one-by-one, fix and verify. When green: `git rm
plans/backlog/e2e-bootstrap-helper-two-phase-wiring.md`. Estimated 1-2 hours
of work.

**Blast radius**: bootstrap_workflow_test, deploy_local_test (4 sites),
build_logs_test, deploy_prepare_fail_test, import_provenance_test,
laravel_recipe_test, verify_test — all rewrites are mechanical, response-shape
issues (if any) will surface as unmarshal failures.

### Finding 8 — Integration tests look healthy, low overlap with eval surface

**What**: 7 files (~2.4k LoC) in `integration/` — bootstrap_conductor,
bootstrap_realistic, deploy_flow, export, multi_tool, orphan_meta + main.

**Verdict**: KEEP as-is. Each test covers a layer that neither e2e (live
platform) nor behavioral (full agent) gives directly: deterministic
mock-platform contract tests for the orchestration layer. They are tight,
focused, and complement rather than duplicate the other layers.

**Marginal note**: `bootstrap_conductor_test.go` and
`bootstrap_realistic_test.go` together overlap somewhat with behavioral
`classic-*` scenarios in *intent* but not in *signal* — integration tests pin
internal handler invariants (orphan meta, status recovery, mock state
machine), behavioral tests pin actual-agent decision-making. They're peer
coverage, not overlapping.

---

## Recipe scope checkpoint — what's actually Aleš's per 14-day git evidence

### What the evidence says

Per CLAUDE.local.md the recipe scope is `internal/recipe/`,
`internal/tools/workflow_recipe.go`, `internal/content/workflows/recipe/`,
`docs/zcprecipator*/`. But the rule is a *general directive*, not a static
list — to use it as a working signal we need to know what Aleš **actually
does**, not what's nominally in his bucket.

Pulled commit history for the last 14 days (`since 2026-04-29`). Authors:

| Author | Commits | Where the time goes |
|---|---|---|
| `Ales Rechtorik <foxxcz@gmail.com>` | **156** | 703 file-touches in `internal/recipe/`, 42 in `cmd/zcp-recipe-sim/`, 26 in `docs/zcprecipator3/`, 14 in `internal/tools/`, 12 in `internal/knowledge/`, 9 in `internal/content/`, 4 in `cmd/zcp-replay-content/`, 2 in `cmd/zcp-recipe-patch/`. |
| `krls2020 <mail@karelhemza.cz>` | 220 | Everything else (workflows, deploy, eval, e2e, bootstrap, content surfaces). |

**Aleš's 14-day commits to `eval/`, `internal/eval/`, `cmd/zcp/eval*.go`,
`e2e/`: ZERO.** Not one file. His recipe-validation loop goes through:

```
cmd/zcp-recipe-sim emit       — stage scaffold + dispatch prompts
                  emit-finalize — finalize sub-agent prompt
                  emit-refinement — refinement sub-agent prompt
                  stitch        — assemble fragments
                  validate      — slot-shape refusals
```

… driven against `docs/zcprecipator3/runs/N/` structured runs and
`docs/zcprecipator3/simulations/N/` shadow runs. The `zcp-recipe-sim`
binary is cited in 7+ Aleš plan docs (`run-32-*`, `run-33-*`,
`multi-file-briefs.md`). This is his canonical validation harness.

**`zcp eval create` is cited nowhere in live plans.** Three hits exist in
`docs/zrecipator-archive/` (`recipe-via-zcp-analysis.md`,
`recipe-system-summary.md`, `implementation-plan.md`) — all archived
"should we route recipes through ZCP?" exploration documents. Zero active
runbooks, zero CI workflows, zero `run-NN-*` plans reference it. The CLI
path exists in `cmd/zcp/eval.go` but is unreached from any live workflow.

### Authorship of every recipe-touching artifact in the eval surface

`git log -1 --pretty='%h %an' -- <file>` for each:

| Artifact | Last author | Last edit | Aleš commits ever | Reality |
|---|---|---|---|---|
| `internal/eval/recipe_create.go` | krls2020 | 2026-04-30 | 2 in April (4/3, 4/13) — both schema-related, none in 14d window | **MINE** |
| `cmd/zcp/eval.go::runEvalCreate/CreateSuite` | krls2020 | (lives in eval.go) | none in 14d | **MINE** |
| `internal/eval/prompt.go` | krls2020 | 2026-04-04 | none in 14d | **MINE** |
| `internal/eval/markdown.go` | krls2020 | 2026-03-25 | none in 14d | **MINE** |
| `eval/behavioral/scenarios/recipe-laravel-minimal-standard.md` | krls2020 | 2026-05-02 | none | **MINE** |
| `eval/behavioral/scenarios/recipe-laravel-showcase-fullstack.md` | krls2020 | 2026-05-02 | none | **MINE** |
| `eval/behavioral/scenarios/recipe-nestjs-minimal-standard.md` | krls2020 | 2026-05-02 | none | **MINE** |
| `eval/behavioral/scenarios/recipe-nextjs-ssr-frontend-standard.md` | krls2020 | 2026-05-02 | none | **MINE** |
| `eval/behavioral/scenarios-local/recipe-nodejs-hello-world.md` | krls2020 | 2026-05-08 | none | **MINE** |
| `internal/eval/scenarios/bootstrap-recipe-*.md` (3) | krls2020 | various | none | **MINE** |
| `internal/eval/scenarios/adopt-existing-laravel.md` | krls2020 | various | none | **MINE** |
| `internal/eval/scenarios/greenfield-laravel-weather.md` | krls2020 | various | none | **MINE** |
| `internal/eval/scenarios/weather-dashboard-{php-laravel,nextjs-ssr}.md` | krls2020 | various | none | **MINE** |

**Verdict**: every recipe-touching file in the eval surface is mine. Aleš
neither edits them nor runs them. The five `recipe-*` behavioral scenarios
were actually executed only via my flow-eval sessions
(`eval/behavioral/runs/` shows `recipe-laravel-minimal-standard` ran 7
times, `recipe-laravel-showcase-fullstack` 4-5 times, `recipe-nestjs` 2x —
all in suites I drove).

### Revised scope rule (working definition)

**Aleš's hands-off scope** (per evidence + CLAUDE.local.md):

- `internal/recipe/**`
- `internal/tools/workflow_recipe.go` + `workflow_recipe_test.go`
- `internal/content/workflows/recipe/**`
- `cmd/zcp-recipe-sim/**`, `cmd/zcp-replay-content/**`, `cmd/zcp-recipe-patch/**`
- `docs/zcprecipator2/**`, `docs/zcprecipator3/**`, `docs/zrecipator-archive/**`
- `plans/run-NN-*.md` (his iteration validation/forensics workflow)

**Everything else** (including all of `eval/`, `internal/eval/`,
`cmd/zcp/eval*.go`, `e2e/`) is mine. Recipe-touching content in those
directories represents *my testing of his tool's agent-facing contract*,
not his testing — so the decision authority on whether to keep/delete is
mine alone.

### Concrete consequences for each finding

**Finding 1 (instruction_eval/instruction_variants zombies)** — already
mine, unchanged. Plus promote `recipe_create.go` + `runEvalCreate` +
`runEvalCreateSuite` (~344 LoC) into the same delete batch. They're a
third zombie tree: 0 callers outside their own CLI handler, 0 plan
references, last touched 4/30, Aleš has his own simulator and never used
this path. Net Finding 1 grows from ~514 LoC to **~858 LoC**.

**Finding 3 (substring-grader scenario corpus)** — recipe-touching
scenarios listed above are mine, no Aleš veto needed. Full Phase 1+2+3
cleanup proceeds as written. The seven recipe-touching scenarios can be:
- Deleted (they're substring-grader era, behavioral suite has analogues
  in `recipe-laravel-*` scenarios).
- Or ported to behavioral if the intent is unique (`bootstrap-recipe-collision-rename`
  / `bootstrap-recipe-collision` may have specific collision-recovery
  intent that no current behavioral scenario tests).

**Finding 4 (substring-grader retirement)** — `prompt.go` + `markdown.go`
+ `RecipeMetadata` only stay alive if the `recipe-*` behavioral scenarios
(in `eval/behavioral/scenarios/`) stay. Question for Karel: do the
behavioral recipe-* scenarios add testing value beyond what they
demonstrate? They verify the agent **calls `zerops_recipe` correctly** in
response to a recipe-stack prompt, which is a contract on Aleš's tool.
Recommendation: **keep** the five behavioral `recipe-*` scenarios as
sentinel coverage of the agent's `zerops_recipe`-routing decision, but
their existence means `prompt.go`/`markdown.go` stay too.

**Finding 5 (eval scripts in Aleš docs)** — three scripts cited only in
archived recipe docs (`docs/zcprecipator2/*`, `docs/zrecipator-archive/*`):
- `eval/scripts/timeline.py` — 3 archived refs
- `eval/scripts/analyze_bash_latency.py` — 3 archived refs
- `eval/scripts/version_metrics.sh` — 1 archived ref

These are weak signal — archived docs may never be re-read, but if Aleš
ever does forensics on a v35 run he might want them. **Lowest-risk
approach**: keep them. They're 3 files, ~500 LoC total, almost zero
maintenance burden. The 9 other scripts in `eval/scripts/` (Finding 5)
remain safe-to-delete.

### Revised summary

Recipe-scope impact on cleanup plan is **smaller than I claimed in v1**:

- **Findings 1, 3, 4, 6, 7, 8**: Zero Aleš-scope friction. Act unilaterally.
- **Finding 2**: `aggregate.go` + `runEvalTriage` — mine, mine, unchanged.
- **Finding 5**: Spare 3 scripts (timeline / analyze_bash_latency /
  version_metrics) as low-cost insurance for archived runbooks.

Net: ~344 additional LoC become safe-to-delete (recipe_create.go + CLI
dispatchers), and we don't need to ping Aleš at all unless we kill the
five `recipe-*` behavioral scenarios — which is the one decision worth
asking him about, as a courtesy ("FYI I'm removing flow-eval coverage of
your tool's agent contract, OK?").

---

## What I'm explicitly not touching

- **e2e suite**: Just cleaned (40→27 in commit `609b1227`). Outstanding only
  Finding 7 (Part B live verification).
- **All Ring 1 artifacts above**: recipe_create.go, runEvalCreate/Suite, all
  `recipe-*.md` scenarios in behavioral (+ scenarios-local).
- **`internal/eval/scenarios/` fixtures + preseed scripts**: Only touch via
  scenario `.md` deletions (Findings 3+4). The fixture YAML and preseed
  scripts get cleaned up with their scenarios.

---

## Recommended action order

Priority = leverage (LoC removed × clarity gained) ÷ risk. Each step ends in a
verifiable state.

1. **Finding 1** — delete `instruction_eval.go` + `instruction_variants.go` +
   their test files. **~514 LoC, zero risk** (literally no callers). 30 min.
2. **Finding 5** — delete `eval/AGENT_PROMPT.md` + `eval/run.sh` +
   `eval/taxonomy.yaml` + `eval/functional/` + 9 of the 13
   `eval/scripts/*.{py,sh}`. **~2.3 KLoC, zero production risk** (nothing live
   depends on them). 1-2 hours including verifying `cleanup.sh` reference and
   `timeline.py` decision.
3. **Finding 3, Phase 1** — delete 11 `weather-dashboard-*.md` scenarios +
   their fixtures. **Zero risk** (weather era closed). 30 min.
4. **Finding 6** — `git mv` test-suite-fixes plan to archive. **5 min**.
5. **Finding 7** — Part B live verification of bootstrap two-phase helper
   rewrites against eval-zcp. **1-2 hours**, possible test-fix follow-ups.
6. **Finding 3, Phase 2 + 3** — consolidate remaining substring-grader
   scenarios against behavioral. **2-4 hours** depending on porting decisions.
7. **Finding 2 + 4** — decide on `zcp eval triage` + substring-grader
   retirement. **Needs user signoff first** because it changes the CLI
   surface. If green-lit, ~1 KLoC reduction in one commit.

**Net potential cleanup**: ~4-4.5 KLoC removed across 50-60 files, eval/ and
internal/eval/ collapsed to a single coherent behavioral-era surface. The
codebase shifts from "three eras stacked atop each other" to "one canonical
behavioral testing strategy + supporting infrastructure."

## What stays after full cleanup

```
internal/eval/                       (was 20 .go files)
├── runner.go, scenario.go, behavioral_run.go, behavioral_assets.go
├── cleanup.go, seed.go, local_setup.go
├── grade.go, probe.go, extract.go, eval.go, prompt.go, markdown.go
├── usersim.go
├── recipe_create.go                 (Aleš scope adjacent)
├── retrospective_prompts/           (1 prompt)
└── scenarios/                       (5-8 scenarios after consolidation)

eval/                                (was 9 files + 3 subdirs + 60 results)
├── behavioral/                      (active; flow-eval ecosystem)
│   ├── flow-eval.sh
│   ├── README.md
│   ├── scenarios/                   (26)
│   ├── scenarios-local/             (5)
│   └── audits/
└── scripts/
    └── build-deploy.sh              (+ maybe cleanup.sh)
```

That's a coherent, single-era surface.
