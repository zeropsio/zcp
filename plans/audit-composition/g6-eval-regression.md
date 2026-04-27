# G6 — eval-scenario regression (Phase 1 baseline)

Date: 2026-04-27
Phase: Phase 1 (per `plans/atom-corpus-hygiene-followup-2026-04-27.md`
§5 Phase 1 §1.2)
Scenario: `develop-add-endpoint` (Laravel adopt-then-develop;
fixture `laravel-dev-deployed.yaml`).
Binary: `zcp dev (a8f61d19, 2026-04-27T08:20:00Z)` —
post-Phase-0 corpus, dev-tagged to skip auto-update.
Target: eval-zcp project, container `zcp` (per
`CLAUDE.local.md` eval-zcp authorization).

**Status: BASELINE GREEN**. Phase 1 establishes baseline only;
Phase 7 re-run on post-Phase-6 corpus is binding for SHIP per
amendment 5 / Codex C5.

## Procedure

Per the followup plan §5 Phase 1 §1.2:

1. **Survey existing scenarios** — `develop-add-endpoint.md`
   chosen as default per the amended plan (Laravel adopt +
   develop is close to "deployed-edit-task" shape; fixture seeds
   a deployed implicit-webserver service).
2. **Run on post-Phase-0 corpus** —
   `~/.local/bin/zcp-hygiene eval scenario --file
   /home/zerops/eval-scenarios/develop-add-endpoint.md` from the
   eval-zcp `zcp` container (per memory rule
   `feedback_eval_always_remote.md`).
3. **Pre-vs-post comparison deferred to Phase 7** — per
   amendment 5, Phase 1 numbers are stale once any content
   phase commits; the binding pre-vs-post comparison is at
   Phase 7. This Phase 1 run establishes "the post-Phase-0
   corpus passes the scenario end-to-end".

## Result — PASS

```
=== Scenario develop-add-endpoint ===
PASS  6m17.002821826s
```

| Metric | Value | Required | Status |
|---|---|---|---|
| Grade verdict | passed: true | passed: true | ✅ |
| Final URL HTTP status | 200 | 200 | ✅ |
| Final URL hostname | app | app | ✅ |
| Final URL | https://app-21ca.prg1.zerops.app | (per finalUrlHostname) | ✅ |
| Duration | 6m17s | (no max) | ✅ |
| zerops_workflow calls | 7 | ≥ 7 (`workflowCallsMin`) | ✅ |
| zerops_discover calls | 2 | ≥ 1 (`mustCallTools`) | ✅ |
| zerops_verify calls | 1 | ≥ 1 (`mustCallTools`) | ✅ |
| Workflows entered | bootstrap, develop | bootstrap, develop (`mustEnterWorkflow`) | ✅ |
| Required patterns | 4/4 | `"workflow":"bootstrap"`, `"route":"adopt"`, `"scope":[`, `"app"` | ✅ |
| `requireAssessment` | EVAL REPORT present, success state | true | ✅ |

## Tool-call sequence (n=27)

```
 1. ToolSearch    select:zerops_workflow,zerops_discover  (load schemas)
 2. zerops_workflow action="status"                         (initial discovery)
 3. zerops_discover                                          (existing services)
 4. zerops_workflow action="start" workflow="bootstrap"      (no route — discovery)
 5. zerops_workflow action="start" workflow="bootstrap" route="adopt"
 6. zerops_workflow action="complete" attestation="..."      (first attestation, app)
 7. zerops_discover includeEnvs                              (verify env vars)
 8. zerops_workflow action="complete" attestation="..."      (second attestation, db)
 9. zerops_workflow action="start" workflow="develop"        (with scope=[app])
10. Bash ls /var/www/app/                                    (inspect app dir)
11-18. Glob/Read/Bash                                        (read laravel sources)
19. zerops_workflow action="strategy" strategies={app:push-dev}
20. Edit /var/www/app/bootstrap/app.php                      (wire api routes)
21. Write /var/www/app/routes/api.php                        (define GET /api/status)
22. ToolSearch select:zerops_deploy
23. zerops_deploy targetService="app"                        (single self-deploy)
24. Bash curl …/api/status                                   (verify endpoint)
25. Bash curl … (main app sanity)
26. ToolSearch select:zerops_verify
27. zerops_verify serviceHostname="app"                      (close session)
```

Tool-call breakdown:
- 7 × `zerops_workflow` (≥7 required ✅)
- 2 × `zerops_discover`
- 1 × `zerops_deploy`
- 1 × `zerops_verify`
- 4 × `Bash`, 4 × `Read`, 3 × `Glob`, 1 × `Edit`, 1 × `Write`
- 3 × `ToolSearch` (load deferred tool schemas)

**0 wasted tool calls** (per agent assessment); **0 iterate
cycles** (first deploy passed); **no iteration loops** required.

## Agent assessment (verbatim from result.json)

```
## EVAL REPORT

### Deployment outcome
State: SUCCESS

### Workflow execution
- Steps completed: bootstrap discover, bootstrap provision,
  bootstrap close (auto-skipped), develop-active (strategy,
  deploy, verify)
- Steps skipped: bootstrap close was auto-skipped after provision
  completed adoption
- Iterations: 0 iterate cycles needed — first deploy passed
- Gate failures: none
- Strategy chosen: push-dev for app

### Failure chains
No failure chains.

### Information gaps
No significant information gaps encountered. The workflow
guidance was comprehensive at each step.

### Wasted steps
- ToolSearch for zerops_verify — needed to load the tool schema
  before calling it. This is a mechanical step, not a knowledge
  gap.
- Total wasted tool calls: 0 (all tool calls contributed to
  forward progress)

### What worked well
- zerops_workflow action="status" — immediately showed
  not bootstrapped state for app and db, plus clear guidance
  to start bootstrap with adopt route discovery.
- zerops_workflow action="start" workflow="bootstrap"
  (no route) — the two-step discovery/commit pattern worked
  cleanly. routeOptions[] returned adopt at the top with
  adoptServices list, exactly as documented.
- zerops_workflow action="start" workflow="bootstrap"
  route="adopt" — plan submission with isExisting: true and
  resolution: EXISTS for db was accepted first try.
- zerops_workflow action="start" workflow="develop" — the
  detailed guide covered implicit-webserver specifics
  (no run.start, no run.ports, documentRoot handling) which
  confirmed I should not touch zerops.yml structure.
- zerops_deploy targetService="app" — self-deploy worked
  seamlessly, returned subdomainUrl for immediate verification.
- zerops_verify serviceHostname="app" — clean pass with
  autoCloseProgress showing 1/1 ready, confirming the session
  would auto-close.
- Develop-active guidance atoms — the "Implicit-Webserver
  Runtime" section correctly noted to omit run.start and
  run.ports for php-nginx, preventing a common
  misconfiguration. The "Push-Dev Deploy Strategy" section
  clearly explained self-deploy semantics.
```

## Atoms exercised by the agent (cross-reference)

The agent's "What worked well" enumeration explicitly cites
hygiene-touched corpus content:

| Agent observation | Atom (cross-reference) | First cycle work |
|---|---|---|
| "Implicit-Webserver Runtime" section | `develop-implicit-webserver` | LOW-risk Phase 6 deferred (still in followup Phase 6 list) |
| "Push-Dev Deploy Strategy" — self-deploy semantics | `develop-push-dev-deploy-container` (or `-local`) | Heavy hygiene scope (axes K/L/M) for followup |
| Adopt route discovery / `adoptServices` list | `bootstrap-adopt-discover` + `bootstrap-adopt-plan` | Untouched |
| `attestation` two-step | `bootstrap-classic-plan-*` | Untouched |
| `isExisting: true / resolution: EXISTS` for db | bootstrap adopt route specifics | Untouched |
| auto-skip of close after provision in adopt | adopt-completion semantics | Untouched |

The agent narrates the post-first-cycle corpus as actively
helping it. No information gaps reported. **Strong signal that
the post-first-cycle hygiene is NOT regressing agent behavior**.

## Phase 7 obligations (per amendment 5)

Phase 7 G6 binding evidence MUST:

1. **Re-run** `develop-add-endpoint` against the post-Phase-6
   corpus (binary built from post-Phase-6 HEAD, dev-tagged or
   auto-update disabled).
2. **Compare** vs:
   - PRE-hygiene baseline (worktree at commit `96b9bab7` —
     before first cycle started). The plan §1.2 step 3 baseline
     command is preserved for Phase 7 to execute.
   - This Phase 1 baseline (post-Phase-0 corpus). Should match
     or improve.
3. Save to
   `plans/audit-composition/g6-eval-regression-post-followup.md`.
4. **Pass criteria**: post-Phase-6 PASS verdict + tool-call
   count ≤ Phase 1 baseline + 0 NEW iterate cycles +
   no NEW information gaps.

## Operational notes

- **Auto-update bypass**: a `dev`-versioned binary
  (`-X server.Version=dev`) skips the post-`serve` self-update
  per `internal/update/once.go:33-35`. For Phase 7 re-run,
  re-build with `-X Version=dev`.
- **Cleanup**: scenario auto-cleans the project after PASS —
  eval-zcp now empty (was 4 services + Laravel fixture during
  the run). For follow-up smoke / dev work, re-provision per
  `CLAUDE.local.md` eval-zcp authorization.
- **Final URL**: `https://app-21ca.prg1.zerops.app` was the
  ephemeral subdomain the eval auto-allocated. Will not exist
  for Phase 7 (services rebuilt).

## Disposition

| Aspect | State |
|---|---|
| Grade verdict | ✅ PASS |
| All `mustCallTools` | ✅ all present |
| `workflowCallsMin` (7) | ✅ exactly 7 |
| All `requiredPatterns` | ✅ 4/4 matched |
| `requireAssessment` | ✅ EVAL REPORT, success state |
| `finalUrlStatus: 200` | ✅ |
| Phase 1 EXIT criterion | ✅ baseline established |

Phase 7 closes G6 binding evidence by re-running on
post-Phase-6 corpus.

## Archived artifacts

- `plans/audit-composition/g6-eval-2026-04-27/result.json`
- `plans/audit-composition/g6-eval-2026-04-27/tool-calls.json`
- `plans/audit-composition/g6-eval-2026-04-27/log.jsonl`
- `plans/audit-composition/g6-eval-2026-04-27/task-prompt.txt`
