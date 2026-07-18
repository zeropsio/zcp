## Status
Phase: develop-active
Services: weatherdash
  - weatherdash (go@1.22) — bootstrapped=true, mode=simple, strategy=push-dev, deployed=true
Guidance:
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

  Each `apiMeta[].metadata` key is a **field path** (e.g. `weatherdash.mode`,
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
  ### Dev-server state triage

  Before deploying, verifying, or iterating on a runtime service, run
  the triage rather than blind-starting a process.

  **Step 1 — Determine the expectation** from `runtimeClass` + `mode`
  in the envelope:

  | Envelope shape | Deployed runtime shape | Dev-server lifecycle |
  |---|---|---|
  | `runtimeClass: implicit-webserver` | Always live post-deploy | Platform-owned — no manual start |
  | `runtimeClass: dynamic`, `mode: dev` | `zsc noop` idle container | You start it via `zerops_dev_server action=start` |
  | `runtimeClass: dynamic`, `mode: simple\|stage` | Foreground binary with `healthCheck` | Platform auto-starts and probes |

  If the envelope reports implicit-webserver, static, or
  simple/stage-mode dynamic, triage ends — platform owns lifecycle.

  **Step 2 — Check current state** for dev-mode dynamic:

  ```
  # container env
  zerops_dev_server action=status hostname="weatherdash" port={port} healthPath="{path}"

  # local env — runs on your machine
  Bash command="curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:{port}{path}"
  ```

  Read the response:

  - `running: true` with HTTP 2xx/3xx/4xx `healthStatus` → proceed to
    `zerops_verify`.
  - `running: false` with `reason: health_probe_connection_refused` →
    start (step 3).
  - `running: true` with `healthStatus: 5xx` → server runs but is
    broken; read logs and response body; do NOT restart (it does not
    fix bugs); edit code then deploy.

  For workers with no HTTP surface (`port=0`, `healthPath=""`), skip
  HTTP status; call `zerops_logs` to confirm consumption.

  **Step 3 — Act on the delta.**

  ```
  # container env
  zerops_dev_server action=start hostname="weatherdash" command="{start-command}" port={port} healthPath="{path}"

  # local env
  Bash run_in_background=true command="{start-command}"
  ```

  After every redeploy the dev process is gone — re-run Step 2 before
  `zerops_verify`.
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
  ### HTTP diagnostics

  When the app returns 500 / 502 / empty body, follow this order. Stop at
  whichever step resolves the error — do **not** default to
  `ssh weatherdash curl localhost` for diagnosis.

  1. **`zerops_verify serviceHostname="weatherdash"`** — runs the canonical
     health probe (HEAD + `/health`/`/status`) and returns a structured
     diagnosis. This is always the first step.
  2. **Subdomain URL** — format is
     `https://weatherdash-${zeropsSubdomainHost}.prg1.zerops.app/` for static
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
     tool (e.g. `/var/www/weatherdash/storage/logs/laravel.log`,
     `var/log/...`). Do NOT `ssh weatherdash tail …` — the mount exposes the
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
  ### Push-Dev Deploy Strategy — container

  The dev container uses SSH push — `zerops_deploy` uploads the
  working tree from `/var/www/weatherdash/` straight into the service
  without a git remote. No credentials on your side: the tool SSHes
  using ZCP's container-internal key. The response's `mode` is `ssh`;
  `sourceService` and `targetService` identify the deploy class.

  - Self-deploy (single service): `zerops_deploy targetService="weatherdash"` — `sourceService == targetService`, class is self.
  - Cross-deploy (dev → stage): `zerops_deploy sourceService="weatherdash" targetService=""` — class is cross.

  `deployFiles` discipline differs per class: self-deploy needs `[.]`
  (narrower patterns destroy the target's source); cross-deploy
  cherry-picks build output. See `develop-deploy-modes` for the full
  rule and `develop-deploy-files-self-deploy` for the self-deploy
  invariant.
  ### Checklist (simple-mode services)

  - The entry in `zerops.yaml` must have a real `start:` command **and** a
    `healthCheck` — simple services auto-start and are probed on deploy.
  - There is no dev+stage pair; `weatherdash` is the single runtime container.
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
  ### Development workflow

  Edit code on `/var/www/weatherdash/`. After each set of changes deploy — the
  container auto-starts with its `healthCheck`, no manual server start:

  ```
  zerops_deploy targetService="weatherdash" setup="prod"
  zerops_verify serviceHostname="weatherdash"
  ```

  If only `zerops.yaml` changes are config-level (no code change), the deploy
  still applies them.
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
  ### `reason` values (DevServerResult)

  When `zerops_dev_server` actions fail, the response's `reason` field
  classifies the failure so you don't need a follow-up call to
  diagnose. Dispatch table:

  | `reason` | Meaning | Action |
  |---|---|---|
  | `spawn_timeout` | The remote shell did not detach; stdio handle still owned by child. | You likely hand-rolled `ssh ... "cmd &"` — re-run through `zerops_dev_server action=start`. |
  | `health_probe_connection_refused` | Spawn succeeded but nothing is listening on `port`. | Check that your app binds to `0.0.0.0` (not `localhost`), that `port` matches `run.ports[0].port`, and that your start command actually starts a server. Read `logTail` for crash output. |
  | `health_probe_http_<code>` | Server runs but returned `<code>` (e.g. 500, 404). | Do NOT restart — it does not fix bugs. Read `logTail` + response body, edit code, deploy. |
  | `post_spawn_exit` | No-probe-mode process died after spawn (port=0/healthPath=""). | `action=logs` for consumption errors; typical for worker crashes. |

  Observable always: `running` (bool), `healthStatus` (HTTP status
  when `port` set, 0 otherwise), `startMillis` (time from spawn to
  healthy), `logTail` (last log lines). Use these to confirm state
  without a second tool call.
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
  ### Development & Deploy

  Infrastructure is provisioned and at least one runtime already has a
  successful first deploy on record. You're in the edit loop: discover
  the current state, implement the user's request, redeploy, verify.
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
  ### Deploy strategy — current + how to change

  Each runtime service in the envelope has a `strategy` field:
  `push-dev` (SSH self-deploy from the dev container), `push-git`
  (push committed code to an external git remote — carries a
  `trigger: webhook|actions|unset` sub-field), `manual` (you
  orchestrate every deploy yourself), or `unset` (bootstrap-written
  placeholder; develop picks one on first use). The rendered Services
  block shows this as `strategy=push-dev|push-git|manual|unset`.

  Switch at any time without closing the session:

  ```
  zerops_workflow action="strategy" strategies={"weatherdash":"push-dev"}
  ```

  Mixed strategies across services in one project are fine — each
  service's strategy is independent in the envelope.
  ### Mode expansion — add a stage pair

  The envelope reports your current service with `mode: dev` or
  `mode: simple` (single-slot). Expanding to **standard** adds a stage
  sibling without touching the existing service. Expansion is an
  infrastructure change — it runs through the bootstrap workflow, not
  develop.

  ```
  zerops_workflow action="start" workflow="bootstrap"
    intent="expand weatherdash to standard — add stage"
  ```

  Submit a plan that flags the existing runtime and names the new
  stage hostname:

  ```json
  {
    "runtime": {
      "devHostname": "weatherdash",
      "type": "<same type as current service>",
      "isExisting": true,
      "bootstrapMode": "standard",
      "stageHostname": "<new-stage-hostname>"
    },
    "dependencies": [
      { "hostname": "<existing dep>", "type": "<dep type>", "resolution": "EXISTS" }
    ]
  }
  ```

  Bootstrap leaves the existing service's code and container untouched,
  creates the new stage service via `zerops_import`, and at close the
  envelope shows both snapshots:

  - the original (now `mode: standard` with `stageHostname` set,
    `bootstrapped: true`, `deployed: true`, strategy intact);
  - the new stage (`mode: stage`, `bootstrapped: true`,
    `deployed: false`).

  After close, run a dev→stage cross-deploy to verify the pair
  end-to-end.
  ### Closing the task

  Simple-mode services auto-start on deploy via their `healthCheck`:

  ```
  zerops_deploy targetService="weatherdash" setup="prod"
  zerops_verify serviceHostname="weatherdash"
  ```
