---
id: develop/failure-tier-3
atomIds: [develop-first-deploy-intro, develop-env-var-model, develop-change-drives-deploy, develop-deploy-modes, develop-env-var-channels, develop-first-deploy-env-vars, develop-first-deploy-scaffold-yaml, develop-http-diagnostic, develop-nodejs-greenfield-buildhint, develop-platform-rules-common, develop-reserved-env-names, develop-checklist-dev-mode, develop-deploy-files-self-deploy, develop-dynamic-runtime-start-container, develop-first-deploy-write-app, develop-knowledge-pointers, develop-auto-close-semantics, develop-first-deploy-execute, develop-verify-matrix, develop-first-deploy-verify, develop-platform-rules-container, develop-strategy-awareness]
description: "Active session, third-iteration failure history — three failed deploy attempts on appdev (close-mode auto)."
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

When destination == source the value resolves to the literal string `${db_hostname}` (not the resolved value), reaches `process.env` as that literal, and the app fails at connect/parse time.

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

**Deploy modes — self-deploy vs cross-deploy** — pull on demand: `zerops_workflow action="develop-atom" atomId="develop-deploy-modes"`

---

**Env var channels** — pull on demand: `zerops_workflow action="develop-atom" atomId="develop-env-var-channels"`

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

### Node.js — `npm install`, not `npm ci`

Fresh Node scaffold with no committed `package-lock.json`: `npm install` in `build.buildCommands`. `npm ci` fails with `EUSAGE` until a lockfile is committed.

---

**Platform rules** — pull on demand: `zerops_workflow action="develop-atom" atomId="develop-platform-rules-common"`

---

**Reserved env-var keys** — pull on demand: `zerops_workflow action="develop-atom" atomId="develop-reserved-env-names"`

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
one. The dev server is unsupervised, so the URL 502s after any
container cycle until restarted: a passing verify means "live now",
not "durably shipped". For an always-on service use simple mode.
Action family on `zerops_dev_server`:

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

### Knowledge on demand — pull extra context

When the embedded guidance isn't enough, these are the canonical lookups:

- **`zerops.yaml` schema / fields** — `zerops_knowledge query="zerops.yaml schema"`
- **Runtime docs** (build tools, start commands, conventions) —
  `zerops_knowledge query="<runtime>"` (e.g. `nodejs`, `go`, `php-nginx`, `bun`);
  match the service's base stack.
- **Env var keys** (no values, safe) — `zerops_discover includeEnvs=true`
  (`includeEnvValues=true` only to troubleshoot).
- **Deeper platform topics** (infra changes, scaling, status codes,
  managed-service categories) — `zerops_knowledge query="<topic>"`.

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

**Platform rules — mount & SSH usage** — pull on demand: `zerops_workflow action="develop-atom" atomId="develop-platform-rules-container"`

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
