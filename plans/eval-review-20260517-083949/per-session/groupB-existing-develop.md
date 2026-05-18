# Group B: existing / develop / adopt sessions (5 scenarios)

Run dir: `/Users/macbook/Documents/Zerops-MCP/zcp/eval/behavioral/runs/20260517-083949/`

## Session: adopt-existing-standard-pair (NEW)

**Task:** Adopt an existing standard pair (`appdev`/`appstage` nodejs + `db` postgres) built manually via dashboard — write ZCP ServiceMeta only, no new services, no redeploy.

**Duration:** 4m41 total (44s scenario, 31s retro). 1 user iteration before user-sim satisfaction. Clean run.

**Findings (by severity):**

1. **[GUIDANCE-MISLEADING] Adopt plan-shape asymmetry between runtime and dependencies** — Dependencies take `resolution: "EXISTS"` enum; the runtime takes a boolean `isExisting: true` nested inside the `runtime` object. Schema docs lean on the `resolution` enum for deps and only mention `isExisting` in passing. Agent guessed correctly but wasn't sure whether `isExisting: true` was load-bearing or whether adopt would infer from live state. Layer: atom `bootstrap-adopt-discover.md` + plan-schema docs. Cross-session: yes — same pair-adopt inference gap reported by `develop-add-managed-dep-to-existing` finding #2.

2. **[GUIDANCE-MISLEADING] `detailedGuide` on provision step is classic-route-coded for adopt** — Mentions `startWithoutCode`, `READY_TO_DEPLOY` recovery, override semantics — none apply to adopt (services already ACTIVE, env vars already real). For adopt, provision is "run discover with envs, write attestation, move on" — one call. Layer: `internal/content/workflows/bootstrap/` or wherever provision step's `detailedGuide` is composed; needs route-specific (adopt vs classic vs recipe) branches. Cross-session: no — adopt-specific.

3. **[RESPONSE-CONFUSING] Final envelope: `progress.completed: 3/3` but close step is `status: "skipped"`** — "completed" is being used loosely (counts skipped as done). For adopt, close auto-skips because nothing to close out. Agent had to reconcile "skipped = finished" by inspection. Layer: `internal/workflow/progress.go` or envelope composer — separate `completed`/`skipped` counts, or label as `closed: 2 complete + 1 skipped`. Cross-session: low — adopt-specific lifecycle.

4. **[RESPONSE-CONFUSING] `intent` required on both menu call and commit call but not spelled out** — Route-menu response declares `kind: "route-menu"` and "no session is open yet"; second call with `route="adopt"` then needs same `intent` again. Agent passed it by reflex. Contract isn't documented. Layer: `internal/tools/workflow.go` schema description or atom for bootstrap-route-options. Cross-session: low.

---

## Session: api-node-postgres-classic-dev (NEW)

**Task:** Stand up Node.js REST API + Postgres in dev-only mode in eval-zcp project. No stage, no production, no existing repo.

**Duration:** 6m05 total (35s scenario, 44s retro). 1 user iteration. User-sim terminated `unknown` (timeout? unclear).

**Findings (by severity):**

1. **[BROKEN] `AskUserQuestion` tool fails twice with bare `Answer questions?` error** — Agent attempted `AskUserQuestion` to clarify branching options, got two consecutive failures with no diagnostic, gave up and fell back to prose. Transcript confirms 7 mentions of "Answer questions?" — failure recurred. No recovery path; agent couldn't even tell whether transient, permission-mode, or fundamental. Layer: tool harness / MCP server (likely not in `internal/tools/zerops_*` — this is a Claude Code built-in). Cross-session: low — but worth flagging if it crops up elsewhere.

2. **[BROKEN-soft] First deploy guaranteed to fail because `buildCommands: npm ci` lacks lockfile** — Greenfield scaffold has no `package-lock.json` committed; `npm ci` requires one. 27-second build cycle wasted. Failure response (`failureClassification.likelyCause`) excellent and points right at it. The fix is to make atom/recipe guidance default to `npm install` for greenfield scaffolds, or have the scaffold step pre-create a lockfile. Layer: atom for develop scaffold or `internal/knowledge/recipes/<nodejs-*>.md`. Cross-session: yes — predictable trap for any greenfield node scenario.

3. **[ATOM-CONTENT] `appdev` mount is NOT empty even with `startWithoutCode: true`** — Mount came pre-seeded with leftover `src/app/dashboard/...` Next.js scaffold from ZCP itself. Develop guidance says "preserve existing scaffold if present" — agent almost wasted time "adapting" three random `page.tsx` files into a REST API. Layer: develop scaffold atom — change "preserve existing" to "inspect first; rm framework boilerplate that doesn't match user intent." Cross-session: medium — any greenfield-mount-with-leftover-scaffold scenario.

4. **[ATOM-CONTENT] Postgres env-var name casing is inconsistent and trap-prone** — `user`, `password`, `hostname`, `port` are lowercase; `dbName` is camelCase; runtime prefix is lowercase `db_`, so reference becomes `${db_dbName}` — a typo creates a silent literal at runtime. Atom doesn't visually flag the case-mix nor warn about silent-failure mode. Layer: env-var catalog atom / bootstrap-env-var-discovery atom. Cross-session: yes — Postgres recurs in develop-add-managed-dep and develop-loop scenarios.

5. **[ATOM-CONTENT] `initCommands` in `run:` for DB migrations not documented** — Agent guessed `initCommands` was the right place for `CREATE TABLE IF NOT EXISTS`; the dev-mode dynamic-runtime checklist mentions `start: zsc noop --silent` but doesn't say where migrations belong. Worked by guess. Layer: dev-mode dynamic-runtime atom or `develop-first-deploy-scaffold-yaml.md`. Cross-session: medium.

6. **[GUIDANCE-MISLEADING] `zerops_verify` reports `http_root: pass` on HTTP 404 ("Cannot GET /")** — Verify is saying "service reachable," not "endpoints correct." Agent flagged that a less-careful reader could treat `status: healthy` as proof user's API works. Same issue surfaces in `develop-loop-after-bootstrap` finding #4. Layer: `internal/ops/verify_http.go` — separate "reachable" check from "responsive at expected route". Cross-session: yes.

7. **[GUIDANCE-OVERLOAD] Develop workflow's first response is enormous** — Thirty-plus sections of embedded guidance. Most correct, but volume drowns out signal. Agent had to skim aggressively; only three sections actually mattered (dev-mode dynamic-runtime checklist, env-var cheatsheet for Postgres, dev-server tool reference). Layer: `internal/content/workflows/develop/` — split into route-specific guidance bundles. Cross-session: yes — every develop session in this group reports volume friction.

---

## Session: cross-deploy-stage-promote-from-dev (RETURNING)

**Task:** Promote `appdev`'s exact build to `appstage` without rebuilding from source.

**Duration:** 5m28 total (3m35 scenario, 20s retro). `terminatedBy: agent_declared_done` (no user iterations needed).

**Findings (by severity):**

1. **[BROKEN-soft → INCONCLUSIVE] User-intent mismatch: there is no "copy bytes / promote artifacts" mode in Zerops** — User asked "don't rebuild, just push the same bytes." That literally cannot be done; every cross-deploy re-runs the build pipeline. Agent had to discover this by inference from tool description + first error. "Nothing told me directly 'there is no artifact promotion path.'" Agent told user honestly and proceeded; correct call. Layer: cross-deploy tool description + atom `develop-first-deploy-promote-stage.md` — needs explicit "every cross-deploy rebuilds; no artifact copy mode exists" upfront. Cross-session: low — cross-deploy-specific.

2. **[GUIDANCE-MISLEADING] `setup` field name is hostname-shaped but refers to zerops.yaml profile name** — Agent flagged "I almost reached for `setup=stage` initially based on the target name." When promoting `appdev → appstage`, you pass `setup=prod` because it names a block in source's zerops.yaml, not the target. Layer: `deploy_ssh.go` field description + atom `develop-first-deploy-promote-stage.md`. Cross-session: yes — same finding in previous run.

3. **[ATOM-CONTENT] `/var/www/` SSHFS mount claim from `claude_container.md` doesn't match reality** — Agent ran `ls /var/www` and saw only `CLAUDE.md`; zerops.yaml lives on the service's container at `/var/www/zerops.yaml`. Same as previous-run finding #2 — still unfixed. Layer: `internal/content/templates/claude_container.md:3` — mount-claim conditional unstated. Cross-session: yes — three sessions in previous run, two in this group (`develop-add-managed-dep`, cross-deploy).

4. **[SMOOTH] No stale `launch-active` envelope from `zerops_workflow action="status"` reported this run** — Previous run's finding #4 (status surfacing unrelated launch-active and inviting resumption) does NOT appear in the self-review. Either no leftover launch sessions on the test project this time, or priority logic was tightened. Worth confirming whether `internal/tools/launch_status_recovery.go` changed.

5. **[SMOOTH] `setup`-required preflight error now treated as resolvable from `failureClassification`** — Previous run found agents had to re-read docs; this run's self-review says "the error itself is actually helpful — `failureClassification.suggestedAction` tells you exactly what to do — so don't waste time re-reading docs, just read the error." Hint that classifier coverage is good enough that the surface is no longer painful. Cross-session: positive signal.

---

## Session: develop-add-managed-dep-to-existing (RETURNING)

**Task:** Add Redis-compatible cache to existing project (`appdev`/`appstage`/`db`), wire to `appdev` only — don't touch `appstage`.

**Duration:** 6m30 total (4m20 scenario, 32s retro). `terminatedBy: agent_declared_done`.

**Findings (by severity):**

1. **[GUIDANCE-MISLEADING] Bootstrap route menu offers `adopt` and `classic`; new-dep-with-existing-services routing not obvious** — Recipe-route options flagged "INCOMPLETE STACK: missing [valkey]" but would have tried to recreate `appdev`/`appstage`/`db` (wrong). Agent correctly picked `classic`. Layer: atom `bootstrap-route-options.md` — needs explicit "primary intent: create-new → classic, even if adoption also needed" rule. Cross-session: yes — same finding in previous run #3.

2. **[GUIDANCE-MISLEADING] Discover plan field nesting (bootstrapMode/stageHostname inside `runtime`) — fussy but guidance now catches it** — Agent got it right on first try because "the guidance called this out — without that warning I would have flat-set them." Previous run reported same gap as `[GUESS-SHAPE]` — guidance has improved enough to redirect attention. Status: guidance band-aid landed; nesting-shape still odd. Layer: schema design vs. atom.

3. **[ATOM-CONTENT] `connectionString` vs composed-URL guidance is now well-surfaced** — Agent explicitly used `${cache_connectionString}` for ioredis and composed manually for Postgres (`pg.Pool` config object). Previous-run finding #5 (env-var catalog doesn't flag references) appears addressed in atom/guidance — agent quotes the `connectionString` warning. Status: positive signal.

4. **[ATOM-CONTENT] Auto-close-on-green gate documentation buried** — Agent set `closeMode={"appdev":"auto"}` before deploying and session auto-closed after verify; that's correct. But "the atom about this is buried in the develop-active guidance; easy to miss." Without setting `closeMode`, the gate stays open with `closeDeployMode=unset` blocking auto-close. Layer: atom about close-mode in develop sessions — promote earlier in the develop guidance stack. Cross-session: medium.

5. **[GUIDANCE-OVERLOAD/RESPONSE-CONFUSING] `npm install` redundancy: deploy buildCommands include it, agent ran it manually via SSH first** — Not harmful, just wasted. Agent says "in dev mode the `buildCommands` are `npm install` which runs during deploy, so technically the in-container `npm install` I ran via SSH first was redundant." Layer: scaffold/deploy guidance should explicitly say "no need to npm install via SSH; buildCommands handles it." Cross-session: low.

6. **[EVAL-PROMPT] `/api/dashboard` referenced in prompt doesn't exist in code** — Same as previous-run finding #1 — unfixed at fixture level. Agent fabricated an endpoint. "If you want to be exact about it, ask." Layer: `eval/behavioral/scenarios/fixtures/` — fixture for this scenario should match the prompt. Cross-session: no.

7. **[SMOOTH] `IMPORT_HAS_PROJECT` rejection now correctly understood** — Agent quote: "ZCP rejects a top-level `project:` block with `IMPORT_HAS_PROJECT` because the project already exists. Strip it. Just submit `services:`. I knew this from the guidance." Previous run did not flag this surface as friction; guidance has remained adequate.

---

## Session: develop-loop-after-bootstrap (NEW)

**Task:** Express API + Postgres in dev-only mode, then iterate on code (add `/version` endpoint via second user turn). Two-turn scenario testing the develop loop after bootstrap.

**Duration:** 15m22 total (3m20 scenario, 28s retro). 3 user iterations. User-sim satisfied. `verification.json` present.

**Findings (by severity):**

1. **[BROKEN] `ToolSearch select:` with comma-separated names returns "No matching deferred tools found"** — Bootstrap guidance instructs `select:zerops_workflow,zerops_discover,zerops_import,zerops_deploy,zerops_verify,zerops_logs,zerops_events,zerops_dev_server` — agent tried it and was rejected. Single keyword queries worked. Transcript confirms the agent had to fall back to one-at-a-time keyword search. Agent quote: "I never figured out why `select:` rejected me — maybe a naming-prefix mismatch, maybe the syntax in the guidance is wrong. Don't trust the batch-load instruction." Layer: either the tool-search harness expects `mcp__zerops__zerops_workflow` prefix form (transcript shows agent tried `select:mcp__zerops__zerops_workflow` later), OR the guidance is wrong about supported syntax. This is a HARD-WRONG guidance: agent followed the documented pattern verbatim and got a dead-end error. Cross-session: HIGH — any develop / bootstrap session is told to use `select:` batch-load on first turn. The previous run's data wasn't reviewed for this finding; if this fired silently before, it's been a sustained tax. Layer source: atom or guidance text that mentions `select:` in `internal/content/atoms/bootstrap-toolsearch-batchload.md` or similar.

2. **[BROKEN-soft / RESPONSE-CONFUSING] Same `zerops_deploy` call returns different response shapes for two consecutive code-changes** — First deploy on `appdev`: `status: DEPLOYED` synchronously in ~60s. Second deploy (added `/version`): `status: BUILD_TRIGGERED` + `timedOut: true` + polling required. Same tool, same service, completely different lifecycle. Transcript confirms both shapes in same session (UUIDs `35d6b5bb` and `0fd7fbac`). Agent quote: "I don't know what flipped — possibly the channel was busy from the dev-server restart I'd done seconds earlier, possibly it's just nondeterministic past some threshold." Layer: `internal/tools/deploy_ssh.go` / `internal/ops/deploy.go` — synchronous-vs-async timeout boundary must be either (a) deterministic from request shape, or (b) explicitly documented. Async polling is an acceptable mode; the surprise is the lack of predictability. Cross-session: yes — any develop loop with multiple deploys can hit this; agent flagged the lesson "don't tie 'task done' signal to deploy response."

3. **[GUIDANCE-MISLEADING] Dev-mode code-live signal is `zerops_dev_server action=restart`, not deploy completion** — Agent: "the code change goes live the moment you `zerops_dev_server action=restart`, not when the deploy completes. I caught myself waiting on the deploy as if it were the gating event for the user — it isn't." Layer: develop-loop atom — code-live vs durable-rebuild distinction needs to lead the second-deploy guidance, not be buried. Cross-session: yes — develop-add-managed-dep had analogous dev-server-after-deploy dance, but here it's the timing-of-user-signal flavor.

4. **[GUIDANCE-MISLEADING] `zerops_verify` reports `http_root: pass` on HTTP 404** — Same as `api-node-postgres-classic-dev` finding #6. "if you skim past it you'll think something's wrong. The check status `pass` is what matters, not the HTTP code." Layer: `internal/ops/verify_http.go`. Cross-session: yes — two sessions in this group.

5. **[ATOM-CONTENT] `connectionString` `/dbName`-suffix warning correctly internalized** — Agent: "If you use `connectionString` directly with Prisma/Drizzle/Sequelize you'll get a silent connection to the wrong database. For raw `pg.Pool` I sidestepped this by passing host/port/user/password/dbName as separate fields." Atom guidance worked. Positive signal.

6. **[GUIDANCE-OVERLOAD] TodoWrite reminders fire on every turn regardless of task complexity** — "For a short linear task like this one, a todo list is overhead, not value." Layer: harness (not zcp's domain unless we hook into reminders). Cross-session: low priority for zcp but worth recording.

---

## Group-level patterns

- **`ToolSearch select:` batch-load is broken or wrongly documented** — `develop-loop-after-bootstrap` (NEW) hit this hard. Bootstrap guidance prescribes the comma-separated form verbatim; the agent followed it and got a dead-end error. This is a hard guidance bug — every fresh-bootstrap agent is being told to make a call that fails. Either fix the tool-search harness to accept the documented form, or fix the guidance to use `mcp__zerops__` prefix / single keyword queries. **Highest-severity finding in the group.**

- **`zerops_deploy` response-shape nondeterminism between sync (`DEPLOYED`) and async (`BUILD_TRIGGERED` + `timedOut`)** — same call site, same service, two consecutive code edits, two completely different lifecycles. develop-loop-after-bootstrap surfaces this. The async mode is fine in principle; the lack of upfront predictability turns it into agent surprise. Worth deciding the threshold (max-wait time? always-async after first deploy?) and documenting it.

- **`zerops_verify` `http_root: pass` despite HTTP 404 is repeating** — Two sessions in this group (`api-node-postgres-classic-dev`, `develop-loop-after-bootstrap`) call out the same misleading-pass surface. Verify is reporting reachability, not endpoint-correctness. Either rename the check, split into two, or make HTTP-code part of the user-facing summary.

- **Adopt-pair plan-shape inference gap is now partially mitigated by guidance but still trips agents** — `adopt-existing-standard-pair` (NEW) and `develop-add-managed-dep-to-existing` (RETURNING) both report inference work; both succeed on first try thanks to guidance text. Underlying schema asymmetry (`isExisting: true` on runtime vs. `resolution: "EXISTS"` on deps) remains. Guidance band-aid is holding; structural fix (unify schema or rename for clarity) deferred.

- **Develop workflow's first response (30+ sections of guidance) consistently flagged as overload** — Two sessions explicitly call out volume drowning signal. Likely candidate: route-specific guidance bundles instead of monolithic stack.

- **Postgres env-var `dbName` casing remains a typo trap** — `api-node-postgres-classic-dev` and `develop-loop-after-bootstrap` both flag the case-mix (`${db_dbName}`) and the silent-literal-at-runtime failure mode. `connectionString` `/dbName`-suffix warning is well-surfaced; the camelCase trap is not.

---

## Comparison to previous run

(Only 2 of 5 sessions returning: `cross-deploy-stage-promote-from-dev` and `develop-add-managed-dep-to-existing`.)

- **`/var/www/` mount-claim friction REGRESSED** — Previous-run group-level top fix. `cross-deploy-stage-promote-from-dev` self-review still flags it ("`ls /var/www` only showed `CLAUDE.md`"). Same surface in `claude_container.md:3` — unfixed. `develop-add-managed-dep-to-existing` did NOT re-flag the mount, but it ran the bootstrap-adopt path which materializes the mount before user-facing work, so the friction may have been silently absorbed rather than fixed.

- **Stale `launch-active` envelope from `status` IMPROVED or absent** — Previous-run finding for cross-deploy ("`kind: launch-active` for an unrelated `api-prod` workflow… completely unrelated to the dev→stage promotion") does NOT appear in this run's self-review for the same scenario. Either eval-zcp project state was cleaner this run, OR launch-status-recovery priority was tightened. Confirm by checking `internal/tools/launch_status_recovery.go` history.

- **`setup`-required preflight error IMPROVED via classifier copy** — Previous run flagged the error as expensive to recover from. This run's self-review explicitly says "the error itself is actually helpful — `failureClassification.suggestedAction` tells you exactly what to do." `setup` field naming is still field-shape-confusing but the recovery loop is now classifier-driven.

- **Adopt-pair discover plan-shape inference IMPROVED by guidance** — Previous run reported as `[GUESS-SHAPE]` in three sessions ("got lucky on the first try"). This run, `develop-add-managed-dep-to-existing` says "I got it right on first try only because the guidance called this out — without that warning I would have flat-set them." Guidance landed; schema asymmetry persists but is now signposted.

- **`/api/dashboard` fixture mismatch UNFIXED** — Previous run finding #1 for `develop-add-managed-dep-to-existing`. This run, same scenario, same fabrication: "there is no actual `/api/dashboard` endpoint in the existing code." Fixture file `eval/behavioral/scenarios/fixtures/` still hasn't been updated.

- **NEW high-severity finding from `develop-loop-after-bootstrap` (new scenario): `ToolSearch select:` batch-load is broken** — Previous run did not surface this because no scenario reviewed in detail exercises the bootstrap-guided batch-load + subsequent retry path. This is a hard guidance bug requiring code or text fix; ranks above any returning-scenario finding in this group.
