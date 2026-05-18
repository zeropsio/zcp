# Group A — Regression check on 6 fix-landing scenarios

Baseline: previous synthesis `plans/eval-review-20260517-083949/synthesis.md`.
This re-run targets 6 scenarios mapped to fixes Karel landed 2026-05-16…18.

---

## Session: develop-loop-after-bootstrap
**Duration:** 5m04s total (3m57s scenario)
**Previous broken behavior:** TOP RC#1 — `ToolSearch query="select:zerops_workflow,zerops_discover,..."` (bare comma-list documented by `bootstrap-tool-preload.md`) returned "No matching deferred tools found", forced one-at-a-time fallback. Karel's fix: commit `02f374f5` "idle-tool-preload" atom rewrite.
**Fix-landed status:** **STILL-BROKEN** — atom still uses bare `zerops_*` names; harness still rejects them.
**Evidence (transcript, not self-review):** First batch call `query="select:zerops_import,zerops_discover,zerops_process,zerops_env,zerops_deploy,zerops_verify,zerops_dev_server,zerops_logs"` returned `matches: []`. Agent self-corrected next turn with `select:mcp__zerops__zerops_import,...` — succeeded. The new atom `internal/content/atoms/idle-tool-preload.md:17` still documents the bare-name form (`select:zerops_workflow,zerops_discover,...`). Self-review does NOT mention this friction at all (agent treated it as a 1-turn correction not worth flagging) — but the dead path remains.
**Other findings in this run** (max 3):
- [SMOOTH] First-deploy `BUILD_FAILED` from `npm ci` no-lockfile recovered cleanly via `failureClassification.likelyCause`. Self-review: "the failureClassification field actually nailed this perfectly — likelyCause said exactly what was wrong." 31d976b4 env-var corpus preflight cushion appears to be holding.
- [SMOOTH] `verify http_root` returned `status=pass, httpStatus=200, bodyText="Express API running on Zerops"` — RC#3 (404-as-pass) did NOT recur here because real app endpoint was reachable. Previous-run reproduction of the false-pass-on-404 issue does NOT manifest in this run because the Express scaffold answered `GET /` correctly. Doesn't prove RC#3 is fixed — see api-node-postgres-classic-dev below.
- [GUIDANCE-MISLEADING] auto-close gating message "ready: 1, total: 1 but enabled: false reason: auto-close gated by close-mode" — self-review: "I had to look twice to realize 'ready' meant verified-and-deployed but auto-close still needs each in-scope service to have closeDeployMode set." Same finding as previous Group A.

---

## Session: cadence-multiservice-build-run2-replay
**Duration:** 16m16s total (49.7s scenario, 15m user-sim wallTimeout)
**Previous broken behavior:** TOP RC#2 — `READY_TO_DEPLOY` 3-call recovery dance after first-deploy fail (`DIAGNOSIS_REQUIRED` → `confirmDestructive` rejection → second rejection reveals field shape → finally accepted). Also `${db_*}` in buildCommands silent fail + `npm ci` default. Karel landed `33fb9358` first-deploy-failed retry guidance, `31d976b4` env-var corpus preflight, `d01a3a88` Prisma audit.
**Fix-landed status:** **NOT-EXERCISED** — scenario completed only 49s of agent work before AskUserQuestion-induced 15min idle wallTimeout. No deploy failure was triggered, no `DIAGNOSIS_REQUIRED`, no `wouldDestroy`, no `confirmDestructive` flow in transcript.
**Evidence:** Transcript shows 7 ZCP workflow calls, 2 discover, 1 import, 1 deploy — no `DIAGNOSIS_REQUIRED` token, no `wouldDestroy`, no `confirmDestructive`. The single `READY_TO_DEPLOY` reference is the initial state of `appstage` (expected for dev/stage pair), NOT a failed-deploy state. The scenario got stuck on first AskUserQuestion turn (`is_error: true "Answer questions?"`); user-sim told agent "vyber default a jdi", but only 49s of subsequent work happened. Self-review still flags: "AskUserQuestion showed nothing useful to the user in this session" + "the npm ci no-lockfile + Prisma-in-devDeps gotchas" (the latter two are written prospectively from knowledge, not from this-run experience).
**Other findings in this run** (max 3):
- [NEW-BROKEN] AskUserQuestion harness rejection still present (`is_error: true "Answer questions?"`). 953433fc fixes the GRADER classification (treat as wait-signal) but NOT the underlying permission denial. Agent quote: "AskUserQuestion showed nothing useful to the user... either harness didn't render them or this caller doesn't support it." Same as 20260517 finding.
- [GUIDANCE-MISLEADING] Self-review surfaces composite-type asymmetry (plan wants `ubuntu/nodejs@22` + `postgresql:single@18`; import YAML wants `nodejs@22` + `mode: NON_HA`) as the biggest friction — recovery is by error-message re-read of `availableStacks`, but the inline doc example shows the wrong (short) form.
- [ATOM-CONTENT] Agent flagged Prisma-CLI-in-devDependencies prune trap as a real production gotcha not surfaced in node knowledge page. d01a3a88 Prisma audit committed an audit-doc but agent says the runtime knowledge page is still silent on the survival-after-prune story.

---

## Session: cadence-multiservice-spec-run2-replay
**Duration:** 3m46s total (3m11s scenario)
**Previous broken behavior:** TOP RC#3 reproduction — `dev_server reason: health_probe_timeout` when actual cause was V8 OOM visible only in logTail (Next.js JIT compile). Karel landed `31d976b4` env-var preflight.
**Fix-landed status:** **NOT-EXERCISED** — agent never called a Zerops tool. Scenario asked for design spec (wireframes, schema, tech stack), agent stayed in pure-prose mode and produced specification only.
**Evidence (self-review):** "Honestly, this scenario didn't trigger any of the four kinds of friction in a meaningful way, because I never actually called a Zerops tool... pure prose work. So I have no retried tool calls, no confusing responses, no misleading tool output to report." Single user-sim turn `terminatedBy: agent_declared_done` after 13.6ms.
**Other findings in this run** (max 3):
- [NEW-BROKEN] CLAUDE.md routing-smell rule misfires on spec-deliverable requests. Agent caught itself: "the smell rule is aimed at agents who write three paragraphs of analysis as a stalling tactic before starting work — it's not aimed at requests where the analysis IS the work. A future agent could read that table fast and conclude they should fire `zerops_workflow action=start workflow=bootstrap` immediately." Argues for an exception clause in the routing rule for explicit-spec requests.
- (smooth — no platform friction because no platform calls)

---

## Session: api-node-postgres-classic-dev
**Duration:** 6m23s total (5m27s scenario)
**Previous broken behavior:** TOP RC#3 — `verify http_root: pass` on HTTP 404 ("Cannot GET /"); AskUserQuestion permission denied. Karel landed `953433fc` AskUserQuestion-as-wait-signal (grader-side).
**Fix-landed status:** **STILL-BROKEN for verify**, **PARTIALLY-FIXED for AskUserQuestion** (grader OK, harness rejection unchanged).
**Evidence (transcript, not self-review):** `http_root: status=pass, httpStatus=404, bodyText="Cannot GET /"` is in the verify response, twice. Agent did NOT notice — self-review never mentions 404-as-pass. The previous-run RC#3 reproduces unmodified. AskUserQuestion: scenario terminated `user_sim_satisfied` after 30s of user-turn wallTime — grader correctly classified the pause, fix landed there. The actual `is_error: true` permission denial from the harness on AskUserQuestion calls is not visible in this run (no AskUserQuestion calls), so cannot independently confirm if harness side changed.
**Other findings in this run** (max 3):
- [GUIDANCE-MISLEADING] Type-format asymmetry (plan-step requires `ubuntu/nodejs@22` + `postgresql:single@18`; import YAML accepts short forms): "Two different validators, two different shape conventions, no warning that the plan-step convention differs." Same surface as cadence-build.
- [ATOM-CONTENT] `run.envVariables` self-shadow warning landed: "the develop-active response had a long section warning that `run.envVariables` self-shadow (writing `db_hostname: ${db_hostname}` resolves to the literal string)" — agent renamed keys preemptively and avoided. 31d976b4 env-var corpus message is reaching agent attention.
- [SMOOTH] `npm ci` no-lockfile recovery via failureClassification clean (same as develop-loop). Self-review: "read failureClassification.likelyCause before scrolling through buildLogs."

---

## Session: classic-php-mariadb-standard
**Duration:** 6m33s total (38.7s scenario)
**Previous broken behavior:** PHP build-base catalog stale — `php-nginx@8.4` accepted then `INVALID_ZEROPS_YML: unknown base`; hello-world example showed `php@8.5`. No specific commit identified — regression check only.
**Fix-landed status:** **NOT-RECURRED** (different friction surface this run). No `INVALID_ZEROPS_YML: unknown base` errors. No `php@8.5` or `php-nginx@8.4` rejections.
**Evidence (transcript):** Agent submitted `php-apache@8.5` (no OS prefix) once, got rejected on the PLAN step (`type "php-apache@8.5" not found in available service types`), then resubmitted with `ubuntu/php-apache@8.5` and succeeded. Build base in the deployed zerops.yaml used `ubuntu/php-apache@8.5` cleanly — no build-base rejection. So the prior-run stale-catalog surface didn't reproduce, but a different friction (OS-prefix-required-on-plan-but-not-on-import-yaml) DID — same asymmetry as cadence-build + api-node-postgres-classic-dev.
**Other findings in this run** (max 3):
- [GUIDANCE-MISLEADING] OS-prefix asymmetry on plan vs. import. Self-review: "The available-stacks listing at the bottom of the workflow response is the authoritative catalog; the inline example is misleading." Cross-scenario pattern (3 of 6 in this group).
- [ATOM-CONTENT] `zerops_knowledge` response for `runtime=php-apache@8.5 services=[mariadb@10.6]` is internally inconsistent. Self-review: "The body comment in the example zerops.yaml says 'DB_NAME matches the PostgreSQL service hostname' — in a doc explicitly requested for MariaDB. The 'Decision Hints' section says 'Use PostgreSQL for everything unless you have a specific reason not to.'" Atom-content drift: knowledge response copies generic content where service-type was supposed to template.
- [GUIDANCE-MISLEADING] close-mode gating message buried at end of session — same friction as develop-loop above, recurring across this group.

---

## Session: cross-deploy-stage-promote-from-dev
**Duration:** 6m47s total (4m32s scenario)
**Previous broken behavior:** RC2 from 20260515 `/var/www/{hostname}/` mount-lie (`ls /var/www/` only shows `CLAUDE.md`); `setup=` reads as hostname. Karel landed `831a99ed` "setup-gate honors ProdSetupNameOverride + lists available names" (launch-production scope).
**Fix-landed status:** **MOUNT-LIE STILL-BROKEN**, **SETUP-GATE NOT-EXERCISED** (fix is in launch flow, not develop-deploy).
**Evidence (transcript):** Bash `ls /var/www/appdev/ 2>/dev/null || echo "NOT MOUNTED"` returned `NOT MOUNTED` — same mount-lie pattern. Agent recovered by re-doing adopt route + waiting for ZCP mount. Self-review does NOT flag mount-lie (agent treated it as 1-turn detour). `internal/content/templates/claude_container.md:3` ("Service code SSHFS-mounted at `/var/www/{hostname}/`; mount IS the service's runtime filesystem") unchanged since 20260515. Setup-gate: agent picked `setup=prod` by direct yaml inspection before any deploy call — no setup-gate error fired, so 831a99ed fix wasn't on-path. The previous "setup= reads as hostname" surface did not reproduce.
**Other findings in this run** (max 3):
- [NEW-BROKEN] Cross-deploy guidance: "Cross-deploy packages the dev tree without a second build" — agent quote: "I read that as 'no build runs at all.' That is not what happens. The actual zerops_deploy response came back with `buildStatus: ACTIVE` and `buildDuration: 1m9s` — a full build pipeline ran." For users whose entire ask was "do not rebuild," this matters; the text reads like a promise that no rebuild happens. Atom/guidance content drift.
- [GUIDANCE-MISLEADING] "Next" hints from develop session point at `zerops_deploy targetService=appdev` even though appdev was already verified — readiness counter treats adopt-then-cross as un-deployed. Self-review: "don't take the 'Next: Deploy appdev' hint at face value when the user has told you appdev is already good."
- [ATOM-CONTENT] Plan target carries ONE `type` field for the runtime pair, but `appdev=ubuntu/nodejs@22` and `appstage=alpine/nodejs@22` diverge. Schema/guidance silent on which half to pick. Agent picked dev's `ubuntu` and it accepted — "but I never got confirmation that's the right call when the halves diverge."

---

## Group-level patterns

- **`ToolSearch select:` bare-name form is dead, atom rewrite preserved the dead form** — `02f374f5` rewrote the atom (`idle-tool-preload.md`) but kept the broken `select:zerops_*` shape; harness only accepts `select:mcp__zerops__zerops_*`. Every fresh bootstrap session does the same N+N dance: try-bare → fail → try-prefixed → succeed. Self-review treats it as 1-turn correction and doesn't flag, so the agent-side cost is invisible in self-reviews but still visible in transcripts. STILL-BROKEN.

- **2 of 6 scenarios "not-exercised" — fix verification incomplete** — `cadence-multiservice-build-run2-replay` stalled at 49s after AskUserQuestion+15min timeout; `cadence-multiservice-spec-run2-replay` was a spec-only deliverable and agent never called Zerops. So fix landings for `33fb9358` (first-deploy-failed retry guidance), `31d976b4` (env-var corpus preflight), `d01a3a88` (Prisma audit) cannot be independently verified from these specific runs — partial signal from develop-loop + api-node where 31d976b4 path was healthy (clean failureClassification recovery, self-shadow warning landed).

- **`http_root: pass` on HTTP 404 unchanged** — api-node-postgres-classic-dev reproduces the RC#3 false-pass cleanly. Agent doesn't notice the 404 because verify says pass. Verify semantics (`internal/ops/verify_checks.go::http_root`) untouched between 20260517 and 20260518. Carry-over from previous synthesis.

- **Composite-type plan-vs-yaml asymmetry is now the cross-scenario top friction** — 3 of 6 sessions (cadence-build, api-node-postgres-classic-dev, classic-php-mariadb-standard) name it in self-review as their biggest pain point. Plan-step needs `ubuntu/nodejs@22` + `postgresql:single@18`; import YAML accepts `nodejs@22` + separate `mode: NON_HA`. Previously buried as a footnote in 20260517 — now load-bearing. Candidate for atom/schema-level unification.

- **AskUserQuestion split: grader-side fixed, harness-side unchanged** — `953433fc` correctly classifies AskUserQuestion as a wait-signal so the grader doesn't terminate spuriously. But the harness still returns `is_error: true "Answer questions?"` (visible in cadence-build transcript). User-sim now correctly absorbs the pause and continues, so the grader-fix is load-bearing for eval stability, but agents still see a denial they have to reason around.

- **`classic-rust-postgres-standard` 30-min context-deadline-exceeded — eval infra signal** — same scenario succeeded in 20260517 in 23m41s. The deadline failure with no self-review means either the agent wedged on a specific path (verify-loop? polling-loop? compaction-recovery?) or eval timeout needs widening for rust+postgres+standard. Suggest grepping its partial transcript before next subset re-run.
