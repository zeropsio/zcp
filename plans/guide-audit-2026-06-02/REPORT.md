# Knowledge-guide audit — `internal/knowledge/guides` + `decisions`

**Date:** 2026-06-02 · **Scope owner:** krls2020 · **Status:** AUDIT + FIXES SHIPPED (scope B: guides + corpus reconcile). See §IMPLEMENTATION at bottom.

Three-axis verification of all 26 guide-docs (21 `guides/` + 5 `decisions/`):
1. **Docs** — each `.md` vs its `../zerops-docs/apps/docs/content/guides/*.mdx` source (+ deeper `references/`, `*/how-to/` docs).
2. **Live platform** — provisioned a fixture in `eval-zcp` and observed real behavior (evidence in `evidence/LEDGER.md` + dumps).
3. **Corpus/handlers** — each guide vs atoms, themes, specs, and the actual ZCP tool/handler code.

Method: a 53-agent workflow extracted **815 atomic claims** and cross-checked docs + corpus; I ran the live platform verification by hand (provision → SSH/observe → teardown). Tallies: **602 match docs · 161 ZCP-added · 24 drift · 28 contradict docs**; consistency: **19 guides clean, 48 non-ok findings (27 critical/major)**.

---

## 0. Four meta-findings (read first)

1. **The env-vars guide is AHEAD of the upstream docs, and live proves ZCP right.** The `.mdx` source still describes cross-service vars as *"Auto-Injected Project-Wide"* (legacy `none` mode) as if it were the behavior. ZCP's repo `.md` was rewritten to *"Explicit `${hostname_varname}` Required"* (the default `service` mode). **Live confirms the repo `.md`** (see §1). Same pattern in `networking.md` + `public-access.md` (subdomain auto-enable). ⇒ **A naïve `zcp sync pull guides` would REGRESS these three guides back to stale/wrong upstream text.** This is the single biggest structural risk.

2. **Live testing refuted two plausible env-vars claims that doc+corpus review accepted.** Only running the platform caught them:
   - **dash→underscore hostname is unconstructable** — Zerops rejects any hostname with a dash (`serviceStackNameInvalid`); the guide's `my-db`→`${my_db_port}` example describes an impossible service.
   - **the `BUILD_` prefix does not resolve** — `${BUILD_x}` (read a build var from runtime) stays literal in the container; build vars are not persisted to runtime. (The reverse, `RUNTIME_` into build, DOES work.)

3. **A cluster of guides emit schema-invalid YAML** — bare top-level `envVariables:` (valid only under `build`/`run`): `smtp`, `production-checklist`, `metrics`, two `php-tuning` fragments. Live-confirmed: the zerops.yml schema has `envVariables` only under `build`/`run`; copy-paste → import rejection.

4. **Several CLI/tool references are stale or wrong** (agent copies → hard error): `cdn` (`zsc cdn purge` missing the mandatory domain arg), `logging` (`zcli service log <name>` positional + `--showBuildLogs` camelCase), `local-development` (`.env` "from `zerops_discover`" — wrong tool, it's `zerops_env generate-dotenv`), `verify-web-agent-protocol` (raw `agent-browser` + `eval`, both forbidden by `zerops_browser`), `cloudflare` (manual subdomain-enable framing, ignores deploy auto-enable).

---

## 1. Live verification results (eval-zcp fixture)

Fixture: `gapi` (nodejs@22, real deploy) + `gdb` postgresql@16 + `gcache` valkey@7.2 + `gstore` object-storage, plus `iso1/iso2/iso3` service-mode isolation probes. All torn down after. Full dumps: `evidence/`.

### CONFIRMED on live platform
| Guide claim | Evidence |
|---|---|
| env: project vars auto-inherit into **runtime AND build** | `APP_KEY` bare in runtime; build log `PROJ_IN_BUILD=[xnorOO…]` |
| env: cross-service `${hostname_var}` resolves (run.envVariables) | `DB_REF=gdb` |
| env: unresolved ref stays **literal** | `UNRESOLVED=${nosuch_var}` |
| env: **self-shadow** `KEY=${KEY}` → literal | iso3 `APP_KEY=${APP_KEY}` (PROJECT_APP_KEY keeps real value) |
| env: default **`service`** isolates siblings; **`none`** auto-injects `<host>_KEY` | iso2 sees `iso1_*`=0 (service sender) but `gdb_*`=35 (none sender); per-service override works |
| env: secrets also undergo `${}` interpolation | iso2 `REF_SECRET=iso1value` from a sibling secret |
| env: `RUNTIME_` prefix (runtime var → build) works | build log `FROM_RUNTIME=[runtimeval]` |
| env: build/runtime isolation | `BUILDONLY` absent in runtime |
| env: `zerops_discover` returns `${…}` templates, not values | discover note + keys-only output |
| env: `${zeropsSubdomainHost}` present from project creation | project env `zeropsSubdomainHost=2445` |
| envReplace **non-recursive** | `conf/a.txt` replaced, `conf/sub/b.txt` not |
| deployFiles `dist/~` strips dir | files at `/var/www` root, no `dist/` |
| initCommands run **every start** | `init.log` 2 lines after deploy+restart |
| bind `0.0.0.0` → L7 reachable | subdomain returns HTTP 200 |
| subdomain URL `{host}-{subdomainHost}-{port}.prg1.zerops.app` | `gapi-2445-3000.prg1.zerops.app` |
| networking: `host` AND `host.zerops` resolve (VXLAN IPv6 ULA); plain-TCP ports open | `getent`, `/dev/tcp` gdb:5432, gcache:6379 |
| object-storage: exactly 9 env vars; `apiUrl` https; `http://`→**301** | discover keys + curl 301 |
| object-storage `apiHost` **exists** (docs omit it — guide is RIGHT) | discover shows `apiHost` key |
| object-storage = separate infra (no internal hostname) | `getent gstore` empty |
| scaling defaults: `minFreeRamGB=0.0625`, `minFreeCpuCores=0.1`, `startCpuCoreCount=2` | live discover resources |
| scaling: object-storage+verticalAutoscaling **rejected**; managed+minContainers **rejected**; `minFreeRamGB:0`+`minFreeRamPercent:0` → `invalidCustomAutoscalingValue` | import-check errors (exact guide string ✓) |
| schema: `envVariables` valid only under `build`/`run` | live zerops.yml schema |
| backup: ~daily cron + age/X25519 key | `gdb_BACKUP_PERIOD=8 0 * * *`, `gdb_ZEROPS_BACKUP_PUBLIC_KEY=age1…` |

### REFUTED on live platform (guide WRONG)
| Guide claim | What live showed |
|---|---|
| env-vars: hostname `my-db` → ref `${my_db_port}` (dash→underscore) | dashed/underscored/uppercase hostnames **all rejected** `serviceStackNameInvalid`; only `[a-z0-9]` valid — the example is unconstructable |
| env-vars: read build var from runtime via `${BUILD_x}` | `FROM_BUILD=${BUILD_BUILDONLY}` stayed **literal** (3× confirmed) |
| scaling: shared-storage+verticalAutoscaling "causes import failure" | **accepted** (only object-storage rejects; guide conflates the two) |
| choose-cache: Valkey `redis://${user}:${password}@${hostname}:6379` | live `redis://gcache:6379`, **no user/password vars exist** |
| choose-database: Postgres connString `…:5432/${db}` | live `postgresql://db:…@gdb:5432` (**no `/db` suffix**); db-name var is `dbName` |

### Undocumented behavior worth noting
- **`PROJECT_` prefix**: project vars appear both bare (`APP_KEY`) AND prefixed (`PROJECT_APP_KEY`) in every container. No guide mentions it. Harmless (bare works) but explains "extra" env noise; the self-shadowed bare key fails while `PROJECT_*` survives.

---

## 2. Real errors to fix (severity-ranked)

> "Guide-ahead-of-docs" cases (env-vars/networking/public-access service-mode + subdomain auto-enable, object-storage `apiHost`) are **NOT** here — those are correct; see §3.

### CRITICAL — agent composes broken artifact
| Guide | Error | Fix |
|---|---|---|
| `choose-cache` | Valkey connString with `${user}:${password}@` (live: unauthenticated) | `redis://${hostname}:6379`, no creds. Also add the **`valkey@8` fails-at-import** trap + NON_HA-default note. |
| `smtp` | bare top-level `envVariables:`/`envSecrets:` fragment (schema-invalid) | nest under `run.envVariables` (zerops.yaml) + `envSecrets` (import service level); they are different files. |
| `production-checklist` | bare top-level `envVariables:` (Mailpit/SMTP) **and** `minContainers:2` "for zero-downtime" (conflates replica count w/ cutover) | fix YAML scope; zero-downtime holds at any minContainers via default `temporaryShutdown:false`. Also Mailpit `type: go@1`→`alpine@3.20`; "5-min health retry" is fabricated. |
| `verify-web-agent-protocol` | raw `agent-browser` CLI + `eval` (forbidden; leaks Chrome) | rewrite onto `zerops_browser`/BrowserBatch (no open/close, no eval); add the verify-Recovery loop. |
| `local-development` | `.env` "generated from `zerops_discover`" (wrong tool) | `zerops_env action="generate-dotenv"`; add `.env.local`/three-channel model + refuse-on-edit gate; `db_host`→`db_hostname`. |
| `cdn` | `zsc cdn purge` examples drop the mandatory **domain** arg; Static-Mode-only not stated | `zsc cdn purge <domain> "<pattern>"`; note Object-Storage CDN purges only via REST. |

### MAJOR — wrong fact, mis-author/mis-diagnose risk
| Guide | Error |
|---|---|
| `environment-variables` | `${BUILD_x}` build→runtime (live-refuted); dash→underscore hostname (live-refuted, impossible); `PATH` listed as "editable" system var (it's hard-reserved, `userDataUseOfSystemKey`); "secret overridden by yaml" bullet contradicts own `userDataDuplicateKey` rule. |
| `scaling` | shared-storage verticalAutoscaling "import failure" (live: accepted); `minFreeCpuCores` is a **fraction 0.0–1.0** but docs label it "%" — 100× footgun; Docker horizontal-scaling tension w/ docs. |
| `zerops-yaml-advanced` | "Gleam requires ubuntu" — WRONG (live schema has `alpine/gleam@*`; only **Deno** is ubuntu-only); `start: zsc noop --silent` is the **retired** dev convention (a blocking gate now forbids it — omit `run.start`); crontab `allContainers` is schema-**required**; startCommands `name` is **optional** (guide says required). |
| `backup` | MariaDB "mysqldump" → real automated tool is **`mariabackup`/`.xb.gz`** (mysqldump is manual export); Postgres format is **`.zip`** not raw pg_dump; ClickHouse omitted entirely. |
| `choose-database` | PostgreSQL framed HA-by-default (live: **NON_HA single** is default; HA opt-in); port **5433 is HA-only** (no qualifier); MariaDB connString `/${db}` suffix fabricated; `${db}`→`${dbName}`. |
| `choose-runtime-base` | "Alpine best for all runtimes" omits Deno/Gleam (ubuntu-only); "system packages → Ubuntu" is the wrong switch axis (apk installs on Alpine; switch on glibc/musl/CGO/C-ext/Deno-Gleam); `apk`/`apt-get` examples drop mandatory `sudo`. |
| `cloudflare` | "must manually `zerops_subdomain enable`" ignores **deploy auto-enable**; the `serviceStackIsNotHttp` rejection is about HTTP-port shape, not deploy state (invented "ACTIVE-only" precondition). |
| `php-tuning` | leads with `run.envVariables` for PHP_INI_* but says "restart (not reload)" — for that channel a value is **baked into the app version → needs full redeploy**; restart-vs-reload nuance applies only to the env-API channel. |
| `logging` | `zcli service log <name>` positional (real CLI uses `-S/--service-id`); `--showBuildLogs`→`--show-build-logs`; never surfaces `zerops_logs` (and that it can't fetch build logs). |
| `vpn` | `--auto-disconnect` = "disconnect existing connection so a new `up` proceeds", NOT "on terminal close". |
| `ci-cd` | skip token needs brackets `[ci skip]`/`[skip ci]`; never mentions `record-deploy` bridge (CI deploy leaves ZCP `never-deployed`); `zeropsio/actions@main` vs corpus-pinned `@v1.0.2` + can't pass `--setup`. |
| `metrics` | `ELASTIC_APM_SERVER_URL` hardcoded to non-resolvable `apmserver.zerops.app` (must copy from GUI); bare top-level `envVariables:`. |
| `deployment-lifecycle` | event-timeline item 3 labels `appVersion ACTIVE` as "deploy started/launching" (it's the terminal deployed state — own later sections agree); "initCommands failures do NOT cancel deploy" vs ZCP `DEPLOY_FAILED` hint "initCommands crashed the container" (**needs a live deploy w/ failing init to settle**); SSHFS "no remount needed" vs atoms/handler that prescribe explicit remount on stale mount. |

### MINOR / drift (mostly upstream-inherited, lower stakes)
choose-database ClickHouse engine nuance · choose-queue Kafka "Schema Registry port 8081" (uncorroborated) · public-access 512MB framed as hard limit (it's the configurable default, 2048m ceiling) + dedicated-IPv4 reuse nuance dropped · cloudflare "Full (strict) mandatory" (docs allow "Full" for testing) · choose-search PLUGINS belongs in `envSecrets` · object-storage env table duplicates an atom (drift risk) · choose-runtime-base ubuntu 22.04 still available · several CDN/logging scope-omissions. Full list: `evidence/consistency-findings.md` + `evidence/doc-findings.md`.

---

## 3. Guide-ahead-of-docs — do NOT "correct" toward the docs

These read as "contradicts-docs" but the **guide is right** (live- or handler-confirmed); the **docs are stale/incomplete**. Fixing toward docs would regress them. Candidates to push UPSTREAM into `zerops-docs`:

- **environment-variables / networking** — default `service` isolation, explicit `${hostname_var}` refs, `none`=legacy auto-inject. (Live-proven. Docs `.mdx` says auto-inject-everywhere — wrong.)
- **public-access / (and the cloudflare fix)** — `zerops_deploy` auto-enables subdomain on first deploy. (Handler behavior; docs `.mdx` says manual enable / "502 without it".)
- **environment-variables** — secret read is privilege-gated (admin verbatim / read-only REDACTED); same-key secret+yaml → `userDataDuplicateKey` reject. (Docs say "write-only" / "silently overrides" — older model.)
- **object-storage** — `apiHost` env var **exists** (live-confirmed); the upstream docs table omits it.

**Sync guard recommendation:** `sync pull guides` is overwrite-on-pull. Before merging a future pull, diff these four guides and re-apply the corrections, or upstream them first so the pull is a no-op. (The env-vars 222-line drift is exactly this corrected content.)

---

## 4. Per-guide verdict

| Guide | Docs | Live | Corpus | Verdict |
|---|---|---|---|---|
| environment-variables | ahead (correct) | **mostly ✓, 3 refuted** | minor PATH/override | **fix 3, keep service-mode** |
| networking | ahead (correct) | ✓ | ✓ | keep; sync-guard |
| public-access | ahead (correct) | ✓ | ✓ | keep; sync-guard |
| object-storage-integration | ✓ (apiHost: guide right) | ✓ | atom drift | minor |
| deployment-lifecycle | mostly ✓ | ✓ (init-every-start) | timeline/sshfs | fix timeline + init-fail (verify) |
| scaling | mostly ✓ | **shared-storage refuted** | ✓ | fix shared-storage + CpuCores unit |
| zerops-yaml-advanced | drift | n/a | **Gleam/zsc-noop stale** | fix 4 |
| choose-cache | contradicts | **refuted (no creds)** | contradicts | **CRITICAL fix** |
| choose-database | drift | NON_HA confirmed | contradicts | fix HA-framing + connString |
| choose-runtime-base | drift | n/a | contradicts | fix Alpine/Deno + sudo |
| backup | contradicts | cron/key ✓ | stale tool | fix MariaDB/PG format |
| smtp | n/a | schema ✓ | **invalid YAML** | **CRITICAL fix** |
| production-checklist | minor | n/a | **minContainers/YAML** | **CRITICAL fix** |
| verify-web-agent-protocol | (no source) | n/a | **stale tool** | **CRITICAL rewrite** |
| local-development | contradicts | n/a | **wrong tool** | **CRITICAL fix** |
| cdn | contradicts | n/a | stale CLI | fix purge cmd |
| cloudflare | minor | n/a | stale subdomain | fix 2 |
| php-tuning | contradicts | n/a | restart-channel | fix restart→redeploy |
| logging | contradicts | n/a | flag/tool | fix CLI |
| vpn | drift | n/a | ✓ | fix --auto-disconnect |
| ci-cd | contradicts | n/a | bracket/record-deploy | fix 3 |
| metrics | contradicts | n/a | APM url/YAML | fix 2 |
| choose-queue | ✓ (enriched) | n/a | minor (8081) | minor |
| choose-search | ✓ | n/a | minor (envSecrets) | minor |
| firewall / logging-mdx-only | ✓ | n/a | — | clean |

*(metrics shows byte-identical `.md`↔`.mdx` but both drift from the deeper `references/metrics.mdx` — the errors are inherited from the guide source, not from sync.)*

---

## 5. Recommended next actions (Karel decides)

1. **Fix the 6 CRITICALs** (choose-cache, smtp, production-checklist, verify-web-agent-protocol, local-development, cdn) — each is an agent-breaks-on-copy bug.
2. **Fix the MAJORs** (env-vars ×4, scaling, zerops-yaml-advanced, backup, choose-database, choose-runtime-base, cloudflare, php-tuning, logging, vpn, ci-cd, metrics).
3. **Add a sync-guard** for the 4 guide-ahead files (§3) so `sync pull guides` can't regress them; ideally upstream the corrections to `zerops-docs`.
4. **One remaining live probe** worth running before finalizing deployment-lifecycle: does a non-zero `initCommands` actually fail the deploy (`DEPLOY_FAILED`)? The guide says no; the ZCP failure-hint says yes. (Needs a deliberate failing-init deploy.)
5. Recipe-scoped items (verify-web-agent-protocol's `zerops_browser`/recipe.md references, build-integration `@v1.0.2`) intersect Aleš's scope — flag, don't auto-edit.

Evidence: `evidence/LEDGER.md` (live), `evidence/doc-findings.md` (28 contradict + 24 drift), `evidence/consistency-findings.md` (48 corpus findings), `evidence/wf-results.json` (815 claims), `evidence/*.txt|json` (raw dumps), `evidence/import-schema.json` + `zerops-yml-schema.json` (live schemas).

---

## IMPLEMENTATION (2026-06-02, scope B)

Fixed all guide errors + reconciled the stale corpus surfaces they exposed. Markdown-only (no Go/test edits). 30 files changed:
- **17 guides**: environment-variables, scaling, zerops-yaml-advanced, deployment-lifecycle, backup, cloudflare, php-tuning, logging, vpn, ci-cd, metrics, public-access, smtp, cdn, production-checklist, local-development, verify-web-agent-protocol.
- **5 decisions**: choose-cache, choose-database, choose-runtime-base, choose-queue, choose-search.
- **5 corpus (themes/bases)**: themes/core, themes/model, themes/services, themes/operations, bases/alpine, bases/ubuntu — Gleam (3 spots incl. a missed core.md:118), shared-storage verticalAutoscaling, dash framing, ci-skip brackets.
- **2 specs**: spec-zerops-env-lifecycle (BUILD_ prefix REFUTED→live, dash), spec-local-dev (.env via generate-dotenv).
- **Untouched (clean)**: firewall, build-cache, networking, object-storage-integration.

### Live re-verifications run this session (grounding the fixes)
- Gleam on Alpine: `alpine/gleam@1.5.1` imports + reaches ACTIVE → only **Deno** is ubuntu-only. (corpus said "Deno and Gleam" in 3 places — all corrected)
- valkey@8 → rejected `serviceStackTypeNotFound`; valkey@7.2 valid.
- **initCommands failure → deploy ABORTS** (definitive, platform log, twice): `exit 7` → `❌ RUN.INIT COMMANDS FINISHED WITH ERROR` → NO start, `/var/www` empty, no process, appVersion not activated. **The upstream per-runtime `build-pipeline.mdx` ("deploy is NOT canceled … application is started regardless of status code") is STALE/WRONG** — file an upstream docs bug. Guide now states the correct (abort) behavior.
- shared-storage + verticalAutoscaling → ACCEPTED (only object-storage rejects).
- health-check live defaults: failureTimeout 3m0s, execPeriod 30s (but not schema-pinned → guides say "configurable, no fixed default").
- zsc `cdn purge <domain> [path]`, `--auto-disconnect`, `--show-build-logs`, `zcli service log -S` — all CLI-confirmed.

### QA round (adversarial workflow, 42 agents) — caught + fixed
- **CRITICAL self-catch**: my initCommands edit "contradicts docs" → re-verified live, docs are the stale side, edit KEPT (now docs-bug-to-upstream).
- **Missed corpus surface**: core.md:118 (shared-storage verticalAutoscaling — a 2nd occurrence) → fixed.
- **Missed**: themes/operations.md:76 (ci-skip brackets), spec-zerops-env-lifecycle BUILD_ (PENDING→REFUTED-live), spec-local-dev (.env tool ×3) → fixed.
- **Incomplete fixes**: choose-runtime-base Ubuntu section + matrix dup, choose-search HEAP_PERCENT/PLUGINS symmetry → completed.
- **My-edit polish**: production-checklist dropped unsourced "~3 min" (cross-guide consistency), metrics APM URL format corrected, environment-variables PATH-overridable softened to "e.g.", smtp fence spacing.

### One item FLAGGED, not blindly done (needs your decision)
**verify-web-agent-protocol agent-browser → zerops_browser rewrite.** I made the safe part (added the mandated verify-Recovery loop). The tool-swap is deferred because it ripples through the `develop-verify-matrix` atom + **~10 atom-golden files** + 2 `corpus_coverage_test` pins on the literal `"agent-browser"`, and rests on whether the verify sub-agent has `zerops_browser`. Recommend: confirm the sub-agent's tool access, then rewrite guide+atom+regenerate goldens+update pins as one change. Borders the verification/recipe subsystem.

### Verification status
- `go build ./...` OK; knowledge + content + recipe + tools + server + eval tests PASS.
- `internal/workflow` test package currently fails to BUILD — **unrelated to this work**: the concurrent schema-single-source refactor changed `ValidateRecipePlan(plan, *schema.Schemas)` and left `recipe_validate_test.go` / `recipe_templates_dualruntime_test.go` calling the old 3-arg signature (both uncommitted `M` from that stream). My batch-1 edits already passed the full workflow suite; batch-2 markdown does not appear in any workflow golden (grep-verified) and removes no coverage pin. Re-run `go test ./internal/workflow/...` once that stream's build is fixed.

### Upstream docs bugs to file (zerops-docs) — guides are now ahead/correct
1. **build-pipeline.mdx (all runtimes)**: init-failure does NOT cancel deploy — WRONG (live: it aborts).
2. **guides/env-variables.mdx**: cross-service "auto-injected project-wide" — describes legacy `none`; default is `service` (explicit refs).
3. **guides/public-access.mdx**: "call zerops_subdomain enable once after first deploy" — deploy auto-enables now.
4. **mariadb/how-to/backup.mdx vs guide**: guide had mysqldump; correct is mariabackup/.xb.gz (docs right here — guide was wrong, now fixed).
5. CLI doc inconsistencies: `--showBuildLogs` (should be `--show-build-logs`), `ci skip` (should be `[ci skip]`).
