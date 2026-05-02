---
id: develop/first-deploy-dev-dynamic-container
atomIds: [develop-first-deploy-intro, develop-api-error-meta, develop-change-drives-deploy, develop-deploy-modes, develop-env-var-channels, develop-first-deploy-env-vars, develop-first-deploy-scaffold-yaml, develop-http-diagnostic, develop-platform-rules-common, develop-checklist-dev-mode, develop-deploy-files-self-deploy, develop-dynamic-runtime-start-container, develop-first-deploy-write-app, develop-knowledge-pointers, develop-auto-close-semantics, develop-first-deploy-execute, develop-verify-matrix, develop-first-deploy-verify, develop-platform-rules-container, develop-strategy-awareness]
description: "develop-active, dev mode, never-deployed dynamic runtime, in-container."
---
### You're in the develop first-deploy branch

The envelope reports at least one in-scope service with
`deployed: false` (bootstrapped but never received code). Finish that
here: establish `zerops.yaml` and the app, deploy, verify.

Flow for each never-deployed runtime:

1. **Establish `zerops.yaml`** — scaffold if absent, refine in place if
   already present (see `develop-first-deploy-scaffold-yaml`).
2. **Establish the application code** — adapt existing source if the
   mount carries it, scaffold real code otherwise (see
   `develop-first-deploy-write-app`).
3. **Run `zerops_deploy targetService=<hostname>`** with NO `strategy`
   argument. Every first deploy uses the default push path;
   `strategy=git-push` requires `GIT_TOKEN` + committed code
   (container) or a configured git remote (local), neither ready yet.
4. **Verify** (see `develop-verify-matrix` for per-service path). Close
   and completion semantics are in `develop-auto-close-semantics`.

Auto-close is gated on `closeDeployMode` being set for every in-scope
service — `unset` blocks the close even after deploy + verify pass.
The Services block names each service's current value (`closeMode=auto|
git-push|manual|unset`); `unset` reads from a bootstrap that didn't
declare a strategy. Set it for each in-scope service:

```
zerops_workflow action="close-mode" closeMode={"<host>":"auto"}
```

`develop-strategy-awareness` covers all three axes (closeMode,
gitPush, buildIntegration) and the per-service mix.

Don't skip to edits before the first deploy lands — HTTP probes
return errors before any code is delivered.

---

### Read `apiMeta` on every error response

Any `zerops_*` tool surfacing a Zerops API 4xx may include `apiMeta`.
Missing key = no server detail; present key = exact rejected fields.

Shape:

```json
{
  "code": "API_ERROR",
  "apiCode": "projectImportInvalidParameter",
  "error": "Invalid parameter provided.",
  "suggestion": "Zerops flagged specific fields — see apiMeta for each field's failure reason.",
  "apiMeta": [
    {
      "code": "projectImportInvalidParameter",
      "error": "Invalid parameter provided.",
      "metadata": {
        "storage.mode": ["mode not supported"]
      }
    }
  ]
}
```

Each `apiMeta[].metadata` key is a **field path** (`<host>.mode`,
`build.base`, `parameter`); values list reasons. Fix those YAML fields
and retry — do not guess.

Common `apiCode` shapes:

| `apiCode` | `metadata` key | Meaning |
|---|---|---|
| `projectImportInvalidParameter` | `<host>.mode` | type/mode combination not allowed |
| `projectImportMissingParameter` | `parameter` (value `<host>.mode`) | required field missing |
| `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |
| `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |
| `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |

Per-service import failures use `serviceErrors[].meta` with the same
shape, one entry per failing service-stack.

---

### Every code change must reach a durable state

Iteration cadence is mode-specific:

- Dev-mode dynamic runtime container: see
  `develop-close-mode-auto-workflow-dev`.
- Simple / standard / local / first-deploy: every change →
  `zerops_deploy`.

Auto-close: see `develop-auto-close-semantics`.

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

**Shadow-loop pitfall**: `zerops_env`-set service-level vars shadow
the same key in `run.envVariables`. Fixing only `zerops.yaml` won't
change live value — delete the service-level key
(`zerops_env action="delete"`) before redeploy.

---

### Env var catalog from bootstrap

Managed services expose env var keys that your runtime should reference.
Fetch the actual key list with `zerops_discover service="<hostname>"
includeEnvs=true` per managed service and use those keys verbatim — **do
not guess alternatives**. The catalog is the authoritative source; the
host key is **`hostname`** (never `host`), but every other key varies
per service type, so don't hardcode from memory.

Place runtime env vars in `run.envVariables`; channel timing and
service-level shadowing rules are in `develop-env-var-channels`.
Cross-service references use this form:

```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  DB_HOST: ${db_hostname}
```

Zerops rewrites `${db_connectionString}` at deploy time from service
`db`'s `connectionString`; a wrong spelling remains literal and the
app fails at connect time.

**Re-check at any point:** `zerops_discover service="<hostname>"
includeEnvs=true` returns the key list. Values are redacted by default;
names alone are enough for cross-service wiring. Add
`includeEnvValues=true` only for troubleshooting.

### Per-service canonical env-var reference

Patterns for the most common managed-service types when wiring `run.envVariables`. Always verify against `zerops_discover` for the live key list — types update versions and key sets.

| Service | Use |
|---|---|
| PostgreSQL / MariaDB / ClickHouse | Prefer `connectionString` over assembling `hostname:port:user:password:dbName`. PostgreSQL + ClickHouse expose `superUser` / `superUserPassword` for DDL; ClickHouse port must match the driver (`portHttp`, `portMysql`, `portNative`, `portPostgresql`). |
| Valkey / KeyDB | No auth on private network; KeyDB has no TLS port. |
| NATS | URI is `connectionString`. |
| Kafka | No `connectionString`; build broker URL from `hostname:port`. |
| Elasticsearch | HTTP basic auth via `user`/`password`. |
| Meilisearch | Use scoped keys (`defaultAdminKey`, `defaultSearchKey`, `defaultReadOnlyKey`, `defaultChatKey`), not `masterKey`. |
| Typesense | Single `apiKey`. |
| Qdrant | HTTP + gRPC via `connectionString` / `grpcConnectionString`; read-only via `readOnlyApiKey`. |
| object-storage | S3-compatible: `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName`; no `region` env var. |
| shared-storage | `hostname` only; mounted via `mount:` in zerops.yaml, not a network service. |

---

### Establish `zerops.yaml`

Scaffold `zerops.yaml` if absent or refine it in place if already present.
Root placement and `setup:` naming rules are in `develop-platform-rules-common`.

**Shape (one `zerops:` block per targeted runtime hostname):**

```yaml
zerops:
  - setup: <hostname>
    build:
      base: <runtime-only key, e.g. nodejs@22 — NOT the composite run key>
      buildCommands: [...]       # optional for pre-built artefacts
      deployFiles: [...]         # see develop-deploy-modes for deployFiles per class
    run:
      base: <run key, may be composite: php-nginx@8.4, nodejs@22, ...>
      ports:
        - port: <app-listens-on>
          httpSupport: true
      envVariables:
        <KEY>: <value or ${service_KEY} cross-ref>
      start: <run command, not a build command>
```

**Env var references** — see `develop-first-deploy-env-vars` for
`${hostname_KEY}` syntax and `develop-env-var-channels` for live timing.

**Mode-aware tips:** emit separate setup entries per targeted hostname.
See `develop-deploy-modes` for deployFiles by deploy class.

---

### HTTP diagnostics

For 500 / 502 / empty body, stop at the first useful signal; do **not**
default to
`ssh appdev curl localhost` for diagnosis.

1. **`zerops_verify serviceHostname="appdev"`** — start with the
   canonical health probe and structured diagnosis; see
   `develop-verify-matrix` for the full verify path.
2. **Subdomain URL** — static / implicit-webserver:
   `https://appdev-${zeropsSubdomainHost}.prg1.zerops.app/`; dynamic
   adds `-{port}`. `${zeropsSubdomainHost}` is numeric and project-scope,
   not the projectId. Read it with `env | grep zeropsSubdomainHost`, or
   use `zerops_discover` for the resolved URL. Do not guess a UUID.
3. **`zerops_logs severity="error" since="5m"`** — recent platform errors
   (nginx, crash traces, deploy failures) without opening a shell.
4. **Framework log file** — read via Read tool at the framework's
   project-relative log path (`storage/logs/laravel.log`,
   `var/log/...`). Per-env access detail in
   `develop-platform-rules-container` (mount-vs-SSH split) and
   `develop-platform-rules-local` (CWD reads).
5. **Last resort: SSH + curl localhost** — only when earlier checks miss
   container-local state (worker-only service, non-default bind). Even
   then, `zerops_verify` usually already encodes the check.

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
- Env var live timing and cross-service syntax:
  `develop-env-var-channels` / `develop-first-deploy-env-vars`.
- Service config changes (shared storage, scaling, nginx fragments):
  use `zerops_import` with `override: true` to update existing services.
  This is separate from `zerops_deploy`, which only updates code.
  **Destructive**: override REPLACES the service stack — the running
  container, deployed code, per-service env vars, and any
  work-in-progress on the service's filesystem are all torn down. The
  response Warnings name the replaced hostnames; back up first.

---

### Checklist (dev-mode dynamic-runtime services)

Applies to **dynamic runtimes only** (Node, Bun, Deno, Go, Rust, Python,
Ruby, Java, .NET — anything with a long-running app process under
manual control). For implicit-webserver runtimes (`php-apache`,
`php-nginx`) see `develop-implicit-webserver`; for static runtimes the
web server auto-starts and this checklist does not apply.

- Dev setup block in `zerops.yaml`: `start: zsc noop --silent`, **no**
  `healthCheck`. Zerops keeps the runtime container idle; you start
  the dev process yourself via `zerops_dev_server action=start` after
  each deploy.
- Stage setup block (if a dev+stage pair exists): real `start:`
  command **plus** a `healthCheck`. Stage auto-starts on deploy and
  Zerops probes it on its configured interval.

---

### Self-deploy destruction risk

`develop-deploy-modes` covers the high-level self-deploy vs cross-deploy classification + the deployFiles table. This atom drills into the specific destruction-risk path that motivates the `[.]` invariant.

When a self-deploying service uses a narrower deployFiles pattern (e.g. `[./out]`):

1. The build container assembles the artifact from the upload + any `buildCommands` output.
2. `deployFiles` selects — with a cherry-pick pattern, only the selected subset enters the artifact.
3. The runtime container's `/var/www/` is **overwritten** with that subset — source files disappear.
4. On subsequent self-deploys, `zerops_deploy` finds no source to upload — the target is unrecoverable without a manual re-push from elsewhere.

Client-side pre-flight rejects this with `INVALID_ZEROPS_YML` before any build triggers, so this failure mode cannot reach Zerops. (The atom fires for `closeDeployModes:[auto, manual, unset]` because git-push delivery uses cross-deploy semantics where this risk class doesn't apply.)

---

### Dynamic-runtime dev server

Dev-mode dynamic runtime containers start running `zsc noop` after
deploy — no dev process is live until you start one. Action family
on `zerops_dev_server`:

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

**After every redeploy, re-run `action=start` before `zerops_verify`** —
the rebuild drops the dev process (see `develop-platform-rules-common`).
The hand-roll `ssh appdev "cmd &"` anti-pattern is in
`develop-platform-rules-container`. See `develop-dev-server-reason-codes`
for `reason` values.

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

**Mount for files, SSH for commands** — see
`develop-platform-rules-container`. Runtime CLIs (`go build`,
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
  fragments): see `develop-platform-rules-common`. For dev → standard
  mode expansion, start a new bootstrap session with `isExisting=true`
  on the existing service plus a `stageHostname` for the new stage pair.
- **Platform constants** (status codes, managed service categories,
  runtime classes): `zerops_knowledge query="<topic>"` — examples:
  `"service status"`, `"managed services"`, `"subdomain"`.

---

### Work session auto-close

Auto-close is gated on every in-scope service carrying `closeDeployMode ∈ {auto, git-push}`. Services with `closeDeployMode=unset` or `closeDeployMode=manual` BLOCK the auto-close trigger — the session stays open until you either pick a close-mode for those services or call `action="close"` explicitly. (Verified by `internal/workflow/work_session_test.go::TestEvaluateAutoClose` — `unset_blocks` and `manual_blocks` both return `want: false`.)

When the gate is open (every in-scope service is `auto` or `git-push`), the session closes automatically under either of two conditions:

- **`auto-complete`** — every service in scope has both a successful
  deploy and a passing verify. The envelope's `workSession.closedAt`
  becomes set, `closeReason: auto-complete`, and `phase` flips to
  `develop-closed-auto`.
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

### Verify the first deploy

After running `zerops_verify`, the returned `status` is `healthy`,
`degraded`, or `unhealthy`; scan `checks[]` for any with `status: fail`
and read its `detail` for the specific failure. For route selection
between non-web and browser-backed checks, see `develop-verify-matrix`.

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

Auto-close behavior is described in `develop-auto-close-semantics`.

---

### Platform rules — container additions

Mount basics in `claude_container.md` (boot shim). Container-only
cautions on top:

- **Mount caveats.** Mount is the build source for each new container.
  Never `ssh <hostname> cat/ls/tail …` for mount files — SSH adds
  shell-escape bugs (nested quotes in `sed`/`awk` break). One-shot
  SSH is for runtime CLIs only.
- **Long-running dev processes → `zerops_dev_server`.** Don't
  hand-roll `ssh <hostname> "cmd &"` — backgrounded SSH holds the
  channel until the 120 s bash timeout. See
  `develop-dynamic-runtime-start-container` for actions, parameters,
  and response shape; `develop-dev-server-reason-codes` for `reason`
  triage.
- **One-shot commands over SSH.** Framework CLIs, git ops,
  `curl localhost` exit quickly — no channel-lifetime concern:

  ```
  ssh <hostname> "cd /var/www && npm install"
  ssh <hostname> "cd /var/www && php artisan migrate"
  ssh <hostname> "curl -s http://localhost:{port}/api/health"
  ```

- **Mount recovery.** If the SSHFS mount goes stale after a deploy
  (stat/ls returns empty, writes hang), remount: `zerops_mount action="mount"`.
- **Agent Browser** — `agent-browser.dev` is available on the ZCP host;
  see `develop-verify-matrix` for the web verification path.

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
  means GIT_TOKEN + .netrc + remote URL are stamped; `unconfigured`
  / `broken` / `unknown` indicate setup is needed before
  `closeMode=git-push` can fire.
- `buildIntegration` — ZCP-managed CI shape. `none` (default),
  `webhook` (Zerops webhook drives the build), or `actions` (GitHub
  Actions workflow YAML). Requires `gitPush=configured`.

Switch any axis without closing the session — three actions, each
operating at a different scope:

- `close-mode` is **per-service** and accepts a multi-entry map: one call sets close-mode for any subset of services in one shot. For a standard pair, set both halves in the same call.
- `git-push-setup` and `build-integration` are **per-pair**: call only on the dev half (or single-runtime hostname). The handler rejects stage-half targets with `INVALID_PARAMETER` because both halves of a pair share the same git-push / build-integration capability stamped on the dev meta.

```
zerops_workflow action="close-mode" closeMode={"appdev":"auto"}
zerops_workflow action="git-push-setup" service="appdev" remoteUrl="..."
zerops_workflow action="build-integration" service="appdev" integration="webhook"
```

Substitute `appdev` with the dev-half hostname (or single-runtime hostname). For a multi-service project, repeat each call once per dev-half service — never per stage-half.

Mixed config across services in one project is fine — each service's three axes are independent in the envelope.
