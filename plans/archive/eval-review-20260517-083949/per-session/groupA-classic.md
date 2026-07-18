# Group A: classic-* sessions (6 scenarios)

Run dir: `/Users/macbook/Documents/Zerops-MCP/zcp/eval/behavioral/runs/20260517-083949/`

All 6 sessions reached `terminatedBy: agent_declared_done` (or `user_sim_satisfied` for rust). **No catastrophic failures, no recovery dead-ends, no incoherent tool responses.** The reportable findings are friction-class — guidance/atom/response-shape issues — most of which recur from the previous run. Where a previous-run finding did NOT appear here, it is called out as `[SMOOTH]`.

---

## Session: classic-bun-simple
**Task:** Set up a small Bun HTTP service for me on Zerops. Just one container, nothing fancy — no database, no staging.
**Duration:** 3m04s wall (2m38s scenario)
**Findings (by severity):**

1. **[ATOM-CONTENT] `fitExtras` field is the load-bearing signal in the route menu but agent had to find it** — the recipe (`bun-hello-world`) is the visually obvious match; agent flagged that the under-`fitExtras` annotation is what tells you "this brings PostgreSQL + dev/stage pair you didn't ask for." Calls out the dev/simple/standard labels themselves as not telegraphing the topology distinction.
   - Layer: route-menu envelope shape, `internal/workflow/bootstrap_*.go` route-menu assembly; atom `bootstrap-route-options.md`.
   - Cross-session signal: yes — go and python both flag the same trap.

2. **[ATOM-CONTENT] Generic `zerops.yaml` example doesn't reflect dev-mode `start: zsc noop --silent` rule** — dev-mode dynamic runtimes need `start: zsc noop --silent` and NO `healthCheck`. Real start command goes into `zerops_dev_server`. Guidance says it but in a separate develop response that's "easy to miss" against the generic scaffold example.
   - Layer: atom `develop-first-deploy-scaffold-yaml.md`, `develop-dynamic-runtime-start-container.md`.
   - Cross-session signal: yes — previous run also flagged this for bun/go/python/rust.

3. **[RESPONSE-CONFUSING] Post-deploy 502 on dev-mode is expected but reads as failure** — verify after deploy reports `http_root: HTTP 502`; agent had to know to run `dev_server start` BEFORE verify. The sequence "deploy → dev_server → verify" is mentioned in guidance but not as front-loaded as it should be.
   - Layer: deploy result `nextActions` prominence; atom on dev-mode deploy-vs-running.
   - Cross-session signal: yes — python flagged identically.

4. **[GUIDANCE-MISLEADING] Recipe-template `importYaml` includes top-level `project:` block that `zerops_import` rejects** — `IMPORT_HAS_PROJECT`. Same source-data trap the previous run flagged.
   - Layer: recipe response `importYaml` field shape (`internal/recipe/` or recipe content), atom on import preflight.
   - Cross-session signal: yes — recurring across all recipe-adjacent classic sessions.

5. **[ATOM-CONTENT] `mode:` field clarification — managed-service only** — agent flagged that `mode:` on a runtime service in the import YAML is invalid; it belongs only on managed services (NON_HA/HA). Worth confirming the import schema description for the field calls this out.
   - Layer: import schema annotation (`internal/platform/...`), atom on classic-mode import YAML.
   - Cross-session signal: medium.

6. **[ATOM-CONTENT] Plan-schema nesting (`bootstrapMode`/`stageHostname` inside `runtime`) still surfaces** — same as every previous classic session, agent succeeded because the description "shouts about it" but flags it as a recurring trap.
   - Layer: atom `bootstrap-classic-plan-dynamic.md` JSON-example prominence.
   - Cross-session signal: yes — every classic session in both runs.

---

## Session: classic-go-simple
**Task:** Set up a small Go HTTP service for me on Zerops. Just one container, nothing fancy.
**Duration:** 3m44s wall (24s scenario, 47s retro — note: scenario time low because user-sim turn dominated; agent action was quick)
**Findings (by severity):**

1. **[ATOM-CONTENT] Go runtime: no documented port convention** — agent had to guess port 8080 (sensible default for `net/http` but nothing in guidance, catalog, or atoms says so). Subdomain URL came back with `-8080` baked into the host (`appdev-227a-8080.prg1.zerops.app`); agent flagged this should be documented up front.
   - Layer: atom on Go runtime defaults, or generic dynamic-runtime port-convention atom.
   - Cross-session signal: low — Go-specific, but the general "no per-runtime port hint" pattern is broader.

2. **[ATOM-CONTENT] Go dev-mode iteration story is broken — `buildCommands` baked binary + `./app` exec means restart re-runs old artifact** — agent's literal observation: "I picked the worst of both." Real options for Go dev are `command: "go run ."` in dev_server (source picks up on restart) OR accept that every code change needs `zerops_deploy` again. The atoms don't pick one explicitly for Go.
   - Layer: atom on Go dev-mode dev-server command (`develop-dynamic-runtime-start-container.md` per-runtime guidance, or recipe knowledge for Go).
   - Cross-session signal: medium — analogous to python's "vendored gunicorn not on PATH" — both are "real start command for dev mode is non-trivial per runtime."

3. **[ATOM-CONTENT] Plan-schema: `dependencies: null` vs `[]` vs omitted not documented** — agent passed `null` and it worked but flagged the ambiguity. Small detail but recurs.
   - Layer: plan-schema description in `internal/workflow/`, atom `bootstrap-classic-plan-dynamic.md`.
   - Cross-session signal: low.

4. **[RESPONSE-CONFUSING] Verify response: `enabled: false` on work-session block reads as failure on otherwise-healthy run** — agent quote: "almost re-ran something." The `reason` field (close-mode unset) is in the right place but `enabled: false` next to green checks creates a momentary "did something break?" parse. Field wording or layout problem, not a real bug.
   - Layer: `internal/workflow/` work-session envelope, `enabled` field naming/placement.
   - Cross-session signal: yes — php and rust both flagged the close-mode auto-close noise; same root cause.

5. **[GUIDANCE-MISLEADING] ToolSearch batch-preload guidance is per-step, not pre-emptive** — workflows tell you which tools to load AFTER the workflow start; agent paid two round-trips (one before bootstrap, one before deploy). Suggests pre-emptive full-set preload at session entry would save a round-trip.
   - Layer: bootstrap-tool-preload guidance, possibly `internal/workflow/` start envelope.
   - Cross-session signal: yes — static-nginx flagged the same.

6. **[SMOOTH] Route-menu `over-provisions` annotation worked as intended** — `go-hello-world` recipe was first in menu, agent skipped it cleanly on the annotation. Previous run had a similar finding but agent there needed two reads; here it was a one-shot. Suggests the prominence is closer to correct.

---

## Session: classic-php-mariadb-standard
**Task:** I want to deploy a PHP web app backed by MariaDB. I need both a development environment and a staging slot for testing builds.
**Duration:** 6m14s wall (27s scenario; user-sim took ~5m on this one)
**Findings (by severity):**

1. **[BROKEN] Build-base catalog is wrong/incomplete for PHP — `php@8.4` is undocumented but works; `php-nginx@*` is documented as a stack but is run-only** — agent's first deploy used `build.base: php-nginx@8.4` (mirroring the run base, "natural"), got `INVALID_ZEROPS_YML` with `unknown base php-nginx@8.4`. Documented build-only list was `php@{7.4,8.0,8.1,8.3}` (no 8.4); hello-world example used `php@8.5`. Agent fixed on a guess (`php@8.4`) which worked. The catalog is stale or wrong, and the runtime-vs-build-base distinction is not flagged on the `php-nginx@*` entries.
   - Confirmed in transcript: 3 separate `INVALID_ZEROPS_YML` results with `unknown base php-nginx@8.4` text.
   - Layer: `zerops_knowledge` catalog for PHP, or wherever the stacks listing comes from (`internal/knowledge/...`).
   - Cross-session signal: probably PHP-specific surface but the "runtime ID is also a valid build base" assumption is universal.

2. **[ATOM-CONTENT] Hello-world template includes composer/`vendor/` that's wrong for plain PDO apps** — agent almost cargo-culted `composer install` + `vendor/` deployFile entry for an app with no `composer.json`. Hello-world is presented as a contract, not a starting point.
   - Layer: recipe knowledge `php-apache.md` / `php-nginx.md`, atom on PHP minimal scaffold.
   - Cross-session signal: medium — similar template-vs-reality issue surfaced in previous run for php env-var parity.

3. **[ATOM-CONTENT] `php-nginx` doc-root behavior and `.php` URL extensions undocumented** — agent guessed: dropped `index.php` + `status.php` in root with no nginx config or `documentRoot` setting, used `status.php` (with extension) for readiness. Worked but agent says "I guessed." Knowledge should say: files at project root serve fine without document-root config; URLs include `.php` unless you add rewrites.
   - Layer: recipe knowledge `php-nginx.md`, atom on implicit-webserver document-root semantics.
   - Cross-session signal: low — runtime-specific.

4. **[RESPONSE-CONFUSING] Cross-deploy `setup="prod"` field name reads ambiguously** — agent had to re-read to confirm `setup="prod"` names the **YAML block name** (not the target hostname or Zerops mode). Schema description does spell it out but the field name itself reads as "which environment am I targeting."
   - Layer: `internal/tools/deploy_local.go` / `deploy_ssh.go` setup-arg description.
   - Cross-session signal: yes — rust flagged the same setup-arg friction from a different angle.

5. **[ATOM-CONTENT] Close-mode auto-close gating not internalized from initial response** — workflow start response said `closeMode=unset` for both services with a reason, but agent didn't internalize until after both deploys still left session `status: open`. Agent recommendation: set close-mode right after workflow start, not at the end. This is recurring noise on every session.
   - Layer: workflow-start envelope `nextActions` prominence for `closeMode`, atom on close-mode workflow ordering.
   - Cross-session signal: yes — bun, go, rust, php all flagged the same noise.

6. **[SMOOTH] No PREFLIGHT_FAILED on env parity** — previous run flagged `PREFLIGHT_FAILED` on identical envVariables in dev/prod from the template. Not repeated here — either the agent inserted an `APP_ENV` differentiator (the self-review doesn't mention env, suggesting it wasn't a friction point), or the template was fixed.

7. **[SMOOTH] No `/status` + `.htaccess` rewrite trap** — previous run had a hard failure on PHP readiness check `/status` because Apache 404'd without `.htaccess` rewrite. Here agent used `status.php` (with extension), bypassing the issue. Different framework (nginx vs apache) so not strictly the same scenario, but the readiness-fail-misclassified-as-init category did not recur.

---

## Session: classic-python-postgres-dev-only
**Task:** Set up a Python web service with a Postgres database for me. Just a development environment, no production stage needed.
**Duration:** 4m51s wall (4m20s scenario)
**Findings (by severity):**

1. **[BROKEN] `pip install --target=./vendor` + `PYTHONPATH=/var/www/vendor` doesn't put `gunicorn` on PATH; first `dev_server start` failed with `env: 'gunicorn': No such file or directory`** — transcript confirms multiple `gunicorn` + `env:` error fragments. Agent recovered on second try by using absolute path `/var/www/vendor/bin/gunicorn`. **Same finding as previous run** — points at unchanged Python recipe knowledge / atom guidance.
   - Layer: recipe knowledge `python.md`, atom on dev-mode dev-server command for Python (or generic "vendored binary path is `vendor/bin/` not `vendor/`").
   - Cross-session signal: yes — Go and Rust hit analogous "real dev start command per runtime" issues.

2. **[ATOM-CONTENT] `dev_server.command` exec-not-shell semantics easy to miss** — `KEY=VAL cmd` shell-assignment syntax doesn't work; must write `env KEY=VAL cmd`. Tool description has the warning; agent got it right on second try. Worth front-loading.
   - Layer: `internal/tools/dev_server.go` description on `command`, atom on dev-server command shape.
   - Cross-session signal: medium.

3. **[ATOM-CONTENT] Plan-schema nesting (`bootstrapMode`/`stageHostname` inside `runtime`) — buried in prose** — same recurring trap. For dev-only, `bootstrapMode: "dev"` + omit `stageHostname` entirely (not empty string, not null). Agent flagged the JSON example is the source of truth.
   - Layer: atom `bootstrap-classic-plan-dynamic.md`.
   - Cross-session signal: every classic session in both runs.

4. **[GUIDANCE-MISLEADING] Recipe vs classic route choice for "no stage"** — `python-hello-world` recipe ships dev+stage pair; user said "no production stage needed." Agent picked `classic` to honor literally but flagged it's a soft judgment call — the menu doesn't surface the topology-lock trade-off.
   - Layer: route-menu annotations.
   - Cross-session signal: yes — go also flagged the over-provisions recipe trap.

5. **[GUIDANCE-MISLEADING] Recipe `importYaml` top-level `project:` block is rejected by `zerops_import`** — agent flagged the recipe preview and the provision-step warning live in different atoms, so the two pieces of guidance look contradictory until reconciled.
   - Layer: recipe response `importYaml` shape, atom on import preflight.
   - Cross-session signal: yes — bun also flagged this; recurring trap across both runs.

6. **[RESPONSE-CONFUSING] Verify `error_logs` `status: info` carries `systemd-machine-id-commit.service failing` text — reads as a real problem** — agent quote: "I read that twice — it sounds like a real problem. It's not." `info` is advisory but the prose looks alarming.
   - Layer: `internal/ops/verify_checks.go` (filter or de-emphasize known-benign platform-level logs) or `info`-status visual treatment in verify envelope.
   - Cross-session signal: yes — previous run flagged the same `/etc/fstab` noise for php-apache; same class of issue.

7. **[RESPONSE-CONFUSING] Dev-mode 502-after-deploy expected** — same finding as bun. `nextActions` field tells you, but it's easy to misread the deploy success + URL as "app is running."
   - Layer: deploy result `nextActions` prominence.
   - Cross-session signal: yes — bun.

---

## Session: classic-rust-postgres-standard
**Task:** Build me a small Rust HTTP API on Zerops, with Postgres. I want a dev environment plus a stage slot — and yes, I want to be able to iterate on dev fast.
**Duration:** 19m03s wall (18m04s scenario — long because cold cargo build of actix-web + tokio-postgres)
**Findings (by severity):**

1. **[GUIDANCE-MISLEADING] Develop workflow "Next" hint drops `setup=` for self-deploy when recipe ships multi-setup `zerops.yaml`** — **same finding as previous run, NOT FIXED.** Envelope said `zerops_deploy targetService="appdev"` (no setup); deploy tool requires `setup=` when YAML has multiple setup blocks; recipe ships `dev` and `prod`. Cross-deploy hint right below it correctly included `setup="prod"`. Agent rescued by reading `zerops.yaml` first and inferred `setup="dev"`.
   - Confirmed in transcript: agent's first `zerops_deploy` call was `{"targetService":"appdev","setup":"dev"}`, not from the workflow hint.
   - Layer: `internal/workflow/` develop synthesize logic — primary-action plan for self-deploy in recipe routes. Previous-run analysis pointed at `aggregate_render_probe_test.go:88`-area hint construction.
   - Cross-session signal: high — recipe-route develop is hit in many scenarios.

2. **[ATOM-CONTENT] `dev_server.waitSeconds` default 15 insufficient for Rust cold compile** — first `cargo run` compiles deps inside the container. Agent set 45 pre-emptively. Recipe `CLAUDE.md` mentions cold-compile but doesn't put a number on it. Same finding as previous run, not fixed.
   - Layer: `internal/tools/dev_server.go` waitSeconds default + atom `develop-dev-server-reason-codes.md`.
   - Cross-session signal: medium — Rust/Java/native-compile runtimes will all hit this.

3. **[RESPONSE-CONFUSING] `zerops_import` response: `stack.build` for `appstage` showed `PENDING` while summary said "All 7 processes completed successfully"** — visually contradictory at first glance. By the time agent polled `zerops_discover`, the stack was `ACTIVE`. Agent's interpretation: a response-assembly snapshot taken before the final transition. Suggests the response should either wait one beat or filter `PENDING` entries that already transitioned by aggregation time.
   - Layer: `internal/ops/import.go` response assembly, or process-snapshot ordering.
   - Cross-session signal: medium — could surface in any multi-process import.

4. **[ATOM-CONTENT] `db_connectionString` missing `/${dbName}` for named libraries** — env catalog has the warning attached but agent flagged: any future agent writing custom Rust against sqlx/diesel and reaching for `${db_connectionString}` will hit a runtime connect failure that looks like auth. Recipe sidesteps via individual `DB_HOST/PORT/USER/PASS/NAME` env vars.
   - Layer: env-catalog atom on `connectionString` warning prominence.
   - Cross-session signal: medium.

5. **[ATOM-CONTENT] Close-mode auto-close noise on every response** — same as classic-go #4, php #5. Agent flagged it explicitly as "friction tax I paid by setting it last." Strong vote for surfacing this at session entry.
   - Layer: workflow envelope, close-mode auto-prompt atom.
   - Cross-session signal: yes — bun, go, php all flag.

6. **[SMOOTH] No two-phase bootstrap `start` (route-menu → session-active) confusion this session** — previous run flagged it. Self-review doesn't mention it as a friction point; the run flow was clean.

---

## Session: classic-static-nginx-simple
**Task:** Put up a small static landing page for me using nginx. Just HTML, no backend.
**Duration:** 3m30s wall (3m03s scenario)
**Findings (by severity):**

1. **[GUIDANCE-MISLEADING] Tool-preload batching is per-step, not pre-emptive** — same as classic-go #5. Bootstrap requires loading `zerops_workflow` first; only after `start` returns can the agent see which downstream tools are needed. Agent paid two round-trips. Recommends pre-loading the full runtime set at the very start.
   - Layer: `bootstrap-tool-preload.md` atom, workflow-start envelope.
   - Cross-session signal: yes — go.

2. **[RESPONSE-CONFUSING] Two-phase bootstrap `start` `kind=route-menu` vs `kind=session-active`** — agent flagged it's easy to miss if you skim; the response is explicit but pattern-matching on "I called start, I got a response, now I `complete`" leads to a malformed call.
   - Layer: workflow envelope, route-menu prose.
   - Cross-session signal: yes — previous run flagged in 4 of 6 classic sessions; here only 1 of 6 mentions it, suggesting the envelope's `kind` field discriminator is becoming legible. Recurrence likelihood seems lower.

3. **[ATOM-CONTENT] Plan-schema nesting (`bootstrapMode`/`stageHostname` inside `runtime`)** — same recurring trap. Agent reads the existence of the "shouted" warning as evidence agents get it wrong often enough to need it. Calls the JSON example the source of truth.
   - Layer: atom `bootstrap-classic-plan-dynamic.md`.
   - Cross-session signal: every classic session in both runs.

4. **[ATOM-CONTENT] `build.base` for static-nginx must be a real builder runtime (e.g. `nodejs@22`), NOT `nginx` or `static` despite both being in schema enum** — atom calls it out explicitly ("Zerops rejects `static`/`nginx` as build bases — `unknown base`"), so docs save you. But agent flagged: if you trusted the enum and skipped the atom, you'd waste a deploy. Either the enum should be tightened or the atom warning surfaced inline at build-base selection.
   - Layer: static-runtime atom, possibly schema enum restriction in `internal/platform/...`.
   - Cross-session signal: medium — applies wherever schema enum is wider than truth.

5. **[GUIDANCE-OVERLOAD] Develop response dumps many KB of guidance, ~95% irrelevant for a static HTML page** — same as previous run. Load-bearing block was a small "Static runtime — develop workflow" section in the middle. Atoms are dense but accurate; agent's meta-lesson is "skim for the matching section, ignore the rest."
   - Layer: `internal/workflow/` develop synthesis — runtime-class + topology-aware filtering.
   - Cross-session signal: yes — every minimal-scope classic session in previous run flagged similar; less prominent this run, suggesting either smaller payload or agents handling it better.

---

## Group-level patterns

- **Close-mode auto-close noise is the most-mentioned friction across the group (4 of 6: bun, go, php, rust).** Every response on every step says "auto-close blocked" until the agent sets close-mode. Every agent recommends setting close-mode at workflow start. The fix is either auto-default close-mode to `auto` on standard topologies, or front-load the close-mode prompt to the workflow-start envelope's `nextActions` (not buried in `workSessionState.progress.reason`).
- **Recipe vs classic route trade-off is still un-annotated for topology mismatches** (bun, go, python all flagged). Recipes lock you into their topology (dev/stage pair, extra managed services) and the menu's `fitExtras`/`over-provisions` annotation exists but is one-of-many small fields. Cross-session: the annotation IS load-bearing for "minimal scope wanted, recipe over-fits" decisions.
- **Per-runtime "real dev start command" is non-trivial and inconsistently documented** (bun: `bun run index.ts`; go: `go run .` vs `./app`; python: `/var/www/vendor/bin/gunicorn` not `gunicorn`; rust: cold-compile waitSeconds bumped to 45). The general atom on `develop-dynamic-runtime-start-container.md` is correct; per-runtime gotchas live (or don't) in recipe knowledge files. Best fix: a per-runtime-class table in the develop envelope, not scattered atoms.
- **PHP build-base catalog is broken in 1 of 6 sessions ([BROKEN])** — `INVALID_ZEROPS_YML` on `php-nginx@8.4` because it's a run-only stack. Documented build-base list (`php@{7.4,8.0,8.1,8.3}`) is stale; `php@8.4` works but isn't listed. This is the one hard-fail in the group and the only one that warrants `[BROKEN]` tag. Recovery was clean (one re-try) so user impact is small, but the catalog is wrong.
- **Plan-schema nesting (`bootstrapMode`/`stageHostname` inside `runtime`)** is mentioned by every single classic session in both runs but every session also succeeds on it. The hard-reject + actionable error is doing its job; the upstream atom-prominence work is still pending but the path is clear.

## Comparison to previous run

- **Two-phase bootstrap `start` (route-menu → session-active) confusion is sharply reduced** — previous run flagged it in 4 of 6 classic sessions; this run only static-nginx mentions it as a residual friction. The `kind` field discriminator may have become more prominent, or agents are pattern-matching better. Either way: probable improvement.
- **No new `[BROKEN]` failures from this run vs. previous** — previous run had `[BROKEN]` on PHP readiness check `/status` → 404 misclassified as `failedPhase: init` (recovery dead-end at the time). Not repeated here (agent used `status.php` with extension, bypassing the `.htaccess` requirement). The PHP build-base catalog issue is new but the recovery was clean.
- **Rust workflow-hint `setup=` drop is unchanged** — `[GUIDANCE-MISLEADING]` regression-status: NOT FIXED. Same envelope output, same workaround (read `zerops.yaml` first). Logical fix from previous run analysis (`internal/workflow/` synthesize, around the aggregate-render-probe path) remains open.
- **Python `gunicorn` PATH gotcha unchanged** — same one-shot recovery, same root cause (recipe knowledge / atom showing `gunicorn` not `vendor/bin/gunicorn`). NOT FIXED between runs.
- **Guidance overload on develop step still flagged in static-nginx and bun** but less prominently — previous run flagged it as 200+ lines / ~95% irrelevant; current static-nginx review reads more pragmatic ("skim for the matching section"), suggesting either payload trim or learned coping.
- **No close-mode-auto-close prompting at workflow start** — close-mode noise was a recurring friction in the previous run too; this run it's now the most-mentioned issue across the group (4 of 6). Either the noise grew more visible, or it's the residual friction left after other things got fixed.
