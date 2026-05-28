---
id: develop/failure-tier-3
atomIds: [develop-env-var-model, develop-first-deploy-intro, develop-change-drives-deploy, develop-deploy-modes, develop-env-var-channels, develop-first-deploy-env-vars, develop-first-deploy-scaffold-yaml, develop-http-diagnostic, develop-nodejs-greenfield-buildhint, develop-platform-rules-common, develop-reserved-env-names, develop-checklist-dev-mode, develop-deploy-files-self-deploy, develop-dynamic-runtime-start-container, develop-first-deploy-write-app, develop-knowledge-pointers, develop-auto-close-semantics, develop-first-deploy-execute, develop-verify-matrix, develop-first-deploy-verify, develop-platform-rules-container, develop-strategy-awareness]
description: "Active session, third-iteration failure history — three failed deploy attempts on appdev (close-mode auto)."
---
### Where values come from

Project envs auto-inject as OS env vars into every container — app
code reads them directly via `process.env.KEY`, no `zerops.yaml` line
required.

`run.envVariables` lines exist for two purposes only:

1. **Rename a cross-service value** — destination on the left,
   `${hostname_varname}` source on the right. Example:
   ```yaml
   run:
     envVariables:
       DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
       REDIS_URL: ${cache_connectionString}
   ```
   Cross-service access is ALWAYS an explicit `${host_var}` ref in
   `run.envVariables`. A sibling's bare var never appears on its own;
   relying on that breaks every isolated project (only `none` mode
   auto-shares siblings).
2. **Mode flag with a per-setup literal** — `NODE_ENV: development`
   in `setup: appdev`, `NODE_ENV: production` in `setup: appstage`.

### Self-shadow — never the same name on both sides

```yaml
db_hostname: ${db_hostname}   # WRONG — destination == source
APP_KEY: ${APP_KEY}           # WRONG — re-declaring a project env
```

Source resolves to the literal string `${db_hostname}` (8 chars
including dollar-brace), reaches `process.env` as that literal, and
the framework crashes when it parses it as a hostname.

---

### You're in the develop first-deploy branch

The envelope reports at least one in-scope service with
`deployed: false` (bootstrapped but never received code). Finish that
here: establish `zerops.yaml` and the app, deploy, verify.

Flow for each never-deployed runtime:

1. **Establish `zerops.yaml`** — scaffold if absent, refine in place if
   already present.
2. **Establish the application code** — adapt existing source if the
   mount carries it, scaffold real code otherwise.
3. **Run `zerops_deploy targetService=<hostname>`** with NO `strategy`
   argument. Every first deploy uses the default push path;
   `strategy=git-push` requires `GIT_TOKEN` + committed code
   (container) or a configured git remote (local), neither ready yet.
4. **Verify** the service responds on its expected surface (web /
   worker / managed). Close and completion semantics fire once the
   close-mode is set and the deploy + verify pass.

Auto-close is gated on `closeDeployMode` being set for every in-scope
service — `unset` blocks the close even after deploy + verify pass.
The Services block names each service's current value (`closeMode=auto|
git-push|manual|unset`); `unset` reads from a bootstrap that didn't
declare a strategy. Set it for each in-scope service:

```
zerops_workflow action="close-mode" closeMode={"<host>":"auto"}
```

The strategy-awareness section of this response covers all three axes
(closeMode, gitPush, buildIntegration) and the per-service mix.

Don't skip to edits before the first deploy lands — HTTP probes
return errors before any code is delivered.

---

### Every code change must reach a durable state

Iteration cadence is mode-specific:

- Dev-mode dynamic runtime: edit code in place; reload via
  `zerops_dev_server` (no full redeploy for code-only changes).
- Simple / standard / local / first-deploy: every change →
  `zerops_deploy`.

Once close-mode is `auto` or `git-push` and every resolved deploy
target is deployed + verified, the work session auto-closes.

---

### Two deploy classes

| Class | Trigger | `deployFiles` constraint | Typical use |
|---|---|---|---|
| **Self-deploy** | `sourceService == targetService`, or omitted and inferred to target | MUST be `[.]` or `[./]`; narrower patterns destroy target source | dev/simple mutable workspace |
| **Cross-deploy** | `sourceService != targetService`, or `strategy=git-push` | Cherry-pick build output: `./out`, `./dist`, `./build` | dev→stage promotion; stage runs foreground binaries |

Self-deploy refreshes a **mutable workspace**; cross-deploy produces an
**immutable artifact** from build-container output after `buildCommands`.

### Picking deployFiles

| Setup block purpose | deployFiles | Why |
|---|---|---|
| Self-deploy (dev, simple modes) | `[.]` | Anything narrower destroys target on deploy. |
| Cross-deploy, preserve dir | `[./out]` | Lands at `/var/www/out/...`; use when `start` references that path or artifacts live in subdirs. |
| Cross-deploy, extract contents | `[./out/~]` | Tilde strips `out/`; use when runtime expects assets at `/var/www/`. |

### Why the source tree sometimes doesn't have `./out`

`deployFiles` is evaluated against the **build container filesystem
after `buildCommands`**, NOT the editor tree. `deployFiles: [./out]`
is correct even when `./out` is absent locally; the build creates it.
See guide `deployment-lifecycle`.

ZCP pre-flight does NOT check cross-deploy path existence; Zerops
builder emits `WARN: deployFiles paths not found: ...` in
`DeployResult.BuildLogs` only if the build produces no matches.

---

### Env var channels

Channel determines when a value goes live.

| Channel | Set with | When live |
|---|---|---|
| Service-level env | `zerops_env action="set"` | `restartedServices` lists cycled runtime containers; `restartedProcesses` has Process details. |
| `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
| `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |

**Suppress restart**: pass `skipRestart=true`; response reports
`restartSkipped: true`, `nextActions` says how to restart, and the value
is **not live** until then. Partial failures land in `restartWarnings`;
`stored` confirms landed keys.

**Layer precedence — yaml-baked > service > project.** A key baked by
`run.envVariables` can't be set at service scope (`userDataDuplicateKey`,
the two never coexist) — edit `zerops.yaml` + redeploy to change it. The
reverse is silent: a `project=true` set of a key some service bakes (or
sets at service scope) stores fine, but that service keeps its higher
value — `shadowWarnings` names the key + service and `nextActions` won't
call it live. Fix at the winning layer. A self-shadow (`KEY: ${KEY}`) is
different: one self-referential line, not two layers.

---

### Env var catalog from bootstrap

Managed services expose env var keys your runtime references. Fetch
the live key list per managed service with `zerops_discover
service="<hostname>" includeEnvs=true` and use those keys verbatim —
**do not guess alternatives**. The catalog is the authoritative source;
the host key is `hostname` (never `host`), other keys vary per service
type. Values are redacted by default; names are enough for wiring.
Pass `includeEnvValues=true` only for troubleshooting.

Cross-service wiring goes in `zerops.yaml` `run.envVariables`. A wrong
spelling on the right-hand side reaches the app as the literal string
and connect-time fails.

### Per-managed-type cheatsheet

- **Postgres / MariaDB / MySQL** — `connectionString` resolves to
  `protocol://${user}:${password}@$appdev:${port}` and **omits
  the database name**. For Prisma / Drizzle / sqlx / SQLAlchemy /
  Sequelize, compose explicitly with `/${db_dbName}` appended (see
  worked example in the env-var-model atom).
- **Prisma — `migrate dev` errors with `P3014`**: shadow DB needs
  CREATE DATABASE the regular user lacks. Fresh schemas:
  `prisma db push` (no shadow). For migration files, pass `migrate dev`
  the elevated URL only for that call —
  `postgresql://${db_superUser}:${db_superUserPassword}@${db_hostname}:${db_port}/${db_dbName}`.
- **Elevated DDL credentials** — `superUser`/`superUserPassword` on
  Postgres + ClickHouse, only when DDL needs them.
- **ClickHouse + Kafka** — multi-port; match driver (`portHttp` /
  `portMysql` / `portNative` / `portPostgresql` for ClickHouse;
  Kafka builds broker URL from `hostname:port`, no `connectionString`).
- **Object storage** — S3-compatible: `apiUrl`, `accessKeyId`,
  `secretAccessKey`, `bucketName`. No `region`.
- **Shared storage** — `hostname`-only mount in zerops.yaml.
- **Search / vector** (Meilisearch, Typesense, Qdrant) — scoped API
  keys; pick the narrow key, never master. Qdrant ships HTTP
  (`connectionString`) + gRPC (`grpcConnectionString`).

For exotic types, `zerops_knowledge query="<service>"` returns the
canonical page. Reserved-keys atom covers the few keys forbidden in
`envVariables` (`HOSTNAME` in run = `BUILD_FAILED` 4-5s, empty logs).

---

### Establish `zerops.yaml`

Scaffold `zerops.yaml` if absent or refine it in place if already
present. The file lives at the repo root; `setup:` matches the runtime
hostname (one `zerops:` entry per in-scope runtime).

**Shape (one `zerops:` block per targeted runtime hostname):**

```yaml
zerops:
  - setup: <hostname>
    build:
      base: <runtime-only key, e.g. nodejs@22 — NOT the composite run key>
      buildCommands: [...]       # optional for pre-built artefacts
      deployFiles: [...]         # [.] for self-deploy; build-output subset for cross-deploy
    run:
      base: <run key, may be composite: php-nginx@8.4, nodejs@22, ...>
      ports:
        - port: <app-listens-on>
          httpSupport: true
      envVariables:
        <KEY>: <value or ${service_KEY} cross-ref>
      start: <run command, not a build command>
```

**Env var references** use `${hostname_KEY}` syntax — Zerops rewrites
the placeholder at deploy time from the named service's catalog. Wrong
spelling stays literal and the app fails at connect.

**Mode-aware tips:** emit separate setup entries per targeted hostname.
`deployFiles: [.]` for self-deploys (single service); narrower patterns
only for cross-deploys where the source ≠ target.

---

### HTTP diagnostics

For 500 / 502 / empty body, stop at the first useful signal; do **not**
default to
`ssh appdev curl localhost` for diagnosis.

1. **`zerops_verify serviceHostname="appdev"`** — start with the
   canonical health probe and structured diagnosis (it picks the right
   check route per service shape).
2. **Subdomain URL** — static / implicit-webserver:
   `https://appdev-${zeropsSubdomainHost}.prg1.zerops.app/`; dynamic
   adds `-{port}`. `${zeropsSubdomainHost}` is numeric and project-scope,
   not the projectId. Read it with `env | grep zeropsSubdomainHost`, or
   use `zerops_discover` for the resolved URL. Do not guess a UUID.
3. **`zerops_logs severity="error" since="5m"`** — recent platform errors
   (nginx, crash traces, deploy failures) without opening a shell.
4. **Framework log file** — read via Read tool at the framework's
   project-relative log path (`storage/logs/laravel.log`,
   `var/log/...`). Path resolves against the runtime root configured
   for the active environment.
5. **Last resort: SSH + curl localhost** — only when earlier checks miss
   container-local state (worker-only service, non-default bind). Even
   then, `zerops_verify` usually already encodes the check.

---

### Node.js — `npm install`, not `npm ci`

Fresh Node scaffold with no committed `package-lock.json`: `npm install` in `build.buildCommands`. `npm ci` fails with `EUSAGE` until a lockfile is committed.

---

### Platform rules

- **Runtime user is `zerops`, not root.** Package installs need `sudo`
  (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
- **Deploy = new container.** Local files in the current runtime container are
  lost; only content covered by `deployFiles` survives across redeploys.
- **Setup blocks (`prod`, `stage`, `dev`) are canonical recipe names,
  NOT hostnames.** Each block deploys independently.
- **Build ≠ runtime container.** Runtime packages → `run.prepareCommands`;
  build-only packages → `build.prepareCommands`. Build-time tools may
  not exist at run time; see guide `deployment-lifecycle`.
- Cross-service env vars use `${hostname_KEY}` syntax and must be
  declared in `run.envVariables` under an own-key alias to reach the
  app process. Project-level vars auto-inherit; same-key declaration
  (`API_URL: ${API_URL}`) produces a literal-string shadow.
- **`zerops_import override=true` is destructive** — REPLACES the
  service stack (container, code, env vars, filesystem). Reserved for
  explicit user-requested config changes (shared storage, scaling,
  nginx) that `zerops_deploy` can't handle. Never the default fix for
  hostname collisions, env drift, or unexpected state — pick a
  different hostname, adopt, or escalate. Back up first; Warnings
  name replaced hostnames.

---

### Reserved env-var keys — two failure modes

A small set of keys is platform-reserved and cannot be set in
`zerops.yaml` `envVariables`. Two distinct failure shapes; knowing
which one you hit tells you where to look.

### Regime 1 — Hard-reserved, API-level (any scope)

The Zerops API rejects these at push time with structured error
`code: userDataUseOfSystemKey`. zcli surfaces the error before upload
so you see it inline. Case-sensitive exact match:

- `hostname` (lowercase — Zerops' service-injected service name)
- `PATH` (uppercase only — `Path` and `path` fall under regime 2)
- `serviceId`, `projectId`, `appVersionId`, `appVersionName`
- `zeropsSubdomain` (the fully-resolved URL — `zeropsSubdomainHost`
  and `zeropsSubdomainString` are NOT in this list and ARE overridable)

If the API rejects, the error names which key failed. Rename the key
(`MY_HOSTNAME`, `MY_PATH`, etc.) and retry.

### Regime 2 — Run-scope-only, runtime-init crash

These pass the API check but break runtime container startup when set
in `run.envVariables`. They're fine in `build.envVariables`. The
pattern: anything that conflicts with `PATH` or `HOSTNAME`
case-insensitively at runtime-init.

- `HOSTNAME` (uppercase)
- `Path` (capitalized)
- `path` (lowercase)

Symptom: `BUILD_FAILED` event in 4-5 seconds with **zero build logs**
and a generic baseline cause. The deploy response carries no specific
hint at this layer; the empty-logs shape is the signal.

Move these to `build.envVariables` if you genuinely need the override
during the build phase, or rename the key entirely (`APP_HOSTNAME`,
`APP_PATH`).

### Regime 3 — Platform-provided, overridable

These vars are platform-injected as OS env vars but the API
silently accepts a user override. The override shadows the
platform-provided value; that is rarely what you want.

- `apiCdnUrl`, `staticCdnUrl`, `storageCdnUrl`
- `envIsolation`, `sshIsolation`
- `zeropsSubdomainHost`, `zeropsSubdomainString`

Override only when there's a specific reason (e.g. routing through
a custom CDN). Default is to read the value Zerops provides.

### Not reserved — feel free to set when needed

Common Linux/runtime defaults Zerops provides but the API does not
restrict you from overriding:

- `USER`, `HOME`, `LOGNAME`, `SHELL`, `PWD`
- `PORT` (number or quoted string — both work)
- `NODE_ENV`, `APP_ENV` and other framework mode flags

---

### Checklist (dev-mode dynamic-runtime services)

Applies to **dynamic runtimes only** (Node, Bun, Deno, Go, Rust, Python,
Ruby, Java, .NET — anything with a long-running app process under
manual control). For implicit-webserver runtimes (`php-apache`,
`php-nginx`) the implicit-webserver guidance fires instead; for static
runtimes the web server auto-starts and this checklist does not apply.

- Dev setup block in `zerops.yaml`: **omit `run.start`**, **no**
  `healthCheck`. Zerops keeps the runtime container idle; you start
  the dev process yourself via `zerops_dev_server action=start` after
  each deploy.
- Stage setup block (if a dev+stage pair exists): real `start:`
  command **plus** a `healthCheck`. Stage auto-starts on deploy and
  Zerops probes it on its configured interval.

---

### Self-deploy destruction risk

In a self-deploy, `sourceService == targetService` — the runtime is both
the build source AND the destination. `deployFiles` selects which build
artifacts overwrite the runtime's deploy root. When that selection is
narrower than `[.]`, the result destroys the target.

When a self-deploying service uses a narrower deployFiles pattern (e.g. `[./out]`):

1. The build container assembles the artifact from the upload + any `buildCommands` output.
2. `deployFiles` selects — with a cherry-pick pattern, only the selected subset enters the artifact.
3. The runtime container's `/var/www/` is **overwritten** with that subset — source files disappear.
4. On subsequent self-deploys, `zerops_deploy` finds no source to upload — the target is unrecoverable without a manual re-push from elsewhere.

Client-side pre-flight rejects this with `INVALID_ZEROPS_YML` before any build triggers, so this failure mode cannot reach Zerops. (The atom fires for `closeDeployModes:[auto, manual, unset]` because git-push delivery uses cross-deploy semantics where this risk class doesn't apply.)

---

### Dynamic-runtime dev server

Dev-mode dynamic runtimes deploy with `run.start` omitted — the
runtime container idles and no dev process is live until you start
one. Action family on `zerops_dev_server`:

| Action | Use | Args |
|---|---|---|
| `status` | check before `start` (idempotent) — avoids duplicate listener | `hostname port healthPath` |
| `start` | spawn the dev process | `hostname command port healthPath` |
| `restart` | survives-the-deploy config/code change | `hostname command port healthPath` |
| `logs` | tail recent for diagnosis | `hostname logLines=40` |
| `stop` | end of session, free the port | `hostname port` |

Args:
- `command` — exact `run.start` from `zerops.yaml`.
- `port` — `run.ports[0].port`.
- `healthPath` — app-owned (`/api/health`, `/status`) or `/`.

Response carries `running`, `healthStatus`, `reason`, and `logTail`
— read these before making another call.

Don't hand-roll `ssh appdev "cmd &"`: the SSH session ends with
the call and kills the process. Always go through `zerops_dev_server`.

---

### Write the application code

Inspect `/var/www/<hostname>/` first. If the mount carries source — adapt
to the user's intent; preserve the existing scaffold rather than rewriting.
If empty — scaffold from scratch using the runtime + env-var catalog.
If `ls` errors (stale SSHFS), run `zerops_mount action="mount"` to recover
before deciding.

**Checklist before deploying:**

| Check | Requirement |
|---|---|
| Env vars | Read OS env at startup. Never hardcode connection strings, hosts, ports, or credentials; use bootstrap's discovered catalog. |
| Bind | Listen on `0.0.0.0`, not `localhost`/`127.0.0.1`; loopback can pass local tests but fail `zerops_verify`. |
| Start | `run.start` launches the production entry point as a long-running process. |
| Health | Add `/status` or `/health` returning HTTP 200 so `zerops_verify` has a deterministic endpoint; include a cheap dependency check when useful. |
| Framework defaults | For Streamlit, Gradio, Vite, Jupyter, etc., pin container-correct dev/proxy/headless settings in the framework config. Push-dev creates `/var/www/.git`, so auto-detecting dev mode from parent `.git/` misfires. Don't suppress dev mode — fix the operational mismatch and keep hot-reload. |

**Mount for files, SSH for commands.** Runtime CLIs (`go build`,
`php artisan`, `pytest`) need SSH because most are not on the ZCP host.

**Don't run `git init` from the ZCP-side mount.** Push-dev deploy
handlers manage the runtime container-side git state; running `git init` on
the SSHFS mount creates root-owned `.git/objects/` that breaks the
runtime container-side `git add`. Recovery: `ssh <hostname> "sudo rm -rf
/var/www/.git"` — the next redeploy re-initializes it.

---

### Knowledge on demand — where to pull extra context

When the embedded guidance is not enough, these are the canonical lookups:

- **`zerops.yaml` schema / field reference**:
  `zerops_knowledge query="zerops.yaml schema"`
- **Runtime-specific docs** (build tools, start commands, conventions):
  `zerops_knowledge query="<your runtime>"` — e.g. `nodejs`, `go`,
  `php-apache`, `bun`. Match the base stack name of the service you are
  working with.
- **Env var keys** (no values — safe by default):
  `zerops_discover includeEnvs=true`. Add `includeEnvValues=true` only
  for troubleshooting.
- **Infrastructure changes** (shared storage, scaling rules, nginx
  fragments): platform-rules guidance in the develop response covers
  base mechanics; deeper detail comes from `zerops_knowledge
  query="<topic>"`. For dev → standard mode expansion, start a new
  bootstrap session with `isExisting=true` on the existing runtime
  plus a `stageHostname` for the new stage pair.
- **Platform constants** (status codes, managed service categories,
  runtime classes): `zerops_knowledge query="<topic>"` — examples:
  `"service status"`, `"managed services"`, `"subdomain"`.

---

### Work session auto-close

Auto-close is gated on every in-scope service carrying `closeDeployMode ∈ {auto, git-push}`. Services with `closeDeployMode=unset` or `closeDeployMode=manual` BLOCK the auto-close trigger — the session stays open until you either pick a close-mode for those services or call `action="close"` explicitly.

When the gate is open (every in-scope service is `auto` or `git-push`), the session closes automatically under either of two conditions:

- **`auto-complete`** — every service in scope has both a successful
  deploy and a passing verify. The envelope's `workSession.closedAt`
  becomes set, `closeReason: auto-complete`, and `phase` flips to
  the closed state.
- **`iteration-cap`** — the workflow's retry ceiling was hit. Same
  close-state shape; `closeReason: iteration-cap`.

Explicit `zerops_workflow action="close" workflow="develop"` emits
the same closed state manually and is rarely needed — starting a new
task with a different `intent` replaces the session.

Close scope follows the session topology: standard-mode pairs include
BOTH halves, so skipping the stage cross-deploy leaves the session
active. Dev-only or simple services close after their one successful
deploy + verify.

Close is cleanup, not commitment. Work itself is durable — code is
in git, infrastructure is on Zerops.

---

### Run the first deploy

The Zerops container is empty until the deploy call lands, so probing
its subdomain or (in container env) SSHing into it first will fail or
hit a platform placeholder — deploy first, then inspect. `zerops_deploy`
batches build + runtime container provision + start. The call returns
when build completes; runtime container start is a separate phase
surfaced by `failureClassification.failedPhase` if it fails — read
that field rather than waiting on a fixed timeout.

If `status` is non-success, read `failureClassification` first — it
carries the matched `category`, `likelyCause`, and `suggestedAction`
distilled from the logs. Only fall through to `buildLogs` /
`runtimeLogs` when the classification is missing or its
`suggestedAction` doesn't match what you observe. A second attempt on
the same broken `zerops.yaml` burns another deploy slot without new
information.

On first-deploy success the response carries `subdomainAccessEnabled:
true` and a `subdomainUrl` — no manual `zerops_subdomain` call is
needed in the happy path. Run verify next.

If you imported a service that you deliberately want to keep without a
public subdomain (internal-only HTTP service), call `zerops_subdomain
action="disable"` after the deploy.

Run for each runtime that hasn't been deployed:

```
zerops_deploy targetService="appdev"
```

---

### Per-service verify matrix

Deploy success does not prove user behavior. Use `zerops_discover`:
subdomain URL means web-facing; managed/no HTTP port means non-web.

Run `zerops_verify` first. If any returned check has a `recovery` field,
execute that recovery (`tool` + `action` + `args`) and re-run verify before
any browser/HTTP probe.

If you adopted or imported a service that you deliberately want to keep
without a public subdomain (internal-only HTTP service), call
`zerops_subdomain action="disable"` after the next deploy.

| Service shape | Required check |
|---|---|
| Non-web: managed DB/cache/worker/no HTTP port | Run `zerops_verify serviceHostname="{targetHostname}"`. `status=healthy` is enough; nothing to browse. |
| Web-facing: dynamic/static/implicit-webserver with subdomain/port | Run `zerops_verify` for infrastructure, then a verify agent using `agent-browser`. Tool healthy + rendered page proves the service; either failure blocks. |

Fetch the web-agent protocol only when needed:

```
zerops_knowledge query="verify web agent protocol"
```

It has the `Agent(model="sonnet", prompt=...)` template; substitute
`{targetHostname}` and `{runtime}`.

### Verdict protocol

- **VERDICT: PASS** → service verified, proceed.
- **VERDICT: FAIL** → visual/functional issue; iterate from the agent's
  evidence.
- **VERDICT: UNCERTAIN** → fall back to `zerops_verify`; the agent could
  not determine the outcome.
- **Malformed output or timeout** → UNCERTAIN; fall back to `zerops_verify`.

---

### Before verify on dev-mode dynamic runtimes

Dev-mode dynamic runtimes deploy with `run.start` omitted — nothing is
listening yet. `zerops_verify` will return `http_root: HTTP 502` and
that is NOT a deploy failure. Start the dev process via
`zerops_dev_server action=start` first, then verify.

For simple-mode and standard-mode runtimes the runtime starts on
deploy; verify directly.

### Verify the first deploy

After running `zerops_verify`, the returned `status` is `healthy`,
`degraded`, or `unhealthy`; scan `checks[]` for any with `status: fail`
and read its `detail` for the specific failure. The verify flow picks
the right check route per service shape (web / worker / managed).

**If unhealthy:**

1. Run `zerops_logs severity="error" since="5m"` — the start or
   request error is in the log.
2. Common first-deploy misconfigs, in frequency order:
   - App bound to `localhost` instead of `0.0.0.0`.
   - `run.start` invokes a build command rather than the entry point.
   - `run.ports.port` doesn't match what the app actually listens on.
   - Env var name drift — check `${hostname_KEY}` spelling against
     the discovered catalog.
3. Fix in place, redeploy, re-verify. Stop after 5 unsuccessful
   attempts and reassess.

Run for each runtime that hasn't been deployed:

```
zerops_verify serviceHostname="appdev"
```

---

### Platform rules — container additions

- **Mount caveats.** Mount is the build source for each new container.
  Never `ssh <hostname> cat/ls/tail …` for mount files — SSH adds
  shell-escape bugs (nested quotes in `sed`/`awk` break). One-shot
  SSH is for runtime CLIs only.
- **Long-running dev processes → `zerops_dev_server`.** Don't
  hand-roll `ssh <hostname> "cmd &"` — backgrounded SSH holds the
  channel until the 120 s bash timeout. The dev-server response
  carries `running`, `healthStatus`, `startMillis`, and on failure
  a `reason` code — read it before another call.
- **One-shot commands over SSH.** Framework CLIs, git ops,
  `curl localhost` exit quickly — no channel-lifetime concern:

  ```
  ssh <hostname> "cd /var/www && npm install"
  ssh <hostname> "cd /var/www && php artisan migrate"
  ssh <hostname> "curl -s http://localhost:{port}/api/health"
  ```

- **Mount recovery.** If the SSHFS mount goes stale after a deploy
  (stat/ls returns empty, writes hang), remount: `zerops_mount action="mount"`.
- **Agent Browser** — `agent-browser.dev` is available on the ZCP host
  for browser-backed verify checks (`zerops_verify` selects the right
  route per service shape).

---

### Deploy config — current axes + how to change

Each runtime service has three orthogonal deploy-config axes — the
rendered Services block shows them as
`closeMode=auto|git-push|manual gitPush=unconfigured|configured|broken|unknown buildIntegration=none|webhook|actions`:

- `closeMode` — what the develop close action does. `auto` runs
  `zerops_deploy` directly (zcli push); `git-push` commits + pushes
  to a configured remote so Zerops/CI builds; `manual` yields to
  you for orchestration. `unset` is the bootstrap-written
  placeholder that develop converts on first use.
- `gitPush` — capability state for the git-push path. `configured`
  means the last `git-push-setup` probe **proved end-to-end auth**: the
  supplied token authenticates against the remote URL, project env carries
  `GIT_TOKEN` (sensitive), and the working tree's git config has its
  `origin` synced. `unconfigured` / `broken` indicate setup is
  needed before `closeMode=git-push` can fire (`broken` means a previously-
  configured token stopped working, e.g. PAT rotation).
- `buildIntegration` — the ZCP-managed CI shape that was picked. `actions`
  (GitHub Actions workflow + secrets), `webhook` (Zerops dashboard OAuth),
  or `none`. Requires `gitPush=configured`. The flag records the choice
  and the handoff shape (workflow YAML body / dashboard URL); workflow
  commit, secrets landing, and OAuth completion happen outside ZCP's
  reach and are not verified by this flag. Treat as "this is the
  integration shape we wired", not "the build trigger is confirmed live".

Switch any axis without closing the session — three actions, each
operating at a different scope:

- `close-mode` is **per-pair** under the hood (one ServiceMeta per dev/stage pair) but accepts a multi-entry map: one call sets close-mode for any subset of services. Passing both halves of a pair with the SAME value is accepted (canonical write once). Passing both halves with DIFFERENT values is rejected with an explicit conflict diagnostic — pick one value for the pair.
- `git-push-setup` and `build-integration` are **per-pair**: capability is stamped on the dev half's meta and shared by both halves. `git-push-setup` rejects stage-half input with `INVALID_PARAMETER` (it mutates push-side state — would write to the wrong target). `build-integration` is permissive: pair-keyed lookup resolves either half to the dev meta and the response carries `pushSource`/`buildTarget`/`topologyNote` so the redirect is visible. Either way: prefer passing the dev half directly.

```
zerops_workflow action="close-mode" closeMode={"appdev":"auto"}
zerops_workflow action="git-push-setup" service="appdev" remoteUrl="..."
zerops_workflow action="build-integration" service="appdev" integration="actions"
```

Substitute `appdev` with the dev-half hostname (or single-runtime hostname). For a multi-service project, repeat each call once per dev-half service — never per stage-half.

Mixed config across services in one project is fine — each service's three axes are independent in the envelope.
