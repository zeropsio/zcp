# FIX-SPEC — guide + corpus correctness sweep (2026-06-02)

Scope B: fix all guide errors + reconcile stale corpus surfaces they expose. NOT recipe-scope (`internal/content/workflows/recipe*`, `internal/recipe/`, `internal/tools/workflow_recipe*`) — flag only. Don't touch defensive handler logic / pinning tests (dash→underscore discover logic, etc.).

Evidence basis: live (LEDGER.md), docs (../zerops-docs), corpus (consistency-findings.md), Codex review. Live facts established this session:
- Gleam runs on Alpine (alpine/gleam@1.5.1 ACTIVE) — only **Deno** is ubuntu-only.
- valkey@8 rejected `serviceStackTypeNotFound`; use valkey@7.2.
- non-zero initCommands → deploy FAILS ("RUN.INIT COMMANDS FINISHED WITH ERROR", appVersion not activated).
- health-check live defaults: failureTimeout 3m0s, execPeriod 30s, disconnect/recovery 1m30s.
- Valkey connString `redis://gcache:6379` (no user/password); Postgres `postgresql://db:…@gdb:5432` (no /db; db-name var = dbName).
- shared-storage + verticalAutoscaling ACCEPTED; object-storage REJECTED.
- `zsc cdn purge domain [path]`; `--auto-disconnect`="disconnect if already connected"; `--show-build-logs` kebab; `zcli service log` positional rejected.
- `${BUILD_x}` build→runtime stays literal; `${RUNTIME_x}` runtime→build works.

## GUIDES — checklist (status: [ ] todo / [x] done)

### CRITICAL
- [ ] **choose-cache** — L17 connString `redis://${user}:${password}@${hostname}:6379` → `redis://${hostname}:6379` (unauthenticated, no user/password vars). L15 "HA: 3 nodes" + L9 "full HA" → note default NON_HA (immutable); HA opt-in. L16 ports 7000/7001 = HA-only replica reads (qualify). Add: use valkey@7.2 (v8 rejected `serviceStackTypeNotFound`).
- [ ] **smtp** — L24-31 bare top-level `envVariables:`/`envSecrets:` → reframe: SMTP_HOST/PORT/USER in `run.envVariables` (zerops.yaml); SMTP_PASSWORD in `envSecrets` (import.yaml service-level) — different files; note restart-on-secret-change.
- [ ] **production-checklist** — L127 "minContainers:2 … for zero-downtime deploys" → decouple (zero-downtime is platform default temporaryShutdown:false at ANY minContainers; ≥2 is for throughput/crash-tolerance). L3 item(2) keep but fix rationale. L36 Mailpit `type: go@1`→`alpine@3.20`. L39-44 bare envVariables → run.envVariables/envSecrets scope. L149 "5-minute retry window" → configurable failureTimeout (health default ~3m).
- [ ] **verify-web-agent-protocol** — (DO MYSELF, check tests) rewrite raw `agent-browser open/snapshot/eval` → `zerops_browser` (no open/close, no eval; dedicated commands; errors/console auto-appended). Add verify-Recovery loop (execute check.recovery + re-verify before FAIL).
- [ ] **local-development** — L3+L40+L101 ".env from zerops_discover" → `zerops_env action="generate-dotenv"`. L42 `db_host`→`db_hostname`. L45 connString drop `/db` (or note dbName). Add `.env.local` override channel + refuse-on-unowned-edit gate.
- [ ] **cdn** — L49-52 `zsc cdn purge /*` → `zsc cdn purge <domain> "/*"` (domain required first). Note: zsc purge = Static-Mode CDN only; Object-Storage CDN purges via REST API. L19/24/30 note CDN urls are project-scoped (no hostname prefix).

### MAJOR
- [ ] **environment-variables** — L30-33 BUILD_ row: `${BUILD_x}` build→runtime does NOT resolve (build vars not persisted to runtime); keep RUNTIME_ (works). L66 dash→underscore `my-db`: hostnames are `[a-z0-9]` only (no dash/underscore/uppercase) — reframe (the transform never triggers; teach the charset). L161 secret "overridden by yaml" → rejected (`userDataDuplicateKey`) — align with L20/L215. L209 PATH "editable" → PATH is hard-reserved (`userDataUseOfSystemKey`); editable system vars are envIsolation/sshIsolation/zeropsSubdomainHost.
- [ ] **scaling** — L22+L191 shared-storage verticalAutoscaling "import failure" → WRONG (accepted; only object-storage rejected). L40 minFreeCpuCores fraction 0.0-1.0 — keep but clarify NOT percent (0.2=20%; passing 20 = 100×). Docker horizontal: docs group Docker under auto horizontal — soften "no autoscaling at all". L104 minContainers system floor=1 (range phrasing).
- [ ] **zerops-yaml-advanced** — L43 `start: zsc noop --silent` retired → dev dynamic omits `run.start` entirely (blocking gate). L69 crontab `allContainers` is schema-REQUIRED. L90 startCommands `name` optional (schema requires only `command`). L180 "Deno, Gleam REQUIRES os: ubuntu" → only Deno (alpine/gleam exists).
- [ ] **backup** — L43 MariaDB `mysqldump`→`mariabackup` (.xb.gz); L42 PostgreSQL pg_dump → format `.zip`; add ClickHouse (native `BACKUP`, .tar.gz — not in supported auto-backup list). Keep schedule/X25519 (live-confirmed).
- [ ] **choose-database** — L3/L9/L15 PostgreSQL HA-default framing → default NON_HA single (HA opt-in, immutable). L16/L18 port 5433 = HA-only (qualify). L17 connString drop `/${db}`→note connectionString omits db; append `/${db_dbName}` if driver needs it; `${db}`→`${dbName}`. L24 MariaDB connString drop `/${db}`. L31 ClickHouse engine nuance (minor).
- [ ] **choose-runtime-base** — L3/L10 switch axis "system packages→Ubuntu" → glibc/musl/CGO/C-ext/Deno (apk installs on Alpine). L18 add Deno is ubuntu-only (NOT Gleam — gleam on alpine confirmed). L17/L24 `apk add`/`apt-get install` → add `sudo`. L25 ubuntu 22.04 also available (minor).
- [ ] **cloudflare** — L55 "zerops_subdomain enable only works on ACTIVE" = invented precondition; real cause = HTTP-port shape. Add deploy auto-enables subdomain (first deploy, eligible modes). L64 gotcha5 reattribute serviceStackIsNotHttp to port-shape not deploy-state. L34 "Full (strict) mandatory" → docs allow "Full" for testing (keep never-Flexible).
- [ ] **php-tuning** — split restart/redeploy by channel: `run.envVariables` PHP_INI/FPM are baked → CHANGE needs REDEPLOY (not restart). `zerops_env`/GUI service env → restart. L3/L9/L43/L129 reword. (Keep defaults table.)
- [ ] **logging** — L18-19 `zcli service log <name>` → `-S/--service-id` (positional rejected live); `--showBuildLogs`→`--show-build-logs`. L57 gotcha same. Add: agent reads runtime logs via `zerops_logs` MCP (runtime-only; build logs only via deploy-failure auto-attach).
- [ ] **vpn** — L8 `--auto-disconnect` comment "on terminal close" → "disconnect if VPN already connected (so a fresh up proceeds)". L31 WSL `systemd=true` + `wsl --shutdown` (minor).
- [ ] **ci-cd** — L40+L47 `ci skip`/`skip ci` → `[ci skip]`/`[skip ci]` (brackets). L22 `zeropsio/actions@main`→`@v1.0.2` + note compact wrapper can't pass `--setup` (multi-setup needs raw `zcli push --setup`). Add gotcha: external/CI deploy leaves ZCP `never-deployed` → `zerops_workflow action="record-deploy"`.
- [ ] **metrics** — L22 `ELASTIC_APM_SERVER_URL` hardcoded → copy real subdomain from GUI. L19 bare `envVariables:` → reframe as service env (GUI/run.envVariables). L42 pg_stat_statements nuance (some metrics; superuser CREATE EXTENSION + restart). L37 prometheus port = your /metrics port (example).

### MINOR
- [ ] **choose-queue** — L43 remove "Schema Registry: Port 8081 (if enabled)" (uncorroborated; not in Kafka docs/corpus card).
- [ ] **choose-search** — L26 PLUGINS set via `envSecrets` (clarify location).
- [ ] **deployment-lifecycle** — timeline item3 (`appVersion ACTIVE`="deploy started/launching") → ACTIVE is terminal deployed state. Gotcha3 "initCommands failures do NOT cancel deploy" → REFUTED: non-zero init FAILS deploy (appVersion not activated). SSHFS "no remount needed" → soften: auto-reconnect only when service running; remount (`zerops_mount action=mount`) is the recovery for stale mount. Readiness defaults (5s/300s) = behavior not schema-pinned (soften).
- [ ] **public-access** — 512MB = configurable default (2048m ceiling) not hard limit (minor). dedicated-IPv4 reuse nuance (minor).
- [ ] **object-storage-integration** — apiHost is CORRECT (live) — KEEP. No change (optionally note region ignored already there). NO-OP unless minor.
- [ ] **networking** — keep service-mode (correct). L39 `${app_API_TOKEN}` example fine (app valid hostname). Minor: nginx defaults table is docs-config (leave). NO substantive change beyond consistency.
- [ ] **firewall**, **build-cache** — clean, no findings.

## CORPUS reconciliation (themes/bases/atoms — editable; NOT recipe/)
- [ ] **bases/alpine.md:11** "Deno and Gleam runtimes (not available on Alpine)" → "Deno runtime (not available on Alpine)".
- [ ] **bases/ubuntu.md:8** "Deno and Gleam runtimes (only available on Ubuntu)" → "Deno runtime (only available on Ubuntu)".
- [ ] **themes/core.md:206** "ALWAYS os: ubuntu for Deno and Gleam … not available on Alpine" → Deno only.
- [ ] **themes/core.md:38 + themes/model.md:101** shared-storage "no vertical autoscaling" claim → reconcile (shared-storage DOES accept verticalAutoscaling; object-storage does not). Read exact text first.
- [ ] **themes/services.md** — Valkey no-creds already correct (no edit). dash→underscore at ~:7 — reframe to charset if it presents my-db as a user hostname (read first; don't break discover logic/test). PostgreSQL 5433 HA-only already correct.
- [ ] **atoms** — scan for same stale facts (Gleam, init-failure, shared-storage) AFTER guide edits; fix per atom-lint. (Workflow QA re-scan.)

## FLAG-ONLY (recipe scope = Aleš)
- verify-web-agent-protocol's recipe.md/browser-walk references; build-integration `@v1.0.2` source; any shared-storage text under `internal/content/workflows/recipe/`. Mention in final report, do not edit.

## VERIFY (post-edit)
- snapshot diff review (all files vs snapshot-before/).
- `go build ./...` + `go test ./... -short` + atom-lint (`make lint-local` or gates).
- Codex-flagged tests: browser/annotations/corpus_coverage/discover/engine_doc/golden.
- Workflow QA: re-scan corpus consistency for residual contradictions.
