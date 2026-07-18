# Eval Review Synthesis — 20260518 subset (17/18 scenarios)

## Headline

Of ~30 fixes Karel landed 2026-05-16/-18, **3 are cleanly confirmed FIXED**, **6 remain STILL-BROKEN** (including all top-3 RCs from the previous synthesis), **3 are NOT-VERIFIABLE because the subset never exercised the relevant code path**, and **1 scenario was rewritten away** (`launch-failure-build-stuck`). Top RC#1 (`ToolSearch select:` comma-list) — the atom rewrite landed but preserved the broken bare-name form, so every fresh session still does the same N+N dance. Top RC#2 (`READY_TO_DEPLOY` 3-call dance) — fix exists but didn't reach the symptom (one scenario hit it through a different code path and the round-trip cost persists). Top RC#3 (`verify http_root: pass` on 404) — no code change in the window; reproduces cleanly on `api-node-postgres-classic-dev`. New friction concentrated on three axes: composite-type plan-vs-yaml shape disagreement (3 sessions), `/var/www/{hostname}/` mount-lie (4 sessions), guidance overload (5 sessions).

## Top 3 RC verification — did the fixes land?

| RC | Previous symptom | Fix commit | Verdict | Evidence |
|---|---|---|---|---|
| #1 | `ToolSearch select:zerops_workflow,...` returns "No matching deferred tools found"; agent falls back to one-at-a-time | `02f374f5` (idle-tool-preload atom) | **STILL-BROKEN** | `idle-tool-preload.md:17` documents bare-name form; harness only accepts `mcp__zcp__zerops_*`. develop-loop transcript shows the same fail-then-prefix dance. Atom rewrite preserved the broken syntax. |
| #2 | `READY_TO_DEPLOY` → `DIAGNOSIS_REQUIRED` → `confirmDestructive` rejection → second rejection reveals shape → finally accepted (3-call dance) | `33fb9358` (first-deploy-failed retry guidance) | **NOT-VERIFIABLE + STILL-BROKEN where exercised** | (a) `cadence-multiservice-build-run2-replay` stalled at 49s after AskUserQuestion → never entered failed-deploy branch. (b) `recover-failed-buildfromgit-missing-dep` hit READY_TO_DEPLOY through develop-adopt path (not launch-production); fix is launch-production-scoped and didn't land here. No structured Recovery struct in first rejection. |
| #3 | `http_root: pass` on HTTP 404 ("Cannot GET /") | (no commit in window) | **STILL-BROKEN** | `api-node-postgres-classic-dev` transcript: `http_root: status=pass, httpStatus=404, bodyText="Cannot GET /"` recorded twice. Agent didn't notice. `internal/ops/verify_checks.go` + `verify.go` last touched `64e44be7` (2026-05-05), well before fix window. |

## Verdict matrix (all 17 scenarios)

| Scenario | Previous broken | Fix commit | Verdict | One-line |
|---|---|---|---|---|
| develop-loop-after-bootstrap | RC#1 ToolSearch select: comma-list | 02f374f5 | STILL-BROKEN | atom rewrite kept the bare `zerops_*` form; harness rejects |
| cadence-multiservice-build-run2-replay | RC#2 READY_TO_DEPLOY dance | 33fb9358, 31d976b4 | NOT-EXERCISED | AskUserQuestion + 15min idle timeout at 49s; no deploy failure triggered |
| cadence-multiservice-spec-run2-replay | RC#3 dev_server health_probe_timeout (V8 OOM) | 31d976b4 | NOT-EXERCISED | spec-only deliverable; agent never called a Zerops tool |
| api-node-postgres-classic-dev | RC#3 http_root pass on 404, AskUserQuestion denial | 953433fc | STILL-BROKEN (verify) + PARTIALLY-FIXED (AskUserQuestion grader-side only) | 404-as-pass reproduces; grader fix landed (no spurious termination); harness denial not re-tested |
| classic-php-mariadb-standard | PHP build-base catalog stale | (none specific) | NOT-RECURRED + DIFFERENT-SURFACE | no `unknown base` error; instead OS-prefix asymmetry (`php-apache@8.5` rejected on plan, accepted in yaml) |
| cross-deploy-stage-promote-from-dev | RC2 mount-lie, setup-gate | 831a99ed | MOUNT-LIE STILL-BROKEN, SETUP-GATE NOT-EXERCISED | `ls /var/www/appdev/` returns nothing; setup-gate fix is launch-scoped, not develop-deploy |
| classic-static-nginx-simple | `build.base: nginx@*` rejected | 2b896edc (static-workflow counter-intuition banner) | STILL-BROKEN | atom landed but gated on `phases:[develop-active]+runtimes:[static]`; agent wrote yaml before atom fanned out |
| landing-page-static-simple | same as classic-static | 2b896edc | STILL-BROKEN | same root, same delivery miss; agent never saw the banner |
| launch-production-dev-only | RC1 stale launch envelope, productionProjectId surfacing | f29293fe | PARTIALLY-FIXED + NOT-FULLY-VERIFIABLE | no stale-envelope leaks observed; flow never reached `launched` (4 fake-token rejections), so productionProjectId surfacing untested |
| launch-production-pipeline-not-configured | RC1 stale envelope, `SERVICE_NOT_FOUND: not bootstrapped` | 831a99ed | ENVELOPE FIXED, SERVICE_NOT_FOUND STILL-BROKEN | clean `idle` envelope; build-integration on cross-project service still fails with misleading "not bootstrapped" |
| launch-production-from-standard-pair | RC1/RC3 classify content-leak, external-secret emission | 4f242bfd | FIXED | classify-prompt renders `<@pickRandom(["REPLACE_ME"])>` correctly; no `yamlPreprocessingError`; clean run |
| launch-failure-build-stuck | architecturally invalid (v1 doesn't auto-build) | 1d71f285 + 19330620 | NEW-BROKEN-REPLACED-OLD (scenario rewritten) | new design = diagnostic-skills (read yaml, identify break); agent correctly identifies `nodejs@99-deliberately-bogus`; no launch attempted |
| launch-production-laravel-showcase | APP_KEY ambiguity, ZCP infra envs bucket gap | 9cdd095a + 74562628 | PARTIALLY-FIXED | APP_KEY no longer mentioned as friction (atom edits worked); ZCP infrastructure bucket (GIT_TOKEN) still requires manual reasoning |
| greenfield-fullstack-multi-runtime | 6 findings: subdomain fan-out, confirmDestructive shape, Next.js standalone, setup-vs-hostname | (no specific commit) | PARTIALLY-FIXED + REGRESSION | confirmDestructive + .next cache trap absent (clean); Next.js OOM regressed back as the biggest time sink |
| recipe-nextjs-ssr-frontend-standard | JIT-compile timeout, exec-wrap `&&`, RAM ceiling | recipe knowledge atom | AMBIGUOUS | recipe-atom guidance landed (deployFiles `.next/standalone/~` worked first try); JIT-compile/RAM not exercised here |
| recover-failed-buildfromgit-missing-dep | RC#2 READY_TO_DEPLOY dance | 33fb9358 | STILL-BROKEN | agent hit READY_TO_DEPLOY through develop-adopt path (not launch-target); 33fb9358 fix is launch-scoped and didn't land on this code path |
| resume-after-compaction | RC2 mount-lie | (none) | STILL-BROKEN | `ls /var/www/appdev/` silent fail; `claude_container.md:3` mount-claim text unchanged since 2026-05-04 |
| classic-rust-postgres-standard | (regression check) | — | TIMED-OUT | 30-min context-deadline-exceeded; no self-review produced; succeeded in 23m41s on 20260517 |

## A. Confirmed FIXED

- **`4f242bfd` external-secret literal REPLACE_ME** — observable in `launch-production-from-standard-pair`. classify-prompt renders `<@pickRandom(["REPLACE_ME"])>` correctly; no `yamlPreprocessingError`. Three independent occurrences in the response, zero agent confusion.
- **`9cdd095a` + `74562628` APP_KEY classification** — observable in `launch-production-laravel-showcase`. Self-review never mentions APP_KEY ambiguity (was central friction in prior runs); agent buckets it once and moves on.
- **`953433fc` AskUserQuestion grader-side wait-signal** — observable in `api-node-postgres-classic-dev`. Scenario terminated `user_sim_satisfied` after 30s pause; grader no longer spuriously terminates. (Harness-side `is_error: true` denial not re-tested in this subset — split fix.)
- **`31d976b4` env-var corpus preflight** (partial) — `develop-loop-after-bootstrap` + `api-node-postgres-classic-dev` both reference clean `failureClassification.likelyCause` recovery from `npm ci` no-lockfile, and `run.envVariables` self-shadow warning reached agent attention. Path is healthy in develop-active, untested in failed-deploy branch.
- **`1d71f285` + `19330620` launch-failure scenario rewrite** — old design (architecturally invalid: v1 doesn't auto-build) replaced with diagnostic-skills test. New design validates correctly: agent reads source yaml, identifies break, explains fix; no spurious launch attempted.
- **`f29293fe` status envelope conflation** (partial) — no stale `launch-active` envelope leaks observed across 4 launch-production sessions. Could be code fix OR clean eval-zcp state; need multi-launch session to confirm.

## B. STILL-BROKEN

- **RC#1 `ToolSearch select:` bare-name form** — 4 sessions hit it (develop-loop, both static-*, others). Atom rewrite (`02f374f5 idle-tool-preload.md:17`) preserved the broken `select:zerops_*` syntax instead of `select:mcp__zcp__zerops_*`. Self-reviews don't flag (agents treat as 1-turn correction), so cost is invisible in retrospectives but visible in every transcript. **Highest-frequency unfixed item.**
- **RC#2 `READY_TO_DEPLOY` recovery dance** — `recover-failed-buildfromgit-missing-dep` reproduces the cost (agent had to read instructions from buried paragraph + hesitate on destructive warning). `33fb9358` fix is launch-production-scoped; develop-adopt path didn't benefit. First `DIAGNOSIS_REQUIRED` response still doesn't carry pre-populated structured Recovery.
- **RC#3 `verify http_root: pass` on HTTP 404** — `api-node-postgres-classic-dev` reproduces unmodified. `internal/ops/verify_checks.go` + `verify.go` untouched between 20260517 and 20260518 (last commit `64e44be7` from 2026-05-05). Agent's primary "did it work" oracle still lies on 404.
- **RC2 `/var/www/{hostname}/` mount-lie** — 4 of 17 sessions hit it (cross-deploy, launch-laravel, launch-from-standard-pair, launch-failure-build-stuck, resume-after-compaction). `internal/content/templates/claude_container.md:3` unchanged since `9c9d0547` (2026-05-04). Highest-frequency baseline-unfixed item across the full subset.
- **Static `build.base: nginx@*` rejection** — both `classic-static-nginx-simple` AND `landing-page-static-simple` hit `INVALID_ZEROPS_YML: unknown base`. `2b896edc` Counter-intuitive banner landed but the static-keyed atom didn't fan out in time (gated on `phases:[develop-active]+runtimes:[static]` — agent wrote yaml during early develop-active before the static-runtime classifier triggered).
- **Composite-type plan-vs-yaml shape asymmetry** — 3 of 6 Group A sessions + 2 Group C sessions name it as their biggest pain point. Plan needs `ubuntu/nodejs@22` + `postgresql:single@18`; import YAML accepts `nodejs@22` + separate `mode: NON_HA`. Cross-scenario top friction this run.
- **Guidance overload in `zerops_workflow action="status"` / `develop` / `bootstrap`** — 5 of 17 sessions cite buried `Next:` line, walls of pre-load tool-schemas blocks, atoms-on-atoms. Same surface as 20260515 group-level finding; gating-by-detected-scope work hasn't landed.
- **`SERVICE_NOT_FOUND: not bootstrapped`** — `launch-production-pipeline-not-configured` reproduces verbatim. Error message reads as "service missing" when actual meaning is "no ZCP metadata"; agent has to derive cross-project nature from context. 3-handler-site finding from previous synthesis untouched.

## C. NOT-VERIFIABLE / coverage gap

- **RC#2 `cadence-multiservice-build` never hit the deploy-failure path** — scenario stalled at 49s after `AskUserQuestion` + 15-min idle wallTimeout. Transcript shows 0 `DIAGNOSIS_REQUIRED`, 0 `wouldDestroy`, 0 `confirmDestructive`. The single `READY_TO_DEPLOY` reference is initial state of `appstage` (expected). `33fb9358` first-deploy-failed retry guidance cannot be verified from this run. **Top RC#2 unverified.**
- **`cadence-multiservice-spec-run2-replay`** — spec-only deliverable; agent never called a Zerops tool. Self-review: "I have no retried tool calls, no confusing responses, no misleading tool output to report." Cannot verify any platform fix from this run.
- **`f29293fe` productionProjectId top-level surfacing** — none of 4 launch-production sessions actually reached `launched` status (dev-only stalled at ready-to-launch with fake tokens, pipeline-not-configured was post-launch, from-standard-pair was clean classify, launch-failure-build-stuck explicitly avoided launching). Code exists, no behavioral evidence.
- **`831a99ed` setup-gate fix** — only exercised on launch-production paths (which didn't reach `launched`). On `cross-deploy-stage-promote-from-dev`, agent picked `setup=prod` by direct yaml inspection before any setup-gate error could fire.
- **`d01a3a88` Prisma audit** — Prisma path not exercised in this subset.
- **`classic-rust-postgres-standard` timed out at 30 min** — same scenario succeeded in 23m41s on 20260517. Wedge on a specific path (verify-loop? compaction-recovery? polling-loop?) or eval timeout needs widening. No self-review = no signal. Recommend grepping partial transcript before next subset re-run.

## D. NEW-BROKEN this run

- **[atom-content] `classify-prompt` `action=` confusion in export atom** (launch-production-laravel) — `export-classify-envs.md` example block omits `action` field; agent inferred `action="classify"` and got `factType is required for action=classify` (real action is recipe-fact classification). 2 wasted tool calls.
- **[guidance-misleading] `mode: NON_HA` on dependency block silently ignored** (greenfield-fullstack-multi-runtime) — agent: "I put `mode: NON_HA` on the dependency block, and it silently disappeared — the priorContext echoed it back as `NON_HA [defaulted]`." Accepted-then-ignored field is a footgun.
- **[atom-content] Cross-deploy "no second build" guidance is misleading** (cross-deploy-stage-promote-from-dev) — atom says "Cross-deploy packages the dev tree without a second build" but actual response shows `buildStatus: ACTIVE`, `buildDuration: 1m9s`. For users whose ask was "do not rebuild," this matters.
- **[guidance-misleading] Develop "Next:" hint after adopt-then-cross treats already-verified service as un-deployed** (cross-deploy-stage-promote-from-dev) — readiness counter doesn't account for cross-deploy completion.
- **[guidance-misleading] CLAUDE.md routing-smell rule misfires on spec-deliverable requests** (cadence-multiservice-spec) — current rule could push future agents into firing `zerops_workflow action=start workflow=bootstrap` on requests where analysis IS the deliverable.
- **[platform/guidance] Existing-project escape path not in `ready-to-launch` text** (launch-production-dev-only) — `existingProdToken` / `existingProjectId` in schema but not in guidance; agent had to remember from schema.
- **[guidance-misleading] Mount ordering surprises** (recover-failed-buildfromgit-missing-dep) — `zerops_workflow action="complete" step="provision"` auto-mounts as side-effect; "verify mount → complete step" felt natural but is backwards.
- **[atom-content] Plan target carries ONE `type` field but dev/stage halves can diverge** (cross-deploy-stage-promote-from-dev) — `appdev=ubuntu/nodejs@22` + `appstage=alpine/nodejs@22`; guidance silent on which half to pick.
- **[atom-content] Python-specific zerops.yaml convention missing** (recover-failed-buildfromgit) — knowledge has extensive Node/Java/Go/PHP/Deno/Gleam rules; Python is essentially "use `--no-cache-dir` for pip". Agent invented `pip install --target=./vendor` + `PYTHONPATH=/var/www/vendor`.
- **[atom-content] Mixed-OS dev/stage pair not flagged** (resume-after-compaction) — `ubuntu/nodejs@22` + `alpine/nodejs@22` accepted silently; should warn on setup mismatch.
- **[eval-infra] `classic-rust-postgres-standard` 30-min timeout** — same scenario succeeded in 23m41s on 20260517. Either agent wedged or rust+postgres+standard needs wider timeout.
- **[eval-infra] AskUserQuestion harness rejection persists** — `cadence-multiservice-build` transcript still shows `is_error: true "Answer questions?"`. `953433fc` fixes grader classification; harness permission denial unchanged.

## Top 3 priority to fix now (post-regression)

1. **RC#1 `ToolSearch select:` atom uses wrong prefix** — `internal/content/atoms/idle-tool-preload.md:17` (also `bootstrap-tool-preload.md`, `develop-tool-preload.md`) documents bare `select:zerops_workflow,zerops_discover,...` form that the harness rejects with "No matching deferred tools found". Fix: rewrite atoms to use `select:mcp__zcp__zerops_workflow,mcp__zcp__zerops_discover,...` (verified working form in develop-loop transcript). Highest-frequency unfixed item; hits every fresh bootstrap session. Self-reviews don't flag it because agents recover in 1 turn, but the dead-end-then-recover pattern is in every transcript.

2. **RC2 `/var/www/{hostname}/` mount-lie** — `internal/content/templates/claude_container.md:3` unconditionally asserts "Service code SSHFS-mounted at `/var/www/{hostname}/`" but mount only materializes after bootstrap/adopt completes provision. 4 of 17 sessions reason around this; persists since 2026-05-04 (commit `9c9d0547`); no commit in 2026-05-16/-18 window touches it. Fix shape: conditional framing — "after bootstrap/adopt provision close, service code mounts at `/var/www/{hostname}/`" + "if `ls /var/www/{hostname}/` empty, service not-yet-adopted, run `workflow=bootstrap route=adopt`".

3. **RC#3 `http_root` 404-as-pass** — `internal/ops/verify_checks.go::http_root` reports `pass` on HTTP 404 with body `"Cannot GET /"`. Verify is the agent's primary "did it work" oracle; reporting pass on 404 invites wrong attestations. Reproduces cleanly on `api-node-postgres-classic-dev`. Fix shape options: (a) gate `pass` on non-4xx/5xx HTTP code, (b) split into `http_reachable` + `http_responsive` with HTTP status in summary. Verify untouched in fix window — straightforward fix on a clear root cause.

**Honorable mentions** (top friction but lower fix-priority for this round):
- Composite-type plan-vs-yaml asymmetry (3 sessions name as biggest pain) — needs design pass, not point fix.
- `READY_TO_DEPLOY` recovery dance on develop-adopt path — `33fb9358` exists for launch-production; needs parallel fix for develop-adopt code path.
- Static `build.base: nginx@*` rejection — `2b896edc` landed but atom fan-out gating misses early develop-active; needs broader phase fan-out OR live `[B]` annotation enhancement.
