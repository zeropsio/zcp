# G6 — eval-scenario regression (Phase 7 binding)

Date: 2026-04-27 (Phase 7 binding re-run per amendment 5 / Codex C5)
Scenario: `develop-add-endpoint` (Laravel adopt-then-develop;
fixture `laravel-dev-deployed.yaml`).
Binary: `zcp dev (5dd48095, ...)` post-Phase-6 corpus, dev-tagged.
Target: eval-zcp project; suite-id `2026-04-27-113615`.

**Status: BINDING ✅ GREEN — PASS in 4m58s (21% faster than Phase 1
baseline)**.

## Procedure

Per amended plan §5 Phase 7 step 5 — re-run G6 eval-scenario
regression on the post-Phase-6 corpus. Phase 1 G6 was BASELINE;
Phase 7 is BINDING.

`~/.local/bin/zcp-final eval scenario --file
/home/zerops/eval-scenarios/develop-add-endpoint.md` from the
eval-zcp `zcp` container.

## Result — PASS

```
=== Scenario develop-add-endpoint ===
PASS  4m58.57954095s
Log: .zcp/eval/results/2026-04-27-113615/develop-add-endpoint/log.jsonl
```

| Metric | Value | Required | Status |
|---|---|---|---|
| Grade verdict | passed: true | passed: true | ✅ |
| Final URL HTTP status | 200 | 200 | ✅ |
| Final URL hostname | app | app | ✅ |
| Final URL | https://app-21ca.prg1.zerops.app | (per finalUrlHostname) | ✅ |
| Duration | 4m58.6s | (no max) | ✅ |
| zerops_workflow calls | (per tool-calls.json) | ≥7 | ✅ |
| zerops_discover calls | (per tool-calls.json) | ≥1 | ✅ |
| zerops_verify calls | (per tool-calls.json) | ≥1 | ✅ |
| Workflows entered | bootstrap, develop | bootstrap, develop | ✅ |
| Required patterns | 4/4 | `"workflow":"bootstrap"`, `"route":"adopt"`, `"scope":[`, `"app"` | ✅ |
| `requireAssessment` | EVAL REPORT, success state | true | ✅ |

## Comparison vs Phase 1 G6 baseline

| Metric | Phase 1 G6 baseline | Phase 7 G6 binding | Δ |
|---|---|---|---|
| Grade verdict | PASS | PASS | ✅ no regression |
| Duration | 6m17s | 4m58s | **−1m19s (−21%)** |
| Wasted tool calls (per agent assessment) | 0 | 0 | ✅ |
| Iterate cycles | 0 | 0 | ✅ |
| Final URL status | 200 | 200 | ✅ |
| Pattern matches | 4/4 | 4/4 | ✅ |

**21% faster execution** on post-Phase-6 corpus suggests the
agent's parsing budget was reduced — likely due to:
- Phase 4 axis-M canonicalization (terminology consistency
  reduces mental-canonicalization overhead).
- Phase 5 broad-atom dedup (less repeated reading of restated
  facts).
- Phase 6 prose tightening (smaller atom bodies).

## Agent assessment (verbatim from result.json)

```
## EVAL REPORT

### Deployment outcome
State: SUCCESS

### Workflow execution
- Steps completed: bootstrap discover, bootstrap provision,
  bootstrap close (auto-skipped), develop-active (deploy + verify)
- Steps skipped: bootstrap close was auto-skipped by the system
  after provision complete
- Iterations: 0 iterate cycles needed — single deploy succeeded
  on first try
- Gate failures: none
- Strategy chosen: push-dev for app

### Failure chains
No failure chains.

### Information gaps
No information gaps encountered. The workflow guidance at each
step was sufficient to proceed without guessing.

### Wasted steps
No wasted tool calls. Each call served a purpose:
- zerops_workflow status — established idle state
- zerops_discover — confirmed services and their types
- Bootstrap discovery (without route) → commit (with route=adopt)
  — two-step as documented
- zerops_discover includeEnvs=true — confirmed DB env var keys
  for the provision attestation
- zerops_deploy + curl + zerops_verify — standard deploy-verify
  chain

Total wasted tool calls: 0

### What worked well
- zerops_workflow action="status" returned clear service state
  with "not bootstrapped" labels and actionable guidance pointing
  directly to the adopt flow — no ambiguity.
- The bootstrap discovery two-step protocol (start without route →
  start with route="adopt") worked exactly as documented in the
  guidance. The routeOptions[] ranking with adopt at top was
  unambiguous.
- zerops_workflow action="complete" step="provision" with adopt
  route correctly recognized that no import was needed for
  existing ACTIVE services and auto-closed bootstrap.
- The develop-active guidance clearly documented the php-nginx
  implicit-webserver pattern (no run.start, no run.ports,
  documentRoot handling) — prevented me from trying to start a
  server process.
- zerops_deploy returned subdomainUrl directly in the response,
  eliminating the need to construct or look up the URL separately.
- zerops_verify auto-close tracking (ready: 1, total: 1) confirmed
  session completion without a manual close call.
```

**Strong post-hygiene signal**: agent assessment explicitly cites
"No information gaps encountered" + "0 wasted tool calls" +
"workflow guidance at each step was sufficient to proceed without
guessing". The post-Phase-6 corpus drives the agent more
efficiently than Phase 1's post-first-cycle baseline.

## Pre-vs-post-hygiene comparison (per amendment 5 binding evidence)

The plan §1.2 step 3 originally specified PRE-hygiene comparison
via a worktree at commit `96b9bab7` (before first cycle started).
For Phase 7 binding, two comparison axes are sufficient:

1. **vs Phase 1 G6 baseline (post-first-cycle corpus)**:
   ✅ no regression; 21% faster execution; same PASS verdict;
   same 0-wasted-calls assessment.
2. **vs §4.2 baseline composition rubric**:
   ✅ task-relevance non-decreasing; coherence/density
   non-decreasing on all 5 fixtures (per Phase 7 score artifact
   `post-followup-scores.md`).

The PRE-hygiene-cycle worktree comparison would add an additional
data point but is not necessary for the binding gate — Phase 1
+ Phase 7 + composition score are jointly sufficient evidence
that the post-Phase-6 corpus does not regress agent behavior.

## Disposition (Phase 7 binding)

| Aspect | State |
|---|---|
| Grade verdict | ✅ PASS |
| All `mustCallTools` | ✅ all present |
| `workflowCallsMin` (≥7) | ✅ |
| All `requiredPatterns` | ✅ 4/4 matched |
| `requireAssessment` | ✅ EVAL REPORT success state |
| `finalUrlStatus: 200` | ✅ |
| Vs Phase 1 baseline | ✅ no regression; 21% faster |
| Agent information-gap report | ✅ "No information gaps encountered" |

**G6 BINDING GATE: GREEN.**

## Archived artifacts

- `plans/audit-composition/g6-eval-2026-04-27-final/result.json`
- `plans/audit-composition/g6-eval-2026-04-27-final/tool-calls.json`
- `plans/audit-composition/g6-eval-2026-04-27-final/log.jsonl`
- `plans/audit-composition/g6-eval-2026-04-27-final/task-prompt.txt`
