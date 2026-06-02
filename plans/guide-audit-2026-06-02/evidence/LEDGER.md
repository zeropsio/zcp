# Live verification ledger — eval-zcp (project 2Biyb7d2TQeSum9HNtjLQQ, org KRLS BkC8AGjFQMyFrLbzjHoE9g)

Fixture (none-mode project): gdb postgresql:single@16, gcache valkey:single@7.2, gstore object-storage, gapi ubuntu/nodejs@22 (startWithoutCode).

## CONFIRMED (live)
- **object-storage env vars** = exactly {apiUrl, apiHost, accessKeyId, secretAccessKey, bucketName, quotaGBytes, projectId, serviceId, hostname} — matches object-storage-integration.md 9-var table, no extras/missing. [discover]
- **object-storage apiUrl** = `https://storage-prg1.zerops.io` (shared regional endpoint, path-style). `http://` → **301 redirect to https** — confirms guide's "gateway rejects plaintext HTTP with 301". [curl from gapi]
- **gstore has NO internal VXLAN hostname** (getent gstore = empty) — confirms "independent infrastructure, separate infra". [getent]
- **zeropsSubdomainHost present at project scope** = `2445`; `zeropsSubdomainString` template = `https://${hostname}-2445-${port}.prg1.zerops.app` — confirms env-vars "${zeropsSubdomainHost} present from project creation" AND public-access/networking subdomain URL format `{host}-{subdomainHost}-{port}.prg1.zerops.app`. [discover + container env]
- **zerops_discover returns ${...} templates, not resolved values** — discover note states it explicitly; resolution is in-container. Confirms env-vars "discover returns literal template". [discover]
- **project vars auto-inherit into runtime** — APP_KEY, GIT_TOKEN, ZCP_API_KEY, apiCdnUrl, staticCdnUrl, storageCdnUrl appear bare in gapi env. [container env]
- **internal DNS**: both `gdb` and `gdb.zerops` resolve to VXLAN IPv6 ULA (fda0:5ef:27e:c0de:...). [getent] — networking "both api and api.zerops resolve"
- **VXLAN internal ports reachable** plain TCP: gdb:5432 OPEN, gcache:6379 OPEN. [/dev/tcp]
- **valkey connectionString** = `redis://gcache:6379` (redis scheme, internal host:port); portTls 6380. [discover sibling vars]
- **backup**: gdb_BACKUP_PERIOD=`8 0 * * *` (00:08 daily cron), gdb_ZEROPS_BACKUP_PUBLIC_KEY=`age1ctn...` (age = X25519 recipient) — consistent with backup.md "00:00-01:00 UTC daily" + "X25519 encryption". [sibling vars]
- **postgresql preloads** pg_stat_statements,pg_cron,timescaledb; exposes superUser/superUserPassword in addition to user/password, plus portTls/connectionTlsString. [discover/env]
- **private network** = CGNAT IPv4 100.64.0.0/10 (ZEROPS_NatIPv4=100.64.39.120) + IPv6 ULA fda0:5ee:bad:c0dd::/64. [container env]

## SCHEMA (live JSON schema, authoritative)
- minContainers/maxContainers: integer 1-10 — confirms scaling "1-10 containers".
- cpuMode enum: {DEDICATED, SHARED} — confirms scaling.
- zerops.yml: `envVariables` appears ONLY under build/run (2 occurrences), NOT at setup-entry top level — confirms env-vars "envVariables rejected at setup-entry top level".
- zerops.yml has healthCheck, readinessCheck, startCommands, start, envReplace, routing, extends, crontab keys present.

## SURPRISE / NUANCE
- **eval-zcp project is envIsolation=none (LEGACY)**, not default `service`. So guide's DEFAULT-mode claims (siblings isolated, explicit ref required) are NOT testable here — need a service-mode project. The `none`-mode behavior IS fully confirmed: 85 sibling `<host>_KEY` vars auto-injected into gapi (gdb_*, gcache_*, gstore_*), AND each sibling re-exports inherited project vars under its prefix (gcache_APP_KEY etc.).
- **PROJECT_ prefix**: project vars ALSO appear as `PROJECT_<KEY>` (PROJECT_APP_KEY, PROJECT_zeropsSubdomainHost=2445) — a prefix the env-vars guide never documents. (none-mode artifact? verify in service-mode.)

## SERVICE-MODE + SELF-SHADOW (iso1/iso2/iso3, per-service envIsolation:service)
- **explicit cross-service ref resolves** (service mode): iso2 REF_HOST=`iso1` from `${iso1_hostname}`. ✓
- **sibling SECRET referencable** via `${host_var}`: iso2 REF_SECRET=`iso1value` from `${iso1_ISO1_SECRET}`. So envSecrets values DO undergo `${}` interpolation. ✓
- **unresolved ref → literal**: iso2 UNRESOLVED=`${nosuch_var}`. ✓
- **service-mode isolation (source-side directional)**: iso2 sees `iso1_*` bare = **0** (iso1 is service-mode sender, does NOT expose) but `gdb_*` bare = **35** (gdb none-mode sender DOES expose). Per-service envIsolation:service overrides project default none for that service. ✓✓
- **self-shadow → literal**: iso3 service secret `APP_KEY=${APP_KEY}` (APP_KEY is a project var) resolves to literal `${APP_KEY}` in-container; PROJECT_APP_KEY keeps real value. ✓ Exactly env-vars guide's self-shadow trap.

## DEPLOY FIXTURE (gapi, real zerops.yml deploy via zcli push)
- **deployFiles `dist/~`**: /var/www has server.js+conf+init.log at ROOT, `dist/` ABSENT — tilde strips dir. ✓ (deployment-lifecycle)
- **build/runtime isolation**: build var BUILDONLY absent in runtime env. ✓
- **RUNTIME_ prefix (runtime→build) WORKS**: build log `FROM_RUNTIME=[runtimeval]` from `${RUNTIME_RUNTIME_MARKER}`. ✓
- **project var inherits into BUILD**: build log `PROJ_IN_BUILD=[xnorOO...]` from `${APP_KEY}`. ✓
- **BUILD_ prefix (build→runtime) REFUTED**: runtime `FROM_BUILD=${BUILD_BUILDONLY}` stayed LITERAL (confirmed via SSH env, restart, AND app process.env). Build vars are NOT persisted to the runtime env store. CONTRADICTS env-vars guide's `${BUILD_BUILD_ID}` example. **MAJOR FINDING.**
- **yaml-baked run.envVariables cross-service ref**: DB_REF=`gdb` from `${gdb_hostname}`. ✓
- **unresolved → literal** (yaml-baked): UNRESOLVED=`${nosuch_var}`. ✓
- **project var forward** (yaml-baked): PROJ_FWD=real APP_KEY. ✓
- **envReplace NON-recursive**: /var/www/conf/a.txt `%%APP_KEY%%`→real value; /var/www/conf/sub/b.txt stayed `%%APP_KEY%%`. ✓ (env-vars + zerops-yaml-advanced)
- **initCommands run EVERY start**: init.log gained a 2nd line after stop/start (12:09:02Z deploy + 12:11:44Z restart). Restart preserved /var/www (deploy would replace). ✓ (deployment-lifecycle)
- **bind 0.0.0.0 → L7**: subdomain `https://gapi-2445-3000.prg1.zerops.app` returns HTTP 200 `{"ok":true,"cwd":"/var/www"}` — L7 SSL-terminates and reaches the 0.0.0.0:3000 app over VXLAN. ✓ (networking; converse localhost→502 not deliberately broken)
- **subdomain URL format**: `gapi-2445-3000.prg1.zerops.app` = `{host}-{subdomainHost}-{port}.prg1.zerops.app`. ✓ (public-access)
- **live scaling defaults** (discover): minFreeRamGB=0.0625, minFreeCpuCores=0.1, startCpuCoreCount=2, cpuMode SHARED, RAM 0.125-8, containers 1-3. ✓ (scaling default-value claims)
- minor curiosity: discover reports port httpSupport:false despite zerops.yml httpSupport:true, yet subdomain HTTP works — discover port flag ≠ L7 http enablement (not a guide claim).

## STILL DOCS/CORPUS-ONLY (not live-tested; deferred or impractical)
- userDataDuplicateKey (secret on yaml-baked key) — needs adopted service or destructive re-import; repo-pinned (CLAUDE.md invariant + tests).
- subdomain AUTO-enable on first deploy — ZCP zerops_deploy handler behavior (repo-pinned); zcli push does not auto-enable (I enabled manually).
- nginx L7 tuning defaults (worker_connections 4000, keepalive_timeout 30s, send_timeout 2s, client_max_body_size 512m), subdomain 50MB cap, IPv4 pricing, CDN/Cloudflare/firewall/SMTP, backup 7-day retention, app-versions kept=10, build 60-min timeout — platform-config, verify against docs.

## TODO live (DONE — section retained for history)
- service-mode project: siblings NOT injected, explicit ${host_var} resolves, dashes→underscores, unresolved→literal, self-shadow→literal.
- dashed hostname allowed at all? (guide my-db example)
- deploy fixture: build/runtime isolation, RUNTIME_/BUILD_ prefix, envReplace no-recurse, deployFiles tilde, initCommands-every-start, bind 0.0.0.0→502, subdomain auto-enable on first deploy.
- schema rejections: verticalAutoscaling on object-storage; minContainers on managed; minFreeRamGB:0+minFreeRamPercent:0; httpGet+exec both; start+startCommands both.
- userDataDuplicateKey (secret on yaml-baked run.envVariables key).

## initCommands-failure → deploy ABORTS (raw evidence, 2026-06-02)
Settles deployment-lifecycle gotcha 3. Tested TWICE (initprobe exit 1; initb exit 7) — identical result.

**initb fixture**: nodejs@22, zerops.yml run.initCommands = `echo "INIT-RAN $(date) exit7" >> /var/www/markers.log; exit 7`, run.start = `node server.js` (binds 0.0.0.0, appends START-RAN to markers.log), deployFiles `./`.

**zcli push result**: build OK (`ALL BUILD COMMANDS FINISHED`, `BUILD ARTEFACTS READY TO DEPLOY`) → `Application is deploying` → `✗ ERR last command has finished with error` (support id msl4EipAQGWe03tBTrPPaw).

**Platform runtime log (verbatim) — the start command NEVER runs after init fails:**
```
┃   RUNNING RUN.INIT COMMANDS   ┃
🙏 echo "INIT-RAN $(date -u +%FT%TZ) exit7" >> /var/www/markers.log; exit 7
❌ echo "INIT-RAN ...; exit 7 => 7 (exited with 7)
❌ RUN.INIT COMMANDS FINISHED WITH ERROR
[no "RUNNING RUN.START" / "starting application" / "listening" line follows — only later SSH-accept lines]
```

**Post-deploy container state**: `/var/www` is EMPTY (`ls -la` shows only `.`/`..`) — no `server.js`, no `markers.log` (neither INIT-RAN nor START-RAN persisted; the failed deploy rolled back to the empty placeholder). `pgrep node server.js` = none. App is NOT running; new appVersion NOT activated.

**Verdict**: a non-zero `initCommands` exit ABORTS the deploy — `run.start` does not execute, the appVersion is not activated. This CONTRADICTS upstream `build-pipeline.mdx` (all runtimes: "deploy is NOT canceled … application is started regardless of status code"), which is STALE. Guide `deployment-lifecycle.md` gotcha 3 corrected to match live; upstream docs bug to file. (initprobe with `exit 1` produced the identical `RUN.INIT COMMANDS FINISHED WITH ERROR` + no app.)
