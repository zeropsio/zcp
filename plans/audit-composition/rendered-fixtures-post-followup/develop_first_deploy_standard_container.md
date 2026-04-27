## Status
Phase: develop-active
Services: appdev, appstage
  - appdev (nodejs@22) — bootstrapped=true, mode=standard, strategy=unset, stage=appstage, deployed=false
  - appstage (nodejs@22) — bootstrapped=true, mode=stage, strategy=unset, deployed=false
Guidance:
  ### You're in the develop first-deploy branch

  The envelope reports at least one in-scope service with
  `deployed: false` (bootstrapped but never received code). Finish that
  here: scaffold `zerops.yaml`, write the app, deploy, verify.

  Flow for each never-deployed runtime:

  1. **Scaffold `zerops.yaml`** from the planned runtime + env-var
     catalog from `zerops_discover` (see
     `develop-first-deploy-scaffold-yaml`).
  2. **Write the application code** that implements the user's intent —
     not a placeholder, real code.
  3. **Run `zerops_deploy targetService=<hostname>`** with NO `strategy`
     argument. Every first deploy uses the default push path;
     `strategy=git-push` requires `GIT_TOKEN` + committed code
     (container) or a configured git remote (local), neither ready yet.
  4. **Verify** (see `develop-verify-matrix` for per-service path). Close
     and completion semantics are in `develop-auto-close-semantics`.

  Don't skip to edits before the first deploy lands — SSHFS mounts can
  be empty and HTTP probes return errors before any code is delivered.
  ### Read `apiMeta` on every error response

  Any `zerops_*` tool that surfaces a 4xx from the Zerops API returns
  structured field-level detail on an optional `apiMeta` JSON key.
  Missing key = server sent no detail; present key = an array of items
  with the exact fields Zerops rejected.

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

  Each `apiMeta[].metadata` key is a **field path** (e.g.
  `appdev.mode`, `build.base`, `parameter`); each value lists the
  reasons. Fix those fields in your YAML and retry — do not guess.

  Common `apiCode` shapes:

  | `apiCode` | `metadata` key | Meaning |
  |---|---|---|
  | `projectImportInvalidParameter` | `<host>.mode` | service-type/mode combination not allowed |
  | `projectImportMissingParameter` | `parameter` (value `<host>.mode`) | required field missing |
  | `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |
  | `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |
  | `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |

  Per-service import failures land in `serviceErrors[].meta` with the
  same shape — one entry per failing service-stack.
  ### Every code change must reach a durable state

  Iteration cadence is mode-specific:

  - Dev-mode dynamic runtime container: see
    `develop-push-dev-workflow-dev`.
  - Simple / standard / local / first-deploy: every change →
    `zerops_deploy`.

  Auto-close: see `develop-auto-close-semantics`.
  ### Two deploy classes, one tool

  `zerops_deploy` has two classes determined by source vs target:

  | Class | Trigger | `deployFiles` constraint | Typical use |
  |---|---|---|---|
  | **Self-deploy** | `sourceService == targetService` (or `sourceService` omitted, auto-inferred to target) | MUST be `[.]` or `[./]` — narrower patterns destroy the target's source | dev services running `start: zsc noop --silent`; agent SSHes in and iterates on the code |
  | **Cross-deploy** | `sourceService != targetService`, or `strategy=git-push` | Cherry-picked from build output: `./out`, `./dist`, `./build` | dev→stage promotion; stage runs foreground binaries (`start: dotnet App.dll`, `start: node dist/server.js`) |

  Self-deploy refreshes a **mutable workspace**; cross-deploy produces an
  **immutable artifact** from the build container's post-`buildCommands`
  output.

  ### Picking deployFiles

  | Setup block purpose | deployFiles | Why |
  |---|---|---|
  | Self-deploy (dev, simple modes) | `[.]` | Anything narrower destroys target on deploy. |
  | Cross-deploy, preserve dir | `[./out]` | Artifact lands at `/var/www/out/...`. Pick when `start` references an explicit path (e.g. `./out/app/App.dll`) or multiple artifacts live in subdirs. |
  | Cross-deploy, extract contents | `[./out/~]` | Tilde strips `out/`; artifact lands at `/var/www/...`. Pick when the runtime expects assets at the root (ASP.NET's `wwwroot/` at `/var/www/`). |

  ### Why the source tree sometimes doesn't have `./out`

  `deployFiles` is evaluated against the **build container's filesystem
  after `buildCommands` runs** — NOT your editor's working tree. So
  `deployFiles: [./out]` is correct even when `./out` doesn't exist
  locally; the build creates it. See guide `deployment-lifecycle` for
  the full pipeline.

  ZCP pre-flight does NOT check path existence for cross-deploy; the
  Zerops builder emits `WARN: deployFiles paths not found: ...` in
  `DeployResult.BuildLogs` only when the build produces no matching files.
  ### Env var channels

  Two channels set env vars, and the channel determines when the value
  goes live.

  | Channel | Set with | When live |
  |---|---|---|
  | Service-level env | `zerops_env action="set"` | Response's `restartedServices` lists hostnames whose runtime containers were cycled; `restartedProcesses` has platform Process details. |
  | `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
  | `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |

  **Suppress restart**: pass `skipRestart=true` — response reports
  `restartSkipped: true`; `nextActions` tells you to restart manually
  (the value is **not live** until that restart). Partial failures
  land in `restartWarnings`; `stored` confirms which keys landed.

  **Shadow-loop pitfall**: `zerops_env`-set service-level vars shadow
  the same key in `run.envVariables`. Fixing only `zerops.yaml`
  won't change the live value — delete the service-level key
  (`zerops_env action="delete"`) before redeploy.
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
  ### Scaffold `zerops.yaml`

  Scaffold `zerops.yaml` before the first deploy. Root placement and
  `setup:` naming rules are in `develop-platform-rules-common`.

  **Shape (one `zerops:` block per runtime hostname the plan targets):**

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

  **Env var references** — see `develop-first-deploy-env-vars` for the
  discovered-key catalog and `${hostname_KEY}` syntax; see
  `develop-env-var-channels` for placement and live-timing rules.

  **Mode-aware tips:** emit separate setup entries for each targeted
  runtime hostname. See `develop-deploy-modes` for deployFiles per
  self-deploy vs cross-deploy class.

  **Content-root tip:** for runtimes that expect assets at the
  working-dir root (e.g. ASP.NET's `wwwroot/` lookup at
  `/var/www/wwwroot/`), use **tilde-extract** (`./out/~`) so contents
  land at `/var/www/` instead of `/var/www/out/`. Use **preserve**
  (`./out`) when `run.start` references an explicit subpath like
  `./out/app/App.dll`. Full decision rule in `develop-deploy-modes`.

  Schema: fetch `zerops.yaml` JSON Schema via `zerops_knowledge` if unsure.
  ### HTTP diagnostics

  When the app returns 500 / 502 / empty body, follow this order. Stop at
  whichever step resolves the error — do **not** default to
  `ssh appdev curl localhost` for diagnosis.

  1. **`zerops_verify serviceHostname="appdev"`** — start with the
     canonical health probe and structured diagnosis; see
     `develop-verify-matrix` for the full verify path.
  2. **Subdomain URL** — format is
     `https://appdev-${zeropsSubdomainHost}.prg1.zerops.app/` for static
     / implicit-webserver runtimes (php-nginx, nginx), `-{port}` appended
     for dynamic runtimes. `${zeropsSubdomainHost}` is a project-scope env
     var (numeric, not the projectId) injected into every runtime container. Read
     it on the host with `env | grep zeropsSubdomainHost`, or call
     `zerops_discover` which returns the resolved URL directly. Do not
     guess a UUID-shaped string.
  3. **`zerops_logs severity="error" since="5m"`** — surfaces recent error-
     level platform logs (nginx errors, crash traces, deploy failures)
     without opening a shell.
  4. **Framework log file on the mount** — read via Read tool (e.g.
     `/var/www/appdev/storage/logs/laravel.log`, `var/log/...`). See
     `develop-platform-rules-container` for the mount-vs-SSH split.
  5. **Last resort: SSH + curl localhost** — only when the above miss
     something container-local (e.g. worker-only service with no HTTP
     entrypoint; service bound to a non-default interface). Even then,
     `zerops_verify` usually already encodes the check.
  ### Platform rules

  - **Runtime container user is `zerops`, not root.** Package installs need `sudo`
    (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
  - **Deploy = new container.** Local files in the current runtime container are
    lost; only content covered by `deployFiles` survives across redeploys.
  - **`zerops.yaml` lives at the repo root.** Each `setup:` block (e.g.
    `prod`, `stage`, `dev`) is deployed independently — these are canonical
    recipe names, NOT hostnames.
  - **Build ≠ runtime container.** Runtime packages → `run.prepareCommands`;
    build-only packages → `build.prepareCommands`. Tools available at
    build time may not be at run time. See guide `deployment-lifecycle`
    for the full split.
  - Env var live timing and cross-service syntax:
    `develop-env-var-channels` / `develop-first-deploy-env-vars`.
  - Service config changes (shared storage, scaling, nginx fragments):
    use `zerops_import` with `override: true` to update existing services.
    This is separate from `zerops_deploy`, which only updates code.
  ### Self-deploy invariant

  Any service self-deploying MUST have `deployFiles: [.]` or `[./]` in
  the matching setup block. See `develop-deploy-modes` for how ZCP
  classifies self-deploy vs cross-deploy.

  A narrower pattern destroys the target's working tree on the next
  deploy:

  1. The build container assembles the artifact from the upload + any
     `buildCommands` output.
  2. `deployFiles` selects — with a cherry-pick pattern like `[./out]`,
     only the selected subset enters the artifact.
  3. The runtime container's `/var/www/` is **overwritten** with that subset —
     source files disappear.
  4. On subsequent self-deploys, `zcli push` finds no source to upload —
     the target is unrecoverable without a manual re-push from elsewhere.

  Client-side pre-flight rejects this with
  `INVALID_ZEROPS_YML` before any build triggers, so this failure mode
  cannot reach Zerops.

  Cross-deploy has opposite semantics; see `develop-deploy-modes` for
  the full contrast.
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
  ### Write the application code

  Bootstrap does NOT ship a verification stub or hello-world — `/var/www/<hostname>/`
  on the SSHFS mount is empty. The first deploy only succeeds if real code
  is there.

  **Checklist before deploying:**

  1. **Code reads env vars from the OS at startup.** Never hardcode
     connection strings or host/port/credentials — bootstrap's discovered
     catalog is the authoritative source.
  2. **App binds `0.0.0.0`** (not `localhost`/`127.0.0.1`). Zerops health
     checks call the service over the runtime container's external interface; a
     loopback-bound app reports as healthy in tests but fails in
     `zerops_verify`.
  3. **`run.start` invokes the production entry point** — must launch a
     long-running process.
  4. **Observability hook** — implement `/status` or `/health` returning
     HTTP 200 so `zerops_verify` has a deterministic endpoint. Embedding
     a cheap dependency check (e.g. DB ping) lets a failing verify
     immediately distinguish app bugs from wiring issues.
  5. **Audit "developer-friendly" framework defaults.** Iterative-dev
     frameworks (Streamlit, Gradio, Vite, Jupyter) are wrong-in-container
     in two ZCP-specific ways: push-dev creates `/var/www/.git` so any
     "auto-detect dev mode from parent `.git/`" heuristic mis-fires; and
     the runtime is behind L7 so `headless`/"reverse-proxy" framework
     flags need to be pinned to container-correct values. Pin each in
     the framework's **own config file** (CLI flags get lost on
     `run.start` rewrites). Don't suppress dev mode — fix the operational
     mismatch and keep hot-reload working.

  **Mount for files, SSH for commands** — see
  `develop-platform-rules-container` for the split. Runtime CLIs
  (`go build`, `php artisan`, `pytest`) need SSH because most aren't on
  the ZCP host.

  **Don't run `git init` from the ZCP-side mount.** Push-dev deploy
  handlers manage the runtime container-side git state; running `git init` on
  the SSHFS mount creates root-owned `.git/objects/` that breaks the
  runtime container-side `git add`. Recovery: `ssh <hostname> "sudo rm -rf
  /var/www/.git"` — the next redeploy re-initializes it.
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
  ### Work session auto-close

  Work sessions close automatically when either of two conditions hold:

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
  ### Run the first deploy

  The Zerops container is empty until the deploy call lands, so probing
  its subdomain or (in container env) SSHing into it first will fail or
  hit a platform placeholder — deploy first, then inspect. `zerops_deploy`
  batches build + runtime container provision + start; expect 30–90 seconds for
  dynamic runtimes and longer for `php-nginx` / `php-apache`.

  If `status` is non-success, read `buildLogs` / `runtimeLogs` /
  `failedPhase` before retrying — a second attempt on the same broken
  `zerops.yaml` burns another deploy slot without new information.

  On first-deploy success the response carries `subdomainAccessEnabled:
  true` and a `subdomainUrl` — no manual `zerops_subdomain` call is
  needed in the happy path. Run verify next.
  ```
  zerops_deploy targetService="appdev"
  ```
  ### Per-service verify matrix

  Deploy success does not prove the app works for end users. Pick the
  verification path per service based on what `zerops_discover` reports:
  subdomain URL present means web-facing; managed or no HTTP port means
  non-web.

  **Non-web services (managed databases, caches, workers, no subdomain):**

  ```
  zerops_verify serviceHostname="{targetHostname}"
  ```

  Tool returns `status=healthy` once Zerops can reach the service.
  That's the whole verification — nothing to browse.

  **Web-facing services (dynamic/static/implicit-webserver with subdomain
  or port):** run `zerops_verify` first for infrastructure baseline, then
  spawn a verify agent that drives `agent-browser` end-to-end. A healthy
  `zerops_verify` plus a rendered page together prove the service works;
  either failing is enough to block.

  Per web-facing target, fetch the sub-agent dispatch protocol on demand:

  ```
  zerops_knowledge query="verify web agent protocol"
  ```

  The protocol carries the full `Agent(model="sonnet", prompt=...)`
  template — substitute `{targetHostname}` and `{runtime}` per service
  when dispatching.

  ### Verdict protocol

  - **VERDICT: PASS** → service verified, proceed.
  - **VERDICT: FAIL** → agent found a visual/functional issue; enter the
    iteration loop with the agent's evidence as the diagnosis.
  - **VERDICT: UNCERTAIN** → fall back to the `zerops_verify` result (the
    agent could not determine the outcome end-to-end).
  - **Malformed agent output or timeout** → treat as UNCERTAIN and fall
    back to `zerops_verify`.
  ### Promote the first deploy to stage

  Standard mode pairs dev + stage. After `appdev` verifies,
  cross-deploy to `appstage`:

  ```
  zerops_deploy sourceService="appdev" targetService="appstage"
  zerops_verify serviceHostname="appstage"
  ```

  No second build — cross-deploy packages the dev tree straight into
  stage. Standard-pair close criteria are in
  `develop-auto-close-semantics`.
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

  Auto-close behavior is described in `develop-auto-close-semantics`.
  ```
  zerops_verify serviceHostname="appdev"
  ```
  ```
  zerops_verify serviceHostname="appstage"
  ```
  ### Platform rules

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
