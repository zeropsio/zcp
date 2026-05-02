---
id: develop/git-push-configured-webhook
atomIds: [develop-intro, develop-api-error-meta, develop-change-drives-deploy, develop-close-mode-git-push, develop-deploy-modes, develop-env-var-channels, develop-http-diagnostic, develop-platform-rules-common, develop-build-observe, develop-knowledge-pointers, develop-auto-close-semantics, develop-verify-matrix, develop-platform-rules-container, develop-strategy-awareness]
description: "Standard pair, close-mode git-push, GitPushState configured, BuildIntegration webhook."
---
### Development & Deploy

Infrastructure is provisioned and at least one runtime already has a
successful first deploy on record. You're in the edit loop: discover
the current state, implement the user's request, redeploy, verify.

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

- Dev-mode dynamic runtime: edit code in place; reload via
  `zerops_dev_server` (no full redeploy for code-only changes).
- Simple / standard / local / first-deploy: every change →
  `zerops_deploy`.

Once close-mode is `auto` or `git-push` and every in-scope service has
both a successful deploy and passing verify, the work session
auto-closes (`closeReason=auto-complete`).

---

This service is on `closeDeployMode=git-push`. Your delivery pattern is `zerops_deploy strategy="git-push"` — commits whatever is live in the workspace and pushes to the configured remote, then Zerops (or your CI) sees the push and runs the build separately. `action="close"` itself is a session-teardown call regardless of close-mode; run the push call below before invoking close.

## Push the build commit

```
zerops_deploy targetService="appdev" strategy="git-push"
```

The deploy tool fetches the working tree from `/var/www` (container) or the local workspace, ensures there's a fresh commit, and pushes to the configured remote on the configured branch. If `failureClassification.category=credential` surfaces (GIT_TOKEN or local credentials missing), re-run `zerops_workflow action="git-push-setup" service="appdev"` to refresh the capability.

## What runs the build depends on `BuildIntegration`

| `buildIntegration` | What happens after the push |
|---|---|
| `webhook` | Zerops dashboard pulls the repo and runs the build pipeline. Watch via `zerops_events serviceHostname="<hostname>"`. |
| `actions` | Your GitHub Actions workflow runs `zcli push` from CI; the build lands on the runtime. Same observation path. |
| `none` | The push is archived at the remote. No ZCP-managed build fires; if you have independent CI/CD, that may pick it up — ZCP doesn't track external CI. |

`build-integration=none` is a valid steady state if your team has independent CI/CD. The `Warnings` array surfaces a soft note when the deploy tool detects this combination — informational, not a blocker.

## When the build lands, ack it

For webhook + actions integrations, the build is async — `zerops_deploy strategy=git-push` returns as soon as the push transmits. After `zerops_events` confirms the build's appVersion went `Status: ACTIVE`, run record-deploy per service:

```
zerops_workflow action="record-deploy" targetService="appdev"
```

This records the deploy so the develop session sees the service as deployed and auto-close becomes eligible (the close-mode gate stays open under git-push).

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
- Env vars use `${hostname_KEY}` syntax for cross-service references
  (Zerops rewrites at deploy from the named service's catalog). Local
  vars in `run.envVariables` shadow project-level entries with the
  same key.
- Service config changes (shared storage, scaling, nginx fragments):
  use `zerops_import` with `override: true` to update existing services.
  This is separate from `zerops_deploy`, which only updates code.
  **Destructive**: override REPLACES the service stack — the running
  container, deployed code, per-service env vars, and any
  work-in-progress on the service's filesystem are all torn down. The
  response Warnings name the replaced hostnames; back up first.

---

With `build-integration=webhook` or `build-integration=actions`, `zerops_deploy strategy=git-push` returns as soon as the push transmits — the build runs separately and asynchronously. You observe it via `zerops_events`, then bridge the result back into the local develop session with `record-deploy`.

## Watch the appVersion

```
zerops_events serviceHostname="appdev"
```

A new push triggers a build appVersion per service. Look for:

| `Status` | Meaning |
|---|---|
| `BUILDING` | The build pipeline is running. Re-call `zerops_events` to advance. |
| `ACTIVE` | Build completed; the runtime now serves the new code. Proceed to record-deploy + verify. |
| `BUILD_FAILED` / `DEPLOY_FAILED` / `PREPARING_RUNTIME_FAILED` | Build or deploy failed. Read the latest event's `failureClass` (build / start / verify / network / config / credential / other) + `failureCause` for the structured diagnosis — same vocabulary the synchronous deploy path produces in DeployResult.FailureClassification. For full build-container output, tail `zerops_logs` per failing service (see command block below). Recovery is whatever fixed the build (yaml, missing env var, code issue) plus a fresh push. |

When a service surfaces a failure status, tail its build container output (per failing service):

```
zerops_logs serviceHostname="appdev" facility=application since=5m
```

The events tool is an envelope-aware lookup — pass `since=<duration>` to limit the window if the service has long history.

## Bridge the deploy back

Once `Status=ACTIVE`, run record-deploy per service:

```
zerops_workflow action="record-deploy" targetService="appdev"
```

This records each deploy so the next envelope render sees the service as deployed. From here the develop atoms gated on `deployStates: [deployed]` start firing, and a `zerops_verify` confirms the new code is healthy.

## When the build doesn't fire

`Warnings` on the push response calls out the case where `build-integration=none` left the push archived at the remote. If you intended to wire a build trigger, run `action=build-integration` now. If your team owns the CI side independently, ignore the warning — ZCP doesn't track external CI/CD by design.

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

Auto-close is gated on every in-scope service carrying `closeDeployMode ∈ {auto, git-push}`. Services with `closeDeployMode=unset` or `closeDeployMode=manual` BLOCK the auto-close trigger — the session stays open until you either pick a close-mode for those services or call `action="close"` explicitly. (Verified by `internal/workflow/work_session_test.go::TestEvaluateAutoClose` — `unset_blocks` and `manual_blocks` both return `want: false`.)

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

### Platform rules — container additions

Mount basics in `claude_container.md` (boot shim). Container-only
cautions on top:

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
