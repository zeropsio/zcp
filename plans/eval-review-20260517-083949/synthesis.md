# Eval Review Synthesis — 20260517-083949 (partial: 14/45 scenarios)

## Coverage caveat

Run was killed mid-stream. The entire **launch-production cluster (9 scenarios in previous run, driving RC1 + much of RC3) did not execute**, nor did `recipe-laravel-minimal`, `greenfield-*`, `existing-simple-mode-node-add-endpoint`, `fullstack-multi-runtime`, or the `git-push-setup-with-cicd-method-prompt` / `export-buildfromgit-self-snapshot` scenarios. **This synthesis cannot speak to the status of any previous-run finding rooted in those clusters** — they're in section D.

Covered: 6 classic-* (bun, go, php-mariadb, python-postgres-dev, rust-postgres-standard, static-nginx); 5 existing/develop (adopt-existing-standard-pair NEW, api-node-postgres-classic-dev NEW, cross-deploy-stage-promote, develop-add-managed-dep, develop-loop-after-bootstrap NEW); 3 multi-service/delivery (cadence-build NEW, cadence-spec NEW, delivery-git-push-actions-setup).

---

## Top 3 priority to fix now

### 1. `ToolSearch select:` comma-list documented form is dead — NEW-BROKEN
- **Evidence:** `develop-loop-after-bootstrap` agent followed `bootstrap-tool-preload.md` verbatim (`select:zerops_workflow,zerops_discover,...`) and got "No matching deferred tools found" twice. Fell back to one-at-a-time keyword search. Agent quote: "Don't trust the batch-load instruction."
- **Layer (verified):** `internal/content/atoms/bootstrap-tool-preload.md:16,19` documents exactly the form that failed. Either harness rejects this form, the bare `zerops_*` short names don't match (prefix may need `mcp__zcp__`), or the comma-syntax was never wired. Same friction also flagged in previous run for `classic-php`, `classic-static`, `greenfield-website-from-brief` (Appendix B item 4 of prior synthesis) — now confirmed as a HARD failure mode, not just disambiguation friction.
- **Fix shape:** Determine whether harness or atom is wrong. If atom is wrong, rewrite to the working form (probably `mcp__zcp__zerops_*` per Group B transcript). If harness is wrong, fix the parser. Every fresh bootstrap session is being told to make a dead-end call.
- **Why this rank:** Hit on the very first turn of every bootstrap; previous runs absorbed the friction silently; new scenario surfaced it cleanly. Wide blast radius, zero workaround except "ignore the guidance."

### 2. Recovery from `READY_TO_DEPLOY` post-failed-deploy is a three-call rejection dance — UNFIXED (now sharper evidence)
- **Evidence:** `cadence-multiservice-build` narrated the full dance: deploy fails → service stuck `READY_TO_DEPLOY` → next deploy refused with `DIAGNOSIS_REQUIRED` → `zerops_import override: true` rejected → must extract `{operation, acknowledgedTargets}` from `wouldDestroy` payload in the rejection → call `override+confirmDestructive` → redeploy. Agent had to round-trip through a second rejection to discover the field-name shape.
- **Layer (verified):** `internal/ops/events.go` exists (ClassifyDeployFailure); `internal/tools/import.go:25-62` defines `ConfirmDestructive` + the wouldDestroy → confirmDestructive flow. The mechanism is implemented (CLAUDE.md "diagnose-before-destruct" invariant), but the FIRST deploy-failure response doesn't populate a `Recovery` struct pointing at the exact `zerops_import override+confirmDestructive` call shape — agent must reach the second rejection to learn the field names.
- **Fix shape:** The first `DIAGNOSIS_REQUIRED` response should already carry a structured `Recovery: {tool, action, args}` with the exact ack payload pre-filled (operation + acknowledgedTargets sourced from the same `wouldDestroy` that the second-call rejection already produces). Atom for `READY_TO_DEPLOY` recovery needs the one-call ack pattern, not "trial, error, fix" framing.
- **Why this rank:** Generic post-failed-deploy state on every greenfield with any first-deploy bug. Recovery is implemented but discovery cost is paid each time. Confirmed root-cause-level (not symptom) — same defect-class as previous RC3 "lifecycle handlers are path-blind."

### 3. `zerops_verify http_root` reports "pass" for HTTP 404 / dev-mode JIT-compile timeouts — NEW-BROKEN
- **Evidence:** Multi-session signal across two groups. (a) `api-node-postgres-classic-dev` + `develop-loop-after-bootstrap` (Group B): `http_root: pass` on HTTP 404 ("Cannot GET /"). (b) `cadence-multiservice-build` + `cadence-multiservice-spec` (Group C): `http_root: fail / context deadline exceeded` after dev-server printed "Ready in 2.4s" — actually a Next.js JIT-compile timeout, not a server fault. Cure was `ssh appdev curl localhost:3000/...` to warm cache.
- **Layer (verified):** `internal/ops/verify_checks.go:16` + `internal/ops/verify.go:140` + `internal/ops/verify_recovery_test.go` carry the `http_root` check shape. The check semantics conflates "reachable" with "responds correctly to expected route" (404-as-pass) AND has a static timeout that's shorter than dev-mode framework first-compile (fail-as-startup-error).
- **Fix shape:** Either (a) split `http_root` into `http_reachable` + `http_responsive` (HTTP code part of summary), or (b) gate `pass` on non-4xx/5xx. For dev-mode runtimes, lengthen first-probe timeout OR annotate fail as "may be JIT-compile, retry after warm." Two independent surfaces (4 sessions across 2 groups) makes this clearly broken behavior, not friction.
- **Why this rank:** Verify is the agent's primary "did it work" oracle; reporting pass on 404 invites "service works" attestations that are wrong. Reproducible across runtime classes. New surface (didn't appear in previous synthesis at this severity).

---

## A. Fundamentally broken (NEW since last run)

- **[platform/atom] Container `exec`-wraps `start:` and silently truncates `&&` chains** — `cadence-multiservice-build` finding 1. `start: npx prisma db push --accept-data-loss && npm start` ran the migrate, then exited (no `npm start`). Logs do not identify the chain as the cause. Verified: NO atom in `internal/content/atoms/` warns about `&&` in `start:`. Affects any recipe chaining DB migration into start (Laravel `php artisan migrate && php-fpm`, Rails `rake db:migrate && rails server`). Fix: either platform stops exec-wrapping start, or atom for runtime `start:` authoring explicitly forbids shell-chains and points at `initCommands` for migrations.
- **[ops/import response]** `zerops_import` returns `stack.build: PENDING` simultaneously with summary "All 7 processes completed successfully" — visually contradictory snapshot in response assembly (`classic-rust-postgres-standard` finding 3). Pinned to `internal/ops/` import response assembly path. Either wait one beat before snapshot or filter aggregated-already entries.
- **[ops/dev_server reason]** `zerops_dev_server` reports top-level `reason: health_probe_timeout` when actual cause is V8 OOM visible only in `logTail` (`cadence-multiservice-spec` finding 1). Verified: `internal/ops/dev_server_start.go:368` literally sets that reason on probe context exceeded, with no inspection of logTail for known-OOM signals. Top-level reason is technically true but actively misleading — agent hunts at probe/network layer.
- **[ops/deploy lifecycle predictability]** Same `zerops_deploy` call returns sync `DEPLOYED` first time, async `BUILD_TRIGGERED + timedOut:true` second time within one session (`develop-loop-after-bootstrap` finding 2). Verified two distinct UUIDs in same session. Async polling is fine; lack of upfront predictability is the bug. Either deterministic threshold or document the boundary.
- **[knowledge/recipes]** `npm ci` default in Node first-deploy is wrong twice in one run (cadence-build #5 + cadence-spec #3). Verified: `internal/knowledge/recipes/{vue,solidstart}*-hello-world.md` default to `npm ci` in prod and `npm install` in dev with framing comments — applied to greenfield no-lockfile contexts it fails 100% of the time. Either gate on lockfile-present or document.
- **[platform/preflight]** `${db_*}` references in `buildCommands` resolve only at run time, not build time; no preflight warns (`cadence-multiservice-build` finding 3). Burned a deploy. Could detect at submit time in deploy preflight.
- **[atoms/PHP catalog]** `php-nginx@8.4` accepted as build base, then `INVALID_ZEROPS_YML: unknown base` (`classic-php-mariadb-standard` finding 1). Catalog stale: documented list (`php@{7.4,8.0,8.1,8.3}`) doesn't include 8.4; hello-world example shows `php@8.5`. Three `INVALID_ZEROPS_YML` rejects per agent before guessing right.
- **[tool-harness]** `AskUserQuestion` fails twice with bare error in `api-node-postgres-classic-dev` (finding 1). Likely host harness not ZCP, but worth flagging — also surfaced previous run (`recipe-laravel-minimal`, `pipeline-skip`).

## B. Unfixed from previous run

| Item | Previous reference | Status this run | Notes |
|---|---|---|---|
| `/var/www/{hostname}/` mount-claim is unconditional in shim, conditional in reality | RC2 (top 3) | confirmed | `cross-deploy-stage-promote-from-dev` self-review: "`ls /var/www/` only showed `CLAUDE.md`." `internal/content/templates/claude_container.md:3` text unchanged. |
| Rust develop workflow hint drops `setup=` for self-deploy with multi-setup yaml | RC3 / `classic-rust-postgres-standard` | confirmed | Same observation, same workaround (read yaml first). Envelope output unchanged. |
| Python `gunicorn` PATH gotcha (vendor/bin/) | Appendix A | confirmed | `classic-python-postgres-dev-only` finding 1. Same one-shot recovery. |
| Plan-schema nesting (`bootstrapMode`/`stageHostname` inside `runtime`) | Appendix A | confirmed | Every classic session flags it; every classic session succeeds because warning is loud. JSON example still not promoted over prose. |
| Recipe `importYaml` top-level `project:` block triggers `IMPORT_HAS_PROJECT` | Appendix A | confirmed | `classic-bun-simple` + `classic-python-postgres-dev-only`. (Inverse: `develop-add-managed-dep` reports the guidance landed for it — partial fix from atom side.) |
| `setup=` field reads as hostname/mode, not yaml block name | RC3 | confirmed | `classic-php-mariadb` + `classic-rust` + `cross-deploy-stage-promote-from-dev`. |
| `delivery-git-push-actions-setup` walkthrough chains three orthogonal actions | RC3 (group2 prior-run #1, #3) | confirmed | `delivery-git-push-actions-setup` findings 1+2+3 explicitly cite "same surface as prior run." `internal/tools/workflow_build_integration.go` + `workflow_git_push_setup.go` unchanged. |
| `/api/dashboard` fixture mismatch in `develop-add-managed-dep` prompt | Appendix A | confirmed | Fixture in `eval/behavioral/scenarios/fixtures/` still references endpoint that doesn't exist in seeded code. |
| Close-mode auto-close prompt buried in workSessionState | (recurring noise prior run) | confirmed and elevated | Most-mentioned friction in Group A (4 of 6: bun, go, php, rust). Agent recommendation in all 4: surface at session start. |

## C. Improved since previous run

| Item | Previous reference | Evidence of fix | Notes |
|---|---|---|---|
| Two-phase bootstrap `start` (route-menu → session-active) confusion | RC1 (4 of 6 classic sessions prior) | 1 of 6 mentions it (static-nginx); 5 of 6 navigated cleanly | `kind` discriminator likely more legible or agents pattern-matched better. |
| Adopt-pair plan-shape inference (`isExisting:true` on runtime, `resolution:EXISTS` on deps) | Appendix A | Schema asymmetry persists but guidance text guides agents through it | `develop-add-managed-dep` quote: "I got it right on first try only because the guidance called this out." `adopt-existing-standard-pair` also succeeded first try. Schema-level structural fix deferred; guidance band-aid holding. |
| `connectionString` `/dbName`-suffix warning | RC3 surface | guidance internalized | `develop-loop-after-bootstrap` + `cadence-multiservice-spec` both quote the warning as load-bearing. Drizzle/Prisma trap avoided. |
| `setup`-required preflight error wording | RC3 | `failureClassification.suggestedAction` is enough to recover without re-reading docs | `cross-deploy-stage-promote-from-dev` self-review. Classifier copy now actionable. |
| Stale `launch-active` envelope from `status` | RC1 (cross-deploy + 8 others) | Absent from `cross-deploy-stage-promote-from-dev` self-review this run | Could be cleaner test-project state OR `launch_status_recovery.go` tightening. Need launch-production cluster to run to confirm. |
| PHP readiness check `/status` → 404 misclassified as `failedPhase: init` | Previous `[BROKEN]` | Absent (agent used `status.php` with extension, sidestepping `.htaccess`) | Different framework (nginx vs apache); not strictly the same scenario, but the readiness-fail-misclassified-as-init category didn't recur. Classifier-fix attribution speculative. |
| `IMPORT_HAS_PROJECT` guidance | (recurring) | `develop-add-managed-dep` quote: "I knew this from the guidance" | Two classic sessions still hit it (bun, python) because they hit the recipe `importYaml` field rather than the discover-then-import path; the atom-level warning landed. |

## D. Ambiguous-coverage (previous findings in untested clusters)

- **RC1 `status` envelope conflation + drift baseline persistence** — entire launch-production cluster didn't run; the only `status`-shape evidence here is `cross-deploy-stage-promote-from-dev`, which was cleaner than previous run, but that may be project-state not code fix. Cannot assess whether `internal/tools/launch_status_recovery.go` baseline-clear was actually fixed.
- **RC3 `SERVICE_NOT_FOUND: not bootstrapped` (3 handler sites)** — not exercised; `git-push-setup-with-cicd-method-prompt` and `export-buildfromgit-self-snapshot` didn't run.
- **RC3 classify-prompt content-leak (export atoms shown in launch-production)** — entire classify-prompt surface untouched.
- **Appendix item: envClassifications four-bucket model missing ZCP-provisioned-regenerate slot** — launch-production specific, not exercised.
- **Appendix item: Subdomain auto-enable fan-out on multi-runtime** — `fullstack-multi-runtime` didn't run.
- **Appendix item: `zerops_workflow workflow="export"` discoverability** — `export-buildfromgit-self-snapshot` didn't run.
- **Appendix item: Phase-consumption of `productionProjectName`/`region`/`skipPipelineSetup`** — not exercised.
- **Appendix item: Build-failure classifier mislabeling readiness-fail as init** — PHP-apache scenario didn't recur (php-mariadb-standard uses nginx and sidestepped).

## E. Eval-infra observations

- **Run died after 14/45 scenarios** — full picture of launch-production / fullstack / recipe-laravel / export-buildfromgit clusters is unknown. The previous run's most actionable RCs (RC1 = status envelope, RC3 = path-blind handlers) had most of their evidence in those clusters; this run can't refute or confirm.
- **`AskUserQuestion` permission/harness denial recurs** (Group B `api-node-postgres-classic-dev`) — host harness issue, surfaced previous run too. Either eval allowlist or scenario rewrite to pre-pick choices.
- **`cadence-multiservice-build` ran 29m12s** — wall time dominated by 6 discrete recovery cycles. The scenario does produce a complete app and `user_sim_satisfied`, but burns most of the run on patterns that should fail fast. Useful as a stress-test of recovery surfaces.
- **`cadence-multiservice-spec` ran 16m44s with only 2m54s scenario time** — suggests ~14m retrospective time; worth checking whether retro-resume token usage is a budget risk for the full 45-scenario run.
