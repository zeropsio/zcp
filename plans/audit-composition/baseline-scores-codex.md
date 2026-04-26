# Codex round: composition baseline scoring (independent pass)

Round type: CORPUS-SCAN per §10.1 P0 row 2
Reviewer: Codex (independent, fresh agent)
Read order (declared):
1. `plans/audit-composition/rendered-fixtures/develop_first_deploy_standard_container.md`
2. `plans/audit-composition/rendered-fixtures/develop_first_deploy_implicit_webserver_standard.md`
3. `plans/audit-composition/rendered-fixtures/develop_first_deploy_two_runtime_pairs_standard.md`
4. `plans/audit-composition/rendered-fixtures/develop_push_dev_dev_container.md`
5. `plans/audit-composition/rendered-fixtures/develop_simple_deployed_container.md`

Did NOT read executor's `baseline-scores.md` until after final scoring: YES

> **Artifact write protocol note (carries over from prior rounds).**
> Codex sandbox blocks artifact writes; this artifact was reconstructed
> verbatim from Codex's text response. The first dispatch with
> `run_in_background=true` returned a "delegated to background"
> placeholder; the synchronous re-run produced the actual analysis.

## Score table

| Fixture | Coherence | Density | Redundancy | Coverage-gap | Task-relevance |
|---|---|---|---|---|---|
| develop_first_deploy_standard_container | 1 | 3 | 1 | 3 | 3 |
| develop_first_deploy_implicit_webserver_standard | 1 | 3 | 1 | 2 | 3 |
| develop_first_deploy_two_runtime_pairs_standard | 1 | 2 | 1 | 2 | 3 |
| develop_push_dev_dev_container | 1 | 3 | 1 | 3 | 2 |
| develop_simple_deployed_container | 1 | 3 | 1 | 3 | 2 |

## Per-fixture qualitative justification

### develop_first_deploy_standard_container

Most-likely tasks: scaffold zerops.yaml + app code, first deploy dev then stage, diagnose build/runtime/HTTP.

- **Coherence 1**: Competing stage-deploy instructions (direct `zerops_deploy targetService="appstage"` vs cross-promote from dev) actively contradict each other; anchor: "sections actively contradict each other."
- **Density 3**: Many operational facts but long defensive sections and broad tables; anchor: "2.0-2.9 facts/KB, acceptable but rewriteable."
- **Redundancy 1**: `[.]` self-deploy, env var deployment, auto-close, verify, HTTP diagnostics, and dev-server restart recur across seven-plus atoms; anchor: "7+ restated facts."
- **Coverage-gap 3**: Most likely next actions present, but stage first-deploy vs promotion is an authoritative ambiguity; anchor: "1 gap on a likely next-action."
- **Task-relevance 3**: Half to three-quarters relevant; broad apiMeta/env/platform/dev-server/auto-close material is partial noise; anchor: "50-74% relevant."

### develop_first_deploy_implicit_webserver_standard

Most-likely tasks: scaffold PHP nginx zerops.yaml without `run.start`/`run.ports`, deploy dev, handle webserver/log diagnostics.

- **Coherence 1**: Implicit-webserver atom says "do not SSH to start a server," generic runtime atoms say bind `0.0.0.0` and set `run.start`; anchor: "actively contradict each other."
- **Density 3**: Fact-heavy but verbose; anchor: "2.0-2.9 facts/KB, acceptable but rewriteable."
- **Redundancy 1**: DeployFiles, env vars, auto-close, verify, platform rules, diagnostics, deploy flow restated more than seven times; anchor: "7+ restated facts."
- **Coverage-gap 2**: Likely-action conflict (dynamic dev-server guidance leaking into implicit-webserver flow) plus multiple lower-probability ambiguities; anchor: "likely-action gap + multiple low-probability gaps."
- **Task-relevance 3**: Implicit-webserver and first-deploy atoms useful, but dynamic/generic deploy-mode material is noise; anchor: "50-74% relevant."

### develop_first_deploy_two_runtime_pairs_standard

Most-likely tasks: scaffold multi-service zerops.yaml for app+api pairs, deploy and verify both dev services, promote both to stage.

- **Coherence 1**: Identical headings with hostname substitution plus direct-stage-deploy vs cross-promote conflict repeated for both pairs; anchor: "sections actively contradict each other."
- **Density 2**: Whole per-service sections duplicated, pushing useful facts per KB down; anchor: "1.0-1.9 facts/KB, significant prose verbosity."
- **Redundancy 1**: Two duplicate dynamic dev-server atoms and two duplicate stage-promotion atoms plus common deploy/env/verify repetition; anchor: "7+ restated facts."
- **Coverage-gap 2**: Commands named but direct-stage vs dev-to-stage promotion conflict for both pairs is unresolved; anchor: "likely-action gap + multiple low-probability gaps."
- **Task-relevance 3**: First-deploy topics present, but double-rendered per-service content and defensive atoms leave only "50-74% relevant" effective value.

### develop_push_dev_dev_container

Most-likely tasks: edit code on `/var/www/appdev/`, restart or start dev server, diagnose dev-server or HTTP verification failures.

- **Coherence 1**: "Edit → deploy → verify" instruction competes with "restart only, no redeploy for code-only changes"; anchor: "actively contradict each other."
- **Density 3**: Many concrete commands and decision rules; anchor: "2.0-2.9 facts/KB, acceptable but rewriteable."
- **Redundancy 1**: Dev-server status/start/restart/logs, deployFiles `[.]`, deploy strategy, auto-close, verify, and platform rules restated across many atoms; anchor: "7+ restated facts."
- **Coverage-gap 3**: Edit-loop path unclear because restart-only and deploy-required flows compete without clear authority; anchor: "1 gap on a likely next-action."
- **Task-relevance 2**: Much of fixture is cross-deploy, mode-expansion, apiMeta, env-channel, and generic platform material rather than the edit-loop itself; anchor: "25-49% relevant."

### develop_simple_deployed_container

Most-likely tasks: edit Go simple-mode app, redeploy single service, diagnose deploy or HTTP verification failures.

- **Coherence 1**: Simple-mode guidance (platform owns lifecycle, no manual server start) contradicts detailed `zerops_dev_server` action atoms that still render; anchor: "actively contradict each other."
- **Density 3**: Command-rich but large irrelevant defensive blocks; anchor: "2.0-2.9 facts/KB, acceptable but rewriteable."
- **Redundancy 1**: DeployFiles, deploy/verify, dev-server, platform rules, auto-close, and strategy awareness repeat enough; anchor: "7+ restated facts."
- **Coverage-gap 3**: Deploy/verify path present, but `setup="prod"` plus dev-server noise creates likely-action ambiguity for simple mode; anchor: "1 gap on a likely next-action."
- **Task-relevance 2**: Most effective content is simple checklist/workflow/close; many atoms are defensive or irrelevant; anchor: "25-49% relevant."

## Cross-validation against executor scores

| Fixture | Dimension | Executor | Codex | Δ | Rubric anchor invoked |
|---|---|---:|---:|---:|---|
| develop_first_deploy_standard_container | Coherence | 3 | 1 | 2 | "actively contradict each other" (1) vs executor reading as "major inconsistencies that misdirect" (3) |
| develop_first_deploy_standard_container | Redundancy | 2 | 1 | 1 | 7+ restated facts |
| develop_first_deploy_standard_container | Coverage-gap | 5 | 3 | 2 | Executor: full coverage. Codex: 1 gap on likely next-action (stage direct vs promote conflict) |
| develop_first_deploy_standard_container | Task-relevance | 4 | 3 | 1 | Executor 75-90% vs Codex 50-74% |
| develop_first_deploy_implicit_webserver_standard | Coherence | 3 | 1 | 2 | Same contradiction-vs-misdirection ambiguity |
| develop_first_deploy_implicit_webserver_standard | Redundancy | 2 | 1 | 1 | 7+ restated facts |
| develop_first_deploy_implicit_webserver_standard | Coverage-gap | 5 | 2 | 3 | Executor: full. Codex: likely-action gap + multiple low-probability gaps |
| develop_first_deploy_implicit_webserver_standard | Task-relevance | 4 | 3 | 1 | Executor 75-90% vs Codex 50-74% |
| develop_first_deploy_two_runtime_pairs_standard | Coherence | 2 | 1 | 1 | Both flagged; Codex read more severe |
| develop_first_deploy_two_runtime_pairs_standard | Density | 3 | 2 | 1 | Per-service duplication pushes down facts/KB |
| develop_first_deploy_two_runtime_pairs_standard | Coverage-gap | 5 | 2 | 3 | Same direct-vs-promote conflict applied twice |
| develop_first_deploy_two_runtime_pairs_standard | Task-relevance | 4 | 3 | 1 | Double-render reduces effective relevance |
| develop_push_dev_dev_container | Coherence | 3 | 1 | 2 | Edit→deploy vs restart-only contradiction |
| develop_push_dev_dev_container | Redundancy | 2 | 1 | 1 | 7+ restated facts |
| develop_push_dev_dev_container | Coverage-gap | 5 | 3 | 2 | Executor: full. Codex: edit-loop ambiguity is a likely-action gap |
| develop_simple_deployed_container | Coherence | 2 | 1 | 1 | Both flagged; Codex more severe |
| develop_simple_deployed_container | Redundancy | 2 | 1 | 1 | 7+ restated facts |
| develop_simple_deployed_container | Coverage-gap | 5 | 3 | 2 | Executor: full. Codex: simple-mode dev-server noise is a likely-action gap |
| develop_simple_deployed_container | Task-relevance | 1 | 2 | 1 | Executor <25% vs Codex 25-49% |

## Disagreements ≥ 2 (rubric refinement triggers per §6.6 L4)

Three pairs of dimensions each have at least one fixture with delta ≥ 2:

### Coherence — "contradicts" vs "misdirects"

The rubric score-1 anchor says "sections actively contradict each
other" and score-3 says "major inconsistencies that could misdirect."
Both apply to competing next-action commands (e.g. direct stage
deploy vs cross-promote), so scorers pick different levels.

**Proposed refinement:** "Score 1 when two atoms recommend
mutually exclusive tool calls for the same likely next action.
Score 3 when one atom contradicts the other but only in framing
or strategy rather than in the tool call itself."

### Coverage-gap — "tool present" vs "authoritative"

Executor treated "tool and arg shape rendered" as sufficient for
full coverage (score 5). Codex treated "competing commands for
same action = gap" as score 3 or 2.

**Proposed refinement:** "Score 5 only when each likely next
action is covered by exactly one unambiguous recommendation. If
two competing recommendations exist for the same likely action,
cap at 3."

### Redundancy — exact restatement vs paraphrase/substitution

Executor appeared to require nearer-verbatim restatement for
score 1. Codex counted paraphrased restates and per-service
hostname-substituted copies.

**Proposed refinement:** "Score 1 when 7+ distinct facts
(including paraphrases and hostname-substituted copies) recur
across different atoms. Score 2 when 4-6 such repeats exist."

## Phase 7 axis-tightening targets — agreement check

- **develop-mode-expansion**: agree — renders in edit-loop/simple contexts where expansion is not a likely task; lowers task-relevance visibly.
- **develop-dev-server-triage**: agree — must be constrained to dynamic dev-mode; simple/stage/implicit/static envelopes should not render manual dev-server lifecycle steps.
- **develop-dev-server-reason-codes**: agree — same axis as triage; reason-code tables are noise when `zerops_dev_server` is not a likely tool.
- **develop-static-workflow**: agree to investigate; these five fixtures had no static envelopes, so cannot confirm from observation alone.
- **develop-strategy-awareness**: agree — renders broadly into already-strategized edit loops where switching strategy is not a likely task.

## Aggregate observations

- Agreement on "corpus is over-included not under-included": **YES**
- Agreement on "two-pair per-service double-render is a coherence failure": **YES**
- Agreement on "task-relevance disparity is the Phase 7 headline": **YES**
- Counter-narratives or new findings:
  1. **Competing next-action commands** (direct stage deploy vs dev-to-stage promotion; edit→deploy vs restart-only) are a stronger problem than general inconsistency framing captured; they functionally prevent an agent from choosing the right call. **NEW Phase 1+ finding.**
  2. **Simple-mode and dev-mode fixtures need a sharper conditional render gate**: "if no dynamic runtime, suppress all `zerops_dev_server` atoms."
  3. **`develop-api-error-meta` renders early and broadly**; in non-error edit-loop envelopes it is pure noise and may deserve its own tightening candidate. **NEW Phase 7 axis-tighten target.**
  4. **Redundancy is more severe than score-2 captures**; paraphrase and per-service duplication accumulate to 7+ conceptually distinct restated facts even without verbatim repetition.

## Verdict

Scores are within ±1 of executor across all dimensions: **NO**

Rubric refinement required before treating scores as ground
truth. Three anchors need tightening: Coherence (contradict vs
misdirect), Coverage-gap (tool-present vs authoritative),
Redundancy (exact vs paraphrase/substitution counting).
