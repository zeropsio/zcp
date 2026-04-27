## Status
Phase: develop-active
Services: weatherdash
  - weatherdash (go@1.22) — bootstrapped=true, mode=simple, strategy=push-dev, deployed=true
Guidance:
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

  Each `apiMeta[].metadata` key is a **field path** (`weatherdash.mode`,
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
  ### Every code change must reach a durable state

  Iteration cadence is mode-specific:

  - Dev-mode dynamic runtime container: see
    `develop-push-dev-workflow-dev`.
  - Simple / standard / local / first-deploy: every change →
    `zerops_deploy`.

  Auto-close: see `develop-auto-close-semantics`.
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
  ### HTTP diagnostics

  For 500 / 502 / empty body, stop at the first useful signal; do **not**
  default to
  `ssh weatherdash curl localhost` for diagnosis.

  1. **`zerops_verify serviceHostname="weatherdash"`** — start with the
     canonical health probe and structured diagnosis; see
     `develop-verify-matrix` for the full verify path.
  2. **Subdomain URL** — static / implicit-webserver:
     `https://weatherdash-${zeropsSubdomainHost}.prg1.zerops.app/`; dynamic
     adds `-{port}`. `${zeropsSubdomainHost}` is numeric and project-scope,
     not the projectId. Read it with `env | grep zeropsSubdomainHost`, or
     use `zerops_discover` for the resolved URL. Do not guess a UUID.
  3. **`zerops_logs severity="error" since="5m"`** — recent platform errors
     (nginx, crash traces, deploy failures) without opening a shell.
  4. **Framework log file on the mount** — read via Read tool
     (`/var/www/weatherdash/storage/logs/laravel.log`, `var/log/...`). See
     `develop-platform-rules-container` for the mount-vs-SSH split.
  5. **Last resort: SSH + curl localhost** — only when earlier checks miss
     container-local state (worker-only service, non-default bind). Even
     then, `zerops_verify` usually already encodes the check.
  ### Platform rules

  - **Runtime user is `zerops`, not root.** Package installs need `sudo`
    (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
  - **Deploy = new container.** Local files in the current runtime container are
    lost; only content covered by `deployFiles` survives across redeploys.
  - **`zerops.yaml` lives at the repo root.** Each `setup:` block (e.g.
    `prod`, `stage`, `dev`) is deployed independently — these are canonical
    recipe names, NOT hostnames.
  - **Build ≠ runtime container.** Runtime packages → `run.prepareCommands`;
    build-only packages → `build.prepareCommands`. Build-time tools may
    not exist at run time; see guide `deployment-lifecycle`.
  - Env var live timing and cross-service syntax:
    `develop-env-var-channels` / `develop-first-deploy-env-vars`.
  - Service config changes (shared storage, scaling, nginx fragments):
    use `zerops_import` with `override: true` to update existing services.
    This is separate from `zerops_deploy`, which only updates code.
  ### Push-Dev Deploy Strategy

  The dev container uses SSH push — `zerops_deploy` uploads the
  working tree from `/var/www/weatherdash/` straight into the service
  without a git remote. No credentials on your side: `zerops_deploy` SSHes
  using ZCP's runtime container internal key. The response's `mode` is `ssh`;
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
  ### Development workflow

  Edit code on `/var/www/weatherdash/`. Redeploy for this mode (see
  `develop-change-drives-deploy`); the runtime container auto-starts with its `healthCheck`:

  ```
  zerops_deploy targetService="weatherdash" setup="prod"
  zerops_verify serviceHostname="weatherdash"
  ```

  Config-only changes still deploy; env-var live timing is in
  `develop-env-var-channels`.
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
  ### Per-service verify matrix

  Deploy success does not prove user behavior. Use `zerops_discover`:
  subdomain URL means web-facing; managed/no HTTP port means non-web.

  | Service shape | Required check |
  |---|---|
  | Non-web: managed DB/cache/worker/no subdomain | Run `zerops_verify serviceHostname="{targetHostname}"`. `status=healthy` is enough; nothing to browse. |
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
  ### Development & Deploy

  Infrastructure is provisioned and at least one runtime already has a
  successful first deploy on record. You're in the edit loop: discover
  the current state, implement the user's request, redeploy, verify.
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

  Bootstrap leaves the existing service's code and runtime container untouched,
  creates the new stage service via `zerops_import`, and at close the
  envelope shows both snapshots:

  - the original (now `mode: standard` with `stageHostname` set,
    `bootstrapped: true`, `deployed: true`, strategy intact);
  - the new stage (`mode: stage`, `bootstrapped: true`,
    `deployed: false`).

  After close, run a dev→stage cross-deploy to verify the pair
  end-to-end.
  ### Closing the task

  Simple-mode services auto-start on deploy; close through the cadence in
  `develop-change-drives-deploy`:

  ```
  zerops_deploy targetService="weatherdash" setup="prod"
  zerops_verify serviceHostname="weatherdash"
  ```
