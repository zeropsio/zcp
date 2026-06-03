---
id: develop/first-deploy-recipe-implicit-standard
atomIds: [develop-first-deploy-intro, develop-env-var-model, develop-change-drives-deploy, develop-deploy-modes, develop-env-cheatsheet-sql, develop-env-var-channels, develop-first-deploy-env-vars, develop-first-deploy-scaffold-yaml, develop-http-diagnostic, develop-implicit-webserver, develop-platform-rules-common, develop-reserved-env-names, develop-deploy-files-self-deploy, develop-first-deploy-write-app, develop-knowledge-pointers, develop-auto-close-semantics, develop-first-deploy-execute, develop-verify-matrix, develop-first-deploy-asset-pipeline-container, develop-first-deploy-promote-stage, develop-first-deploy-verify, develop-strategy-awareness]
description: "develop-active, mode=standard pair, php-nginx implicit-webserver runtime + db, never-deployed; bootstrap arrived via recipe route."
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

### Picking deployFiles

| Setup block purpose | deployFiles | Why |
|---|---|---|
| Self-deploy (dev, simple modes) | `[.]` | Anything narrower destroys target on deploy. |
| Cross-deploy, preserve dir | `[./out]` | Lands at `/var/www/out/...`; use when `start` references that path or artifacts live in subdirs. |
| Cross-deploy, extract contents | `[./out/~]` | Tilde strips `out/`; use when runtime expects assets at `/var/www/`. |

`deployFiles` is evaluated against the **build-container filesystem after `buildCommands`**, not the editor tree — `[./out]` is correct even when `./out` is absent from the source checkout (the build creates it). ZCP doesn't pre-check the path; the builder emits `WARN: deployFiles paths not found` in `DeployResult.BuildLogs` if it produces no matches.

---

### SQL database env keys

- **Postgres / MariaDB / MySQL** — `connectionString` is
  `protocol://${user}:${password}@$appdev:${port}` and **omits the
  db name**; append `/${db_dbName}` for Prisma / Drizzle / sqlx /
  SQLAlchemy / Sequelize (worked example in the env-var-model atom).
- **Prisma `migrate dev` P3014** — shadow DB needs DDL the regular user
  lacks: use `prisma db push` for fresh schemas, or pass the
  `${db_superUser}:${db_superUserPassword}` URL for that one call.
- **Elevated DDL** — `superUser`/`superUserPassword`, only when DDL
  needs them.

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
type. Values are redacted by default — names suffice; pass
`includeEnvValues=true` only to troubleshoot.

Per-managed-type key cheatsheets render for the dep types in THIS
project only. For exotic types, `zerops_knowledge query="<service>"`
returns the canonical page. Reserved-keys atom covers keys forbidden in
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

### Implicit-Webserver Runtime (`php-apache`, `php-nginx`)

Apache or nginx is bundled into the runtime image — **no manual `start:` and no `zerops_dev_server` cycling**. After deploy, the web server is already running and serves disk contents; before first deploy the runtime container exists but no web server has been provisioned yet (deploy is the moment that lands files + activates the server). **Do not SSH in to start a server** — there is no `{start-command}` to run.

**`zerops.yaml` differences vs. dynamic runtimes:**

- Omit `run.start` — leave the field out entirely.
- Omit `run.ports` — port 80 is fixed; Zerops handles it.
- Set `run.documentRoot` to the web-serving subtree. Laravel / Symfony /
  composer apps use `public`; root-serving apps omit it or set `.`.

**Deploy flow (both strategies):**

1. Write or edit application files.
2. Run the strategy-specific deploy (see the active strategy atom).
3. Verify as a web-facing service via `zerops_verify`.

**When 404/403 follows successful deploy:**

- Wrong `documentRoot` — the web server points at a directory that lacks
  the expected entrypoint.
- `.htaccess` / rewrite rules not shipped — `deployFiles` must include
  the files the web server needs, not just the PHP sources.

`zerops_logs` surfaces Apache / nginx errors for routing / permission
triage; there is no app process to crash.

---

### Platform rules

- **Runtime user is `zerops`, not root.** Package installs need `sudo`
  (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
- **Deploy = new container.** Local files in the current runtime container are
  lost; only content covered by `deployFiles` survives across redeploys.
- **Setup-block names depend on origin:** a recipe pre-authors `dev`/`prod`
  — don't rename those to hostnames. Authoring `zerops.yaml` from scratch you
  choose the name (a `setup:` per runtime hostname is fine). Each block
  deploys independently.
- **Build ≠ runtime container.** Runtime packages → `run.prepareCommands`;
  build-only packages → `build.prepareCommands`. Build-time tools may
  not exist at run time; see guide `deployment-lifecycle`.
- **`zerops_import override=true` is destructive** — REPLACES the
  service stack (container, code, env vars, filesystem). Reserved for
  explicit user-requested config changes (shared storage, scaling,
  nginx) that `zerops_deploy` can't handle. Never the default fix for
  hostname collisions, env drift, or unexpected state — pick a
  different hostname, adopt, or escalate. Back up first; Warnings
  name replaced hostnames.

---

### Reserved env-var keys

A few keys are platform-reserved in `zerops.yaml` `envVariables`, with two distinct failure shapes:

- **API-rejected at push** (`code: userDataUseOfSystemKey`, named inline by zcli): `hostname`, `PATH`, `serviceId`, `projectId`, `appVersionId`, `appVersionName`, `zeropsSubdomain`. Rename (`MY_HOSTNAME`) and retry.
- **Runtime-init crash** when set in `run.envVariables` — `HOSTNAME`, `Path`, `path` (anything colliding with `PATH`/`HOSTNAME` case-insensitively). Fine in `build.envVariables`. The symptom is the giveaway: `BUILD_FAILED` in 4-5s with **zero build logs**. Move to `build.envVariables` or rename (`APP_HOSTNAME`).

Platform-injected vars (`zeropsSubdomainHost`, `*CdnUrl`, `envIsolation`/`sshIsolation`) accept overrides but shadow the real value — override only with a reason. Common defaults (`USER`, `HOME`, `PORT`, `NODE_ENV`, …) are free to set.

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

Auto-close fires only when EVERY in-scope service carries `closeDeployMode ∈ {auto, git-push}` AND has a successful deploy + passing verify (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

Scope follows session topology — standard pairs include both halves. For dev-only work pass `outOfScope=["<stage>"]` on develop start; the stage half drops to a non-blocking reminder and the session closes on the dev half alone.

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

Verify every service after deploy — deploy success ≠ working app. Shape from
`zerops_discover`: subdomain URL = web-facing; managed / no HTTP port = non-web.
Run `zerops_verify` first; a check with a `recovery` field → run it, re-verify,
before any browser probe.

| Shape | Check |
|---|---|
| non-web (managed / worker / no HTTP port) | `zerops_verify` → `status=healthy` is the whole check |
| web (dynamic / static / implicit-webserver) | `zerops_verify` → judge `http_root`: `httpStatus` + `bodyText` + `consoleErrors`; healthy + a real body (not a blank shell / error page, no fatal console error) proves it |

When `bodyText`/`consoleErrors` are missing, truncated, or the page needs
interaction / SPA routes / non-root / auth, drive the browser **inline** with
`zerops_browser`. Never spawn a sub-agent, call raw `agent-browser`, or use `eval`.
Internal-only service (no public subdomain) → `zerops_subdomain action="disable"` after deploy.

- **VERDICT: PASS** — healthy + real rendered content; proceed.
- **VERDICT: FAIL** — healthy infra but blank/broken/error page, or a failing check; iterate from the check's `detail` + render evidence.
- **VERDICT: UNCERTAIN** — no render data + URL unreachable; fall back to `zerops_verify`.

---

### Frontend asset pipeline

`php-nginx` / `php-apache` services with a frontend build pipeline
(Laravel+Vite, Symfony+Encore, …) typically OMIT `npm run build` from dev
`buildCommands`. Dev assumes HMR via Vite over SSH, not a production asset
rebuild on every `zerops_deploy`.

**Consequence:** after first deploy, `public/build/manifest.json` is
missing. Vite helpers throw HTTP 500 ("Vite manifest not found"), so
`zerops_verify` fails before any framework bug.

**After the first `zerops_deploy` lands, BEFORE `zerops_verify`:**

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null appdev \
  'cd /var/www && npm run build'
```

The build writes `public/build/manifest.json` in the dev container;
SSHFS propagates it without redeploy. PHP-FPM reads it on next request —
no restart needed.

**For iterative frontend work, start Vite via the dev-server primitive** (one durable lifecycle per service — survives this MCP call, restarts cleanly with `action=status` / `action=restart`):

```
zerops_dev_server action=start hostname="appdev" command="npm run dev" port=5173 healthPath="/"
```

Vite drops `public/build/hot`; helpers route assets through it. The dev-server primitive tracks the process via the runtime container's lifecycle, so backgrounding is not your concern. New containers start on every `zerops_deploy` — re-run `action=start` after each redeploy.

**Do NOT add `npm run build` to dev `buildCommands`.** It defeats
HMR-first dev setup: every push rebuilds assets (~20–30 s penalty).

---

### Promote the first deploy to stage

Standard mode pairs dev + stage. After each dev runtime verifies,
cross-deploy it to its paired stage:

```
zerops_deploy sourceService="appdev" targetService="appstage" setup="prod"
zerops_verify serviceHostname="appstage"
```

Cross-deploy builds the dev source on stage; dev side unchanged.
Auto-close fires once both halves carry a successful deploy +
passing verify.

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
zerops_verify serviceHostname="appstage"
```

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
