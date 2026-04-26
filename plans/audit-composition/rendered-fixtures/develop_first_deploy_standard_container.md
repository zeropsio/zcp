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
  4. **Verify** with `zerops_verify serviceHostname=<hostname>`. A
     passing verify combined with a recorded successful deploy flips
     the envelope's `deployed: true` on the next envelope build.

  Don't skip to edits before the first deploy lands — SSHFS mounts can
  be empty and HTTP probes return errors before any code is delivered.
  ### Read `apiMeta` on every error response

  Any `zerops_*` tool that surfaces a 4xx from the Zerops API returns
  structured field-level detail on an optional `apiMeta` JSON key.
  Missing key = server sent no detail; present key = an array of items
  with the exact fields the platform rejected.

  Shape:

  ```json
  {
    "code": "API_ERROR",
    "apiCode": "projectImportInvalidParameter",
    "error": "Invalid parameter provided.",
    "suggestion": "The platform flagged specific fields — see apiMeta for each field's failure reason.",
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

  Each `apiMeta[].metadata` key is a **field path** (e.g. `appdev.mode`,
  `build.base`, `parameter`); each value is the list of reasons. Fix those
  specific fields in your YAML and retry — do not guess.

  Common shapes you will see:

  - `projectImportInvalidParameter` with `metadata: {"{host}.mode": ["..."]}` —
    the service-type/mode combination is not allowed.
  - `projectImportMissingParameter` with `metadata: {"parameter": ["{host}.mode"]}` —
    a required field is missing.
  - `serviceStackTypeNotFound` with `metadata: {"serviceStackTypeVersion": ["nodejs@99"]}` —
    the version string is not in the platform catalog.
  - `zeropsYamlInvalidParameter` with `metadata: {"build.base": ["unknown ..."]}` —
    zerops.yaml validator caught the field before the build cycle.
  - `yamlValidationInvalidYaml` with `metadata: {"reason": ["line N: ..."]}` —
    YAML syntax error with line number.

  Per-service failures on import land in `serviceErrors[].meta` with the
  same shape — one entry per failing service-stack.
  ### Every code change must flow through the deploy strategy

  Editing files on the SSHFS mount (or locally in local mode) only
  persists across deploys when covered by `deployFiles` (see
  `develop-platform-rules-common` for the deploy-replaces-container
  invariant). The rule is:

  > **edit → deploy (via active strategy) → verify**

  Auto-close semantics are described in `develop-auto-close-semantics`;
  `closeReason` values you can observe are `auto-complete` (every
  in-scope service passed) and `iteration-cap` (retry ceiling hit).
  Explicit `zerops_workflow action="close" workflow="develop"` emits
  the same closed state; it's rarely needed because starting a new task
  with a different `intent` replaces the session. Session close is
  cleanup, not commitment — close always succeeds.
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
  | Cross-deploy, extract contents | `[./out/~]` | Tilde strips the `out/` prefix; artifact lands at `/var/www/...`. Pick when the runtime expects assets at root (ASP.NET's `wwwroot/` at ContentRootPath = `/var/www/`). |

  ### Why the source tree sometimes doesn't have `./out`

  `deployFiles` is evaluated against the **build container's filesystem
  after `buildCommands` runs**, not your editor's working tree. A
  cross-deploy `deployFiles: [./out]` is correct even when `./out`
  doesn't exist locally — the build creates it (`dotnet publish -o out`,
  `vite build`, `go build -o out/server`, etc.).

  ZCP pre-flight does NOT check path existence for cross-deploy; the
  Zerops builder emits `WARN: deployFiles paths not found: ...` in
  `DeployResult.BuildLogs` only when the build produces no matching files.
  ### Env var channels

  Two channels set env vars, and the channel determines when the value
  goes live.

  | Channel | Set with | When live |
  |---|---|---|
  | Service-level env | `zerops_env action="set"` | Response's `restartedServices` lists hostnames whose containers were cycled; `restartedProcesses` has platform Process details. |
  | `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
  | `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |

  To suppress the service-level restart, pass `skipRestart=true` — the
  response then reports `restartSkipped: true` and `nextActions` tells
  you to restart manually before the value is live. Partial failures
  surface in `restartWarnings`. Read `stored` to confirm exactly which
  keys landed.

  **Shadow-loop pitfall**: a service-level env var set via `zerops_env`
  shadows the same key declared in `run.envVariables`. If you set
  `DB_HOST` via `zerops_env` and later fix it in `zerops.yaml`,
  redeploys will not change the live value. Delete the service-level
  key first (`zerops_env action="delete"`), then redeploy.
  ### Env var catalog from bootstrap

  Managed services expose env var keys that your runtime should reference.
  Fetch the actual key list with `zerops_discover service="<hostname>"
  includeEnvs=true` per managed service and use those keys verbatim — **do
  not guess alternatives**. The catalog is the authoritative source; the
  host key is **`hostname`** (never `host`), but every other key varies
  per service type, so don't hardcode from memory.

  **Cross-service reference form** — inside `run.envVariables` of a
  runtime service:

  ```yaml
  envVariables:
    DATABASE_URL: ${db_connectionString}
    DB_HOST: ${db_hostname}
  ```

  The platform rewrites `${db_connectionString}` at deploy time by
  looking up service `db`'s env var named `connectionString`. A wrong
  spelling resolves to the literal string `${db_connectionString}` and
  the app fails at connect time.

  **Re-check at any point:** `zerops_discover service="<hostname>"
  includeEnvs=true` returns the key list. Values are redacted by default;
  names alone are enough for cross-service wiring. Add
  `includeEnvValues=true` only for troubleshooting.
  ### Scaffold `zerops.yaml`

  Write `zerops.yaml` at the repository root before the first deploy —
  without it, `zerops_deploy` fails at the validation stage and the agent
  wastes a deploy slot.

  **Shape (one `zerops:` block per runtime hostname the plan targets):**

  ```yaml
  zerops:
    - setup: <hostname>
      build:
        base: <runtime-only key, e.g. nodejs@22 — NOT the composite run key>
        buildCommands: [...]       # optional for pre-built artefacts
        deployFiles: [.]           # dev mode; stage uses the build output dir
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
  discovered-key catalog and the cross-service `${hostname_KEY}` syntax.

  **Mode-aware tips:**

  - `dev` mode: `deployFiles: [.]`, build runs on SSHFS, `run.start` wakes
    the container — no stage pair to worry about.
  - `simple` mode: identical layout to dev but single-slot, no stage.
  - `standard` mode: emit separate entries for dev AND stage hostnames;
    stage's `deployFiles` points at the build output directory.

  **Content-root tip (ASP.NET, static-serving frameworks):**

  When a foreground runtime expects assets at `ContentRootPath = CWD`
  (e.g. ASP.NET's `wwwroot/` lookup at `/var/www/wwwroot/`), use
  **tilde-extract** (`./out/~`) so contents land at `/var/www/` instead
  of `/var/www/out/`. Use **preserve** (`./out`) when `run.start`
  references an explicit subpath like `./out/app/App.dll`. See
  `develop-deploy-modes` for the full decision rule.

  Schema: fetch `zerops.yaml` JSON Schema via `zerops_knowledge` if unsure.
  ### HTTP diagnostics

  When the app returns 500 / 502 / empty body, follow this order. Stop at
  whichever step resolves the error — do **not** default to
  `ssh appdev curl localhost` for diagnosis.

  1. **`zerops_verify serviceHostname="appdev"`** — runs the canonical
     health probe (HEAD + `/health`/`/status`) and returns a structured
     diagnosis. This is always the first step.
  2. **Subdomain URL** — format is
     `https://appdev-${zeropsSubdomainHost}.prg1.zerops.app/` for static
     / implicit-webserver runtimes (php-nginx, nginx), `-{port}` appended
     for dynamic runtimes. `${zeropsSubdomainHost}` is a project-scope env
     var (numeric, not the projectId) injected into every container. Read
     it on the host with `env | grep zeropsSubdomainHost`, or call
     `zerops_discover` which returns the resolved URL directly. Do not
     guess a UUID-shaped string.
  3. **`zerops_logs severity="error" since="5m"`** — surfaces recent error-
     level platform logs (nginx errors, crash traces, deploy failures)
     without opening a shell.
  4. **Framework log file on the mount** — read directly via the `Read`
     tool (e.g. `/var/www/appdev/storage/logs/laravel.log`,
     `var/log/...`). Do NOT `ssh appdev tail …` — the mount exposes the
     same file, cheaper and without quote-escaping hazards.
  5. **Last resort: SSH + curl localhost** — only when the above miss
     something container-local (e.g. worker-only service with no HTTP
     entrypoint; service bound to a non-default interface). Even then,
     `zerops_verify` usually already encodes the check.
  ### Platform rules

  - **Container user is `zerops`, not root.** Package installs need `sudo`
    (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
  - **Deploy = new container.** Local files in the running container are
    lost; only content covered by `deployFiles` survives across deploys.
  - **`zerops.yaml` lives at the repo root.** Each `setup:` block (e.g.
    `prod`, `stage`, `dev`) is deployed independently — these are canonical
    recipe names, NOT hostnames.
  - **Build ≠ run container.** Runtime-only packages (`ffmpeg`,
    `imagemagick`) go in `run.prepareCommands`; build-only packages in
    `build.prepareCommands`. Tools available at build time may not be
    available at run time.
  - `envVariables` are declarative config, **not live** until a deploy.
    Never check them with `printenv` before deploying — they will not be
    set yet. Cross-service ref syntax + typo behavior is in
    `develop-env-var-channels` / `develop-first-deploy-env-vars`.
  - Service config changes (shared storage, scaling, nginx fragments):
    use `zerops_import` with `override: true` to update existing services.
    This is separate from `zerops_deploy`, which only updates code.
  ### Self-deploy invariant

  Any service self-deploying (`sourceService == targetService` — the
  default when sourceService is omitted; typical pattern for dev services
  and simple mode) MUST have `deployFiles: [.]` or `[./]` in the matching
  setup block.

  A narrower pattern destroys the target's working tree on the next
  deploy:

  1. Build container assembles the artifact from the upload + any
     `buildCommands` output.
  2. `deployFiles` selects — with a cherry-pick pattern like `[./out]`,
     only the selected subset enters the artifact.
  3. Runtime container's `/var/www/` is **overwritten** with that subset —
     source files disappear.
  4. On subsequent self-deploys, `zcli push` finds no source to upload —
     the target is unrecoverable without a manual re-push from elsewhere.

  Client-side pre-flight rejects this with
  `INVALID_ZEROPS_YML` before any build triggers, so this failure mode
  cannot reach the platform.

  ### Cross-deploy has opposite semantics

  Cross-deploy (`sourceService != targetService`, or
  `strategy=git-push`) ships build output to a **different** service —
  source is not at risk. Cross-deploy's `deployFiles` typically
  cherry-picks (`./out`, `./dist`, `./build`).
  See `develop-deploy-modes` atom for the full contrast.
  ### Dynamic-runtime dev server (container)

  Dev-mode dynamic-runtime containers start running `zsc noop` after
  deploy — no dev process is live until you start one.

  **Start:**

  ```
  zerops_dev_server action=start hostname=appdev command="{start-command}" port={port} healthPath="{path}"
  ```

  - `command`: the exact shell command from `run.start` in
    `zerops.yaml` (e.g. `npm run start:dev`, `bun run index.ts`,
    `python app.py`).
  - `port`: the HTTP port from `run.ports[0].port`.
  - `healthPath`: an app-owned path (`/api/health`, `/status`) if
    defined; else `/`.

  The response carries `running`, `healthStatus` (HTTP status of the
  health probe), `startMillis` (time from spawn to healthy), and on
  failure a concrete `reason` code plus `logTail` so you can diagnose
  without a follow-up call.

  **Check before starting (idempotent status):**

  ```
  zerops_dev_server action=status hostname=appdev port={port} healthPath="{path}"
  ```

  Call this BEFORE `action=start` when uncertain — avoids spawning a
  duplicate listener on a port already bound.

  **Restart after config or code change that survived the deploy:**

  ```
  zerops_dev_server action=restart hostname=appdev command="{start-command}" port={port} healthPath="{path}"
  ```

  **Tail recent logs for diagnosis:**

  ```
  zerops_dev_server action=logs hostname=appdev logLines=40
  ```

  **Stop at end of session or to free the port:**

  ```
  zerops_dev_server action=stop hostname=appdev port={port}
  ```

  **After every redeploy the container is new — the previous dev
  process is gone.** Re-run `action=start` before `zerops_verify`. Do
  not hand-roll `ssh appdev "cmd &"` — backgrounded commands hold
  the SSH channel open until the 120-second bash timeout because the
  child still owns stdio. See `develop-dev-server-reason-codes` for
  diagnosing `reason` values.
  ### Write the application code

  Bootstrap does NOT ship a verification stub or hello-world — `/var/www/<hostname>/`
  on the SSHFS mount is empty. The first deploy only succeeds if real code
  is there.

  **Checklist before deploying:**

  1. **Code reads env vars from the OS at startup.** Never hardcode
     connection strings or host/port/credentials — bootstrap's discovered
     catalog is the authoritative source.
  2. **App binds `0.0.0.0`** (not `localhost`/`127.0.0.1`). Zerops health
     checks call the service over the container's external interface; a
     loopback-bound app reports as healthy in tests but fails in
     `zerops_verify`.
  3. **`run.start` invokes the production entry point.** Not `npm install`,
     not `build` — the start command must launch a long-running process.
  4. **Observability hook** — implement `/status` or `/health` so
     `zerops_verify` has a deterministic endpoint to probe. Return 200 on
     success; embed a cheap dependency check (e.g. `SELECT 1` to the
     database) so a failing verify immediately tells you whether the
     problem is the app or the wiring.
  5. **Audit "developer-friendly" framework defaults.** Iterative-dev
     frameworks (Streamlit, Gradio, Vite, Jupyter) are wrong-in-container
     in two ZCP-specific ways: push-dev creates `/var/www/.git` so any
     "auto-detect dev mode from parent `.git/`" heuristic mis-fires; and
     the runtime is behind L7 so `headless`/"reverse-proxy" framework
     flags need to be pinned to container-correct values. Pin each in
     the framework's **own config file** (CLI flags get lost on
     `run.start` rewrites). Don't suppress dev mode — fix the operational
     mismatch and keep hot-reload working.

  **Write files** directly to `/var/www/<hostname>/` through the SSHFS
  mount — Read/Edit/Write tools (and plain `rm`, `mv`, `cp` against mount
  paths) all work because the mount bypasses container-side permissions
  at the SFTP protocol level.

  **Run commands** (`go build`, `php artisan`, `pytest`, framework CLIs,
  dev server) via SSH into the container: `ssh <hostname> "cd /var/www
  && <command>"`. The reason is tool availability, not ownership — most
  runtime-specific CLIs aren't installed on the ZCP host.

  **Don't run `git init` from the ZCP-side mount.** Push-dev deploy
  handlers manage the container-side git state — calling `git init` on
  the SSHFS mount (`cd /var/www/<hostname>/ && git init`) creates
  `.git/objects/` owned by root, which breaks the container-side
  `git add` the deploy handler runs. Recovery if this already happened:
  `ssh <hostname> "sudo rm -rf /var/www/.git"` — the next deploy
  re-initializes it.
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
    fragments, mode expansion): these are **not** code changes — they
    live in the import YAML. Update the relevant import fragment and
    re-apply via `zerops_import override=true`. For dev → standard mode
    expansion, start a new bootstrap session with `isExisting=true` on
    the existing service plus a `stageHostname` for the new stage pair.
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

  For standard-mode pairs, "every service in scope" includes BOTH
  halves — skipping the stage cross-deploy leaves the session active.
  For dev-only or simple services, a single successful deploy + verify
  is enough.

  Close is cleanup, not commitment. Work itself is durable — code is
  in git, infrastructure is on the platform.
  ### Run the first deploy

  The Zerops container is empty until the deploy call lands, so probing
  its subdomain or (in container env) SSHing into it first will fail or
  hit a platform placeholder — deploy first, then inspect. `zerops_deploy`
  batches build + container provision + start; expect 30–90 seconds for
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
  ```
  zerops_deploy targetService="appstage"
  ```
  ### Per-service verify matrix

  Verify every service in scope after a successful deploy — never assume
  deploy success means the app works for end users. Pick the verification
  path per service based on what `zerops_discover` reports (subdomain URL
  present = web-facing; managed or no HTTP port = non-web).

  **Non-web services (managed databases, caches, workers, no subdomain):**

  ```
  zerops_verify serviceHostname="{targetHostname}"
  ```

  Tool returns `status=healthy` once the platform can reach the service.
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
  stage. Auto-close requires BOTH halves verified; see
  `develop-auto-close-semantics`. Skipping stage leaves the session
  active and blocks auto-close.
  ### Verify the first deploy

  After running `zerops_verify`, the returned `status` is `healthy`,
  `degraded`, or `unhealthy`; scan `checks[]` for any with `status: fail`
  and read its `detail` for the specific failure. A passing verify
  corresponds to: service `status=ACTIVE` in `zerops_discover`, HTTP 200
  from the subdomain root (or configured `/status`), and every declared
  env var present at runtime.

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
  ### Platform rules (container environment)

  - **Code lives on an SSHFS mount** at `/var/www/<hostname>/` (one path
    per dev/simple service). A deploy rebuilds the container from mounted
    files; no transfer at deploy time.
  - **Read and edit directly on the mount.** Use Read/Edit/Write/Glob/Grep
    tools against `/var/www/<hostname>/` — they work through SSHFS. Never
    `ssh <hostname> cat/ls/tail …` for mount files; SSH adds setup cost
    and shell-escaping bugs (nested quotes in `sed`/`awk` pipelines break).
  - **Long-running dev processes → `zerops_dev_server`.** Start/stop/
    restart/probe/tail all go through the MCP tool. Response carries
    `running`, `healthStatus`, `startMillis`, `reason`, `logTail` — full
    diagnosis without follow-up. Don't hand-roll `ssh <hostname> "cmd &"`
    — backgrounded SSH holds the channel until the 120 s bash timeout.
    See `develop-dynamic-runtime-start-container` for the canonical start
    recipe; `develop-dev-server-reason-codes` for `reason` triage.
  - **One-shot commands over SSH.** Framework CLIs, git ops,
    `curl localhost` exit quickly — no channel-lifetime concern:

    ```
    ssh <hostname> "cd /var/www && npm install"
    ssh <hostname> "cd /var/www && php artisan migrate"
    ssh <hostname> "curl -s http://localhost:{port}/api/health"
    ```

  - **Mount recovery.** If the SSHFS mount goes stale after a deploy
    (stat/ls returns empty, writes hang), remount: `zerops_mount action="mount"`.
  - **Agent Browser** — `agent-browser.dev` is on the ZCP host; use it to
    verify deployed web apps from the browser.
