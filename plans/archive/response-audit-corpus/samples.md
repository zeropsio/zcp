# All response-kinds — representative full payloads (recent median)

Ordered by total recent-byte impact. Each block = one real ZCP response the agent received.


---

## `workflow:develop:start::prose`
scenario=greenfield-website-from-brief | bytes=15599 | input={"action": "start", "workflow": "develop"}

```json
## Status
Phase: develop-active — intent: "Scaffold a one-page static website for a Czech sports club. Use designdotmd tokens (npx designdotmd add my-brand) for colors, typography, component rules. Czech-only content. Sections: hero, about club, team/schedule, contact (visual-only form, no backend). Write zerops.yaml with npm build pipeline, deploy to app service."
Services: app
  - app (ubuntu/nginx@1.22) — bootstrapped=true, mode=simple, closeMode=unset, gitPush=unconfigured, buildIntegration=none, deployed=false
→ Auto-close blocked: 0/1 ready, pending app. Next: zerops_deploy targetService="app" (un-deployed edits revert on a container cycle — deploy makes the change durable)
Guidance:
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
     Reading a sibling's exposed VALUE (managed-service creds, a sibling's
     own `run.envVariables`) is ALWAYS an explicit `${host_var}` ref — a
     sibling's bare var never appears on its own; relying on that breaks
     every isolated project (only `none` mode auto-shares siblings).
     Reaching another RUNTIME's HTTP endpoint is different: runtimes expose
     no URL env, so there is no `${api_url}`-style ref — use the internal-DNS
     literal `http://<hostname>:<port>` (e.g. `API_BASE_URL: http://api:3000`),
     http never https, over the project's private network.
  2. **Mode flag with a per-setup literal** — `NODE_ENV: development`
     in `setup: appdev`, `NODE_ENV: production` in `setup: appstage`.

  ### Self-shadow — never the same name on both sides

  ```yaml
  db_hostname: ${db_hostname}   # WRONG — destination == source
  APP_KEY: ${APP_KEY}           # WRONG — re-declaring a project env
  ```

  When destination == source the value resolves to the literal string `${db_hostname}` (not the resolved value), reaches `process.env` as that literal, and the app fails at connect/parse time.
  **Deploy modes — self-deploy vs cross-deploy** — pull on demand: `zerops_knowledge uri="zerops://atoms/develop-deploy-modes"`
  **Env var channels** — pull on demand: `zerops_knowledge uri="zerops://atoms/develop-env-var-channels"`
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
  returns the canonical page.

  **Reserved keys — never set these in `envVariables`:** `hostname`, `PATH`,
  `serviceId`, `projectId`, `appVersionId`, `appVersionName`, `zeropsSubdomain`
  are rejected at push (zcli names the offender). `HOSTNAME` / `Path` / `path`
  in `run.envVariables` crash runtime init — silent `BUILD_FAILED` in 4-5 s with
  empty logs (they're fine in `build.envVariables`). Rename (`APP_HOSTNAME`).
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
  `deployFiles` selects which build-container files land in the runtime:
  - **Self-deploy** (single service, `sourceService == targetService`): MUST be
    `[.]` — narrower patterns overwrite and destroy the target's own source.
  - **Cross-deploy** (dev → stage, `sourceService != targetService`): cherry-pick
    the build output — a dir path like `[./out]` keeps the dir, `[./out/~]` (tilde)
    extracts its contents to the deploy root.
  ### HTTP diagnostics

  For 500 / 502 / empty body, stop at the first useful signal; do **not**
  default to
  `ssh app curl localhost` for diagnosis.

  1. **`zerops_verify serviceHostname="app"`** — start with the
     canonical health probe and structured diagnosis (it picks the right
     check route per service shape).
  2. **Subdomain URL** — static / implicit-webserver:
     `https://app-${zeropsSubdomainHost}.prg1.zerops.app/`; dynamic
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
  **Platform rules** — pull on demand: `zerops_knowledge uri="zerops://atoms/develop-platform-rules-common"`
  **Reserved env-var keys** — pull on demand: `zerops_knowledge uri="zerops://atoms/develop-reserved-env-names"`
  ### Static runtime — develop workflow

  Static services have no runtime process to restart. The develop loop is:

  1. Edit files.
  2. Deploy with `zerops_deploy targetService="app"` — `zerops_deploy`
     picks the right mechanism for the active strategy.
  3. Verify the change via HTTP — open the project subdomain or fetch
     with curl. Do not tail `zerops_logs` for readiness; nginx is already
     serving the moment deploy lands.

  **There is no SSH start step.** Static services have no long-running
  process — nginx serves files as soon as the deploy lands.

  **Counter-intuitive build.base for static sites:** `build.base` MUST be
  a real builder runtime — Zerops rejects `static` / `nginx` as build
  bases (`unknown base`) even though both appear in the schema enum. Use
  `nodejs@22` as the convention even when there is no JS to build. The
  runtime nginx in `run.base` is unrelated to the build step.

  **Minimal `zerops.yaml` for plain HTML / no build step:**

  ```yaml
  zerops:
    - setup: <hostname>
      build:
        base: nodejs@22        # builder runtime — NOT nginx/static
        deployFiles: [.]
      run:
        base: nginx@1.22       # runtime — serves deployFiles via nginx
  ```

  `buildCommands` is OPTIONAL — omit it entirely; do not add a no-op
  `echo` defensively. `run.start`, `run.ports`, `run.envVariables`,
  `run.healthCheck` do not apply (nginx auto-serves on Zerops's
  managed port).

  **Build step** (Tailwind, bundler, SSG like Astro or Eleventy):
  runs in the Zerops build container at deploy time. Local builds are
  preview-only; Zerops rebuilds anyway.

  **Close-mode fit:** `manual` for low-traffic sites; `git-push` when the
  site has CI; `auto` for fast iteration.

  A deploy that lands files but serves 404 / 403 is a `deployFiles` path
  mistake, not a runtime failure.
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
  ### Work session auto-close

  Auto-close fires only when EVERY in-scope service carries `closeDeployMode ∈ {auto, git-push}` AND has a successful deploy + passing verify (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

  Scope follows session topology — standard pairs include both halves. For dev-only work pass `outOfScope=["<stage>"]` on develop start; the stage half drops to a non-blocking reminder and the session closes on the dev half alone.
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
  zerops_deploy targetService="app"
  ```
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
Next:
  ▸ Primary: Deploy app — zerops_deploy targetService="app"

```

---

## `workflow:complete:discover::session-active`
scenario=api-node-postgres-classic-dev | bytes=5078 | input={"action": "complete", "step": "discover"}

```json
{"kind":"session-active","sessionId":"33eacb86e0da54d2","intent":"Node.js REST API s PostgreSQL databází, dev-only prostředí pro iteraci na kódu, žádné stage/produkce","progress":{"total":3,"completed":1,"steps":[{"name":"discover","status":"complete"},{"name":"provision","status":"in_progress"},{"name":"close","status":"pending"}]},"current":{"name":"provision","index":1,"tools":["zerops_import","zerops_process","zerops_discover"],"verification":"SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND service types match plan AND managed dependency env vars recorded in session state. Runtime services are auto-mounted on completion.","detailedGuide":"### Provision recipe services\n\nProcedure is fixed; do NOT rewrite or reorder.\n\n1. **Project-level env vars (if any).**\n\nIf the YAML begins with a `project:` block containing `envVariables:`, set\nthem at project scope BEFORE `zerops_import`; the import tool rejects\nproject-level blocks.\n\n```\nzerops_env action=\"set\" scope=\"project\" key=\"APP_KEY\" value=\"\u003c@generateRandomString(\u003c32\u003e)\u003e\"\n```\n\nPreprocessor directives (`\u003c@...\u003e`) evaluate server-side; pass the literal\nstring, not a pre-rendered value. Repeat for each project env var.\n\nSome recipes carry framework-specific notes about a particular key —\ne.g. which prefix format the framework will or won't accept, or whether\na value must be regenerated post-deploy. Check the recipe's gotchas via\n`zerops_knowledge recipe=\"\u003cslug\u003e\"` BEFORE pre-setting project env vars;\nthe gotcha section names the key and the exact value shape that works\nfor the framework.\n\n2. **Import services.**\n\nStrip `project:`. Submit `services:` verbatim via `zerops_import` — ZCP\nalready applied plan hostnames and dropped EXISTS-resolved managed\nservices. Don't edit resource limits, `buildFromGit`, `priority`,\n`zeropsSetup`, or `type`.\n\n3. **Wait until every service reaches a running state.** Stage services in standard mode legitimately sit at `READY_TO_DEPLOY` until the first dev → stage cross-deploy; that's acceptable here. Poll:\n\n```\nzerops_discover\n```\n\nRuntimes must reach a running state (`RUNNING` or `ACTIVE`) before `deploy` — both states are acceptable. Managed deps usually transition first.\n\n4. **Record discovered env vars.**\n\nAfter services are running, include managed-service env var keys in the provision attestation (e.g. `db: connectionString, port`) for later `run.envVariables` references.\n\n---\n\n## Recipe — \"nodejs-hello-world\"\n\nProvision the recipe's services from the YAML below (already rewritten with any hostname/resolution choices from your plan):\n\n1. If the YAML has a `project:` block with `envVariables`, set those at the project level FIRST: `zerops_env action=\"set\" scope=\"project\" ...`.\n2. Call `zerops_import` with the `services:` section ONLY — the import tool rejects YAML that includes `project:`.\n3. Poll `zerops_discover` until every service reports `ACTIVE`. Recipes build from `buildFromGit`, so first provision can take 2–5 minutes while Zerops clones and builds.\n\n```yaml\n# AI agent environment provides a development space for AI\n# agents to build and version the app. Includes a dev service\n# with the code repository and development tools, a staging\n# service to validate builds, and a low-resource database.\nproject:\n    name: nodejs-hello-world-agent\nservices:\n    # Set up the AI agent development environment — Zerops pulls\n    # source and zerops.yaml from the 'buildFromGit' repo, using\n    # the 'dev' setup which installs Node.js 22 and all\n    # dependencies. SSH in and start developing.\n    # Subdomain access gives the dev workspace a public HTTPS URL\n    # so AI agents can verify endpoints during development.\n    - hostname: appdev\n      type: nodejs@22\n      zeropsSetup: dev\n      buildFromGit: https://github.com/zerops-recipe-apps/nodejs-hello-world-app\n      enableSubdomainAccess: true\n      verticalAutoscaling:\n        # Low allocation for an idle dev workspace — scale up\n        # via verticalAutoscaling settings when needed.\n        minRam: 0.5\n    # PostgreSQL single-node database shared by 'appdev' and\n    # 'appstage'. Priority 10 starts data services before app\n    # containers, preventing connection errors on first boot.\n    # NON_HA is appropriate for dev/staging where HA durability\n    # isn't required.\n    - hostname: db\n      type: postgresql@18\n      mode: NON_HA\n      priority: 10\n      verticalAutoscaling:\n        minRam: 0.25\n```\n","priorContext":{"plan":{"targets":[{"runtime":{"devHostname":"appdev","type":"nodejs@22","bootstrapMode":"dev","primarySetupName":"dev"},"dependencies":[{"hostname":"db","type":"postgresql@18","mode":"NON_HA","resolution":"CREATE"}]}],"createdAt":"2026-06-04T20:48:55Z"},"attestations":{"discover":"Planned targets: appdev (nodejs@22), db (postgresql@18, NON_HA)"}},"planMode":"dev"},"message":"Derived plan from recipe nodejs-hello-world: appdev.\n\nStep 2/3: provision"}
```

---

## `discover`
scenario=launch-production-pipeline-configured | bytes=1347 | input={}

```json
{"project":{"id":"2Biyb7d2TQeSum9HNtjLQQ","name":"zcp-eval","status":"ACTIVE"},"services":[{"hostname":"zcp","serviceId":"n3AAVraKQyG3bLBlpaqTUA","type":"zcp@1","status":"ACTIVE","adoptionState":"zcp-self","isInfrastructure":false,"subdomainEnabled":true},{"hostname":"db","serviceId":"ynkyM3HzRvaGUbZqsAXAow","type":"postgresql:single@18","status":"ACTIVE","mode":"NON_HA","adoptionState":"managed-dep","isInfrastructure":true},{"hostname":"appdev","serviceId":"GWVvaArLR3mRxZphPRtAmQ","type":"ubuntu/nodejs@22","status":"ACTIVE","adoptionState":"adoptable","isInfrastructure":false,"subdomainEnabled":true},{"hostname":"appstage","serviceId":"WcezT99sRcqPGiGVCpJvIg","type":"alpine/nodejs@22","status":"ACTIVE","adoptionState":"adoptable","isInfrastructure":false,"subdomainEnabled":true}],"warnings":["Services with adoptionState=\"adoptable\" (live but not tracked by ZCP): appdev, appstage. Run `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"` before MUTATING them: only `zerops_deploy` and the `develop` / `build-integration` workflows reject with ADOPT_REQUIRED until adoption completes. Read-only diagnostics work pre-adopt — for a reported URL/HTTP problem run `zerops_verify` FIRST (it carries the exact Recovery call), and `zerops_discover` / `zerops_events` / `zerops_logs` are all usable before adopting."]}
```

---

## `workflow:complete:provision::session-active`
scenario=landing-page-static-simple | bytes=3405 | input={"action": "complete", "step": "provision"}

```json
{"kind":"session-active","sessionId":"8e76b5e3fa24b7e4","intent":"Simple static HTML landing page served by nginx — single container, no database, no dependencies","progress":{"total":3,"completed":2,"steps":[{"name":"discover","status":"complete"},{"name":"provision","status":"complete"},{"name":"close","status":"in_progress"}]},"current":{"name":"close","index":2,"tools":["zerops_workflow"],"verification":"SUCCESS WHEN: bootstrap administratively closed (metas written, transition to develop presented).","detailedGuide":"### Verify infrastructure before closing bootstrap\n\nBootstrap is infra-only: no code, no deploy, no HTTP probe. Close must\nconfirm the **platform layer** is healthy before develop starts.\n\n```\nzerops_discover\n```\n\nRequired state for every planned service:\n\n- Platform `status` = `RUNNING` for managed services (databases, caches,\n  object storage). A managed service that never reached `RUNNING` means\n  the import failed silently — investigate `zerops_process` logs, do\n  not close.\n- Runtime services may appear as `NOT_YET_DEPLOYED` — that is expected.\n  Code and the first deploy happen in the develop workflow.\n- Env vars discovered during provisioning must be recorded in the\n  session so develop can wire them without re-discovering.\n\nDo **not** run `zerops_verify` here — that tool probes the app layer\n(HTTP reachability, `/status` endpoints) which only makes sense **after**\ndevelop writes code and runs the first deploy. Running it during\nbootstrap will report every runtime as failing and is noise.\n\nIf a managed service is stuck in a non-`RUNNING` state, bootstrap\nhard-stops: surface the failure to the user rather than retrying —\ninfrastructure issues require the user's judgment.\n\n---\n\n### Closing bootstrap\n\nBootstrap is **infrastructure-only**. After\n`action=\"complete\" step=\"close\"`, planned runtimes show\n`bootstrapped: true`: managed services are `RUNNING`, runtimes are\nregistered, dev containers are SSH-mount-ready, and managed env vars\nare discoverable. Classic and recipe-with-first-deploy-later services\nshow `deployed: false` and enter develop's first-deploy branch. Adopted\nservices and recipes that deployed during bootstrap show `deployed: true`.\n\nNo application code is written, no `zerops.yaml` generated, and no\ndeploy runs as part of bootstrap close itself.\n\n**Next step — `zerops_workflow action=\"start\" workflow=\"develop\"`.** Develop owns code, the first deploy, verify, iteration, and close-mode setup. Services with `deployed: false` enter the first-deploy branch on develop entry.\n\nDirect tools (`zerops_scale`, `zerops_env`, `zerops_subdomain`, `zerops_discover`) stay callable without a workflow wrapper for one-shot infra changes.\n\nComplete this step before starting develop.","priorContext":{"plan":{"targets":[{"runtime":{"devHostname":"app","type":"nginx@1.22","bootstrapMode":"simple"}}],"createdAt":"2026-06-04T21:33:56Z"},"attestations":{"discover":"[complete: Planned targets: app (nginx@1.22)]","provision":"Service app (nginx@1.22) provisioned and ACTIVE. No managed dependencies — no env vars to discover."}},"planMode":"simple"},"message":"Step 3/3: close","checkResult":{"passed":true,"checks":[{"name":"app_status","status":"pass"}],"summary":"all services provisioned"},"autoMounts":[{"hostname":"app","mountPath":"/var/www/app","status":"MOUNTED"}]}
```

---

## `knowledge`
scenario=classic-php-mariadb-standard | bytes=8451 | input={}

```json
## Service Stacks (live)
[B]=also usable as build.base in zerops.yaml

Runtime: bun@1.3.9 (latest) · 1.2.2 · 1.1.34 [B] | docker@26.1.5 | dotnet@10 (latest) · 9 · 8 [B] | elixir@1.16.3 (latest) · 1.16.2 [B] | gleam@1.5.1 [B] | go@1.22 [B] | java@21 (latest) · 17 [B] | nginx@1.22 | nodejs@24 (latest) · 22 · 20 [B] | php-apache@8.5 (latest) · 8.4 · 8.3 · 8.1 | php-nginx@8.5 (latest) · 8.4 · 8.3 | python@3.14 (latest) · 3.12 · 3.11 [B] | ruby@3.4 (latest) · 3.3 · 3.2 [B] | rust@stable · nightly [B] | static@1.0 | zero@0.1 [B] | alpine@3.23 (latest) · 3.22 · 3.21 · 3.20 [B] | deno@2.0.0 [B] | ubuntu@24.04 (latest) · 22.04 [B] | zcp@1
Managed: clickhouse@25.3 | elasticsearch@9.2 (latest) · 8.16 | kafka@3.9 | keydb@6 | mariadb@10.6 | meilisearch@1.44 (latest) · 1.20 · 1.10 | nats@2.12 (latest) · 2.10 | postgresql@18 (latest) · 17 · 16 · 14 | qdrant@1.12 (latest) · 1.10 | typesense@30.2 (latest) · 27.1 | valkey@7.2
Shared storage: shared-storage
Object storage: object-storage
Build-only: golang@1 | php@8.5 (latest) · 8.4 · 8.3 · 8.1


# PHP Hello world on Zerops

## 1. Adding `zerops.yaml`

The main application configuration file you place at the root of your repository.
It tells Zerops how to build, deploy, and run your application.

```yaml
zerops:
  # Production setup — optimized Composer install, minimal deploy footprint.
  - setup: prod
    build:
      base: php@8.5
      buildCommands:
        # Install production dependencies only; --no-dev excludes test tools,
        # --optimize-autoloader builds a classmap for faster class resolution.
        - composer install --no-dev --optimize-autoloader
      deployFiles:
        - ./index.php
        - ./migrate.php
        # vendor/ holds the Composer autoloader (and any packages you add).
        - ./vendor
      # Cache vendor/ between builds — Composer restores unchanged packages
      # from cache, skipping redundant network fetches on every deploy.
      cache:
        - vendor

    # Readiness check: new containers must answer HTTP 200 on port 80
    # before the project balancer routes traffic to them. This is what
    # enables zero-downtime deploys (temporaryShutdown: false by default).
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /

    run:
      base: php-apache@8.5
      # PHP-FPM starts via the php-apache base image default (foreground mode).
      # Apache runs alongside it as an OS-level service.
      # No 'start' needed here — the base image default handles it.
      # Run migration exactly once per deploy, regardless of container count.
      # initCommands run per container before traffic is accepted; zsc execOnce
      # ensures one container executes the migration and all others wait.
      # --retryUntilSuccessful handles brief DB startup delays on first deploy.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php migrate.php
      envVariables:
        # DB_NAME matches the PostgreSQL service hostname — a static value,
        # not a generated variable (Zerops names the database after hostname).
        DB_NAME: ${db_dbName}
        # The remaining vars reference generated credentials from the 'db'
        # service. Pattern: ${hostname_key} → e.g., ${db_hostname}, ${db_port}.
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

  # Dev setup — deploys full source for live development via SSH.
  # PHP is interpreted per-request: edit files in /var/www and changes
  # take effect immediately — no rebuild or container restart required.
  - setup: dev
    build:
      base: php@8.5
      buildCommands:
        # Install all dependencies including dev packages, so the developer
        # has testing and debugging tools available after SSH.
        - composer install
      deployFiles:
        # Deploy the entire working directory — source files, vendor/,
        # and zerops.yaml so 'zcli push' works from the dev container.
        - ./
      cache:
        - vendor

    run:
      base: php-apache@8.5
      initCommands:
        # Migration runs once per deploy — DB is ready when SSH session starts.
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php migrate.php
      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      # PHP-FPM is the Zerops-managed process for php-apache services —
      # omitting 'start' uses the base image default, which runs PHP-FPM
      # in foreground mode. Apache runs alongside it as an OS service.
      # SSH in and edit PHP files in /var/www; changes take effect on the
      # next request without any restart.
```



---

## Service Cards

### MariaDB

**Type**: `mariadb` (check live stacks for versions) | **Mode**: optional (default NON_HA), immutable
**Ports**: 3306 (fixed, no separate replica port)
**Env**: `hostname`, `port`, `projectId`, `serviceId`, `user`, `password`, `connectionString`, `dbName`
**HA**: MaxScale routing, read/write splitting, async replication, auto-failover
**Gotchas**: No separate replica port (MaxScale routes on single port). No internal TLS. Don't modify `zps` user. Min RAM 0.25 GB (platform default is 0.125 — don't set `verticalAutoscaling.minRam` below 0.25).
**Wiring** (sample hostname: `db`):
**VARS**: `DB_HOST: db` `DB_PORT: ${db_port}` `DB_NAME: ${db_dbName}`
**SECRETS**: `DATABASE_URL: mysql://${db_user}:${db_password}@db:${db_port}/${db_dbName}`

---

## Wiring Patterns

- **Hostname substitution**: In templates below, each service uses a sample hostname (e.g., `db`, `cache`, `search`). Replace it with your actual service hostname. The syntax `${hostname_varname}` is real Zerops cross-service reference syntax — `hostname` must match the target service hostname exactly. Service hostnames are lowercase alphanumeric only (`[a-z0-9]`, no dashes or underscores), so the hostname segment maps verbatim.
- **Reference**: `${hostname_variablename}` — the hostname segment is the literal service hostname (`[a-z0-9]`)
- **envSecrets** (import.yaml or GUI): injected directly as OS env vars — the app reads them via `getenv()` without any wiring. Do NOT re-reference envSecrets in zerops.yaml `run.envVariables` — `${MY_SECRET}` is NOT a valid reference (it becomes a literal string). The `${...}` syntax is ONLY for cross-service references. Changes to envSecrets require a service **restart** to take effect.
- **import.yaml service level**: ONLY `envSecrets` and `dotEnvSecrets` exist. There is NO `envVariables` at service level (only at project level). Use `envSecrets` only for generated secrets (`<@generateRandomString(...)>`) and real credentials.
- **Hostname = DNS**: use hostname directly for host (`db`, NOT `${db_hostname}`), but use `${db_port}` for port
- **Internal**: ALWAYS `http://` — NEVER `https://` (SSL at L7 balancer)
- **Project vars**: auto-inherited by all services — do NOT re-reference (creates shadow)
- **Password sync**: changing DB password in GUI does NOT update env vars (manual sync)

**Wire credentials in zerops.yaml `run.envVariables`** — Managed services auto-generate credentials but they are NOT automatically available to runtime services. Wire them via `run.envVariables` in zerops.yaml (the deploy-time config). Use import.yaml `envSecrets` ONLY for generated secrets like `<@generateRandomString(...)>`:

```yaml
# zerops.yaml — wire cross-service references here
zerops:
  - setup: myapp
    run:
      envVariables:
        DB_HOST: mydb
        DB_PORT: ${mydb_port}
        DB_NAME: ${mydb_dbName}
        DB_USER: ${mydb_user}
        DB_PASSWORD: ${mydb_password}
```
```yaml
# import.yaml — only generated secrets here
services:
  - hostname: mydb
    type: mariadb@{version}
    mode: NON_HA
    priority: 10

  - hostname: myapp
    type: nodejs@22
    envSecrets:
      APP_SECRET: <@generateRandomString(<32>)>
```

Without zerops.yaml wiring, the runtime service has no way to connect to managed services.

## Decision Hints

- **Choose Database**: **Use PostgreSQL** for everything unless you have a specific reason not to — the best-supported database on Zerops, with optional HA, read replicas, and pgBouncer. Default mode is **NON_HA** (single...

---

## Version Check

- ✓ `php-nginx@8.5`
- ✓ `mariadb@10.6`

```

---

## `workflow:bootstrap/classic:start::session-active`
scenario=classic-php-mariadb-standard | bytes=7693 | input={"action": "start", "workflow": "bootstrap", "route": "classic"}

```json
{"kind":"session-active","sessionId":"b572a3f7a4591caa","intent":"PHP web app with MariaDB database. Need both a development environment and a staging slot for testing builds.","progress":{"total":3,"completed":0,"steps":[{"name":"discover","status":"in_progress"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"discover","index":0,"tools":["zerops_discover","zerops_knowledge","zerops_workflow"],"verification":"SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).","detailedGuide":"Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.\n\nThree routes:\n\n- **Recipe** — services come from a matched recipe's import YAML.\n- **Classic** — agent constructs the import YAML from the user's intent.\n- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.\n\nRoute is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action=\"status\"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)\n\n---\n\n### Dynamic runtime plan\n\nIf the plan you're about to submit includes a dynamic runtime (Node, Go, Python, Bun, Ruby, …), apply this section. Classic bootstrap creates the runtime + managed services with `startWithoutCode: true` so dev containers reach RUNNING with an empty filesystem; `workflow=develop` then scaffolds `zerops.yaml`, writes the application, and runs the first deploy.\n\n`bootstrapMode` and `stageHostname` MUST be inside `runtime` — flat placement is hard-rejected. If you flatten by reflex, the error response includes the corrected JSON literal; paste-and-resend in one turn.\n\n```json\n[\n  {\n    \"runtime\": {\n      \"devHostname\": \"appdev\",\n      \"stageHostname\": \"appstage\",\n      \"type\": \"nodejs@22\",\n      \"bootstrapMode\": \"standard\",\n      \"isExisting\": false\n    },\n    \"dependencies\": [\n      {\"hostname\": \"db\", \"type\": \"postgresql@18\", \"resolution\": \"CREATE\"}\n    ]\n  }\n]\n```\n\nConfirm dev/stage pairing with the user before submitting the plan. Mode + close-mode + git-push capability decisions all happen later in develop, not here.\n\n---\n\n### Static runtime plan\n\nIf the plan you're about to submit includes a static-runtime container (`nginx`, `static`), apply this section. Static-runtime containers come up serving an empty document root after bootstrap. The first build artifact lands in develop via `zerops_deploy`; bootstrap creates the empty container and stops there.\n\nBefore submitting the plan, confirm with the user:\n\n- the chosen runtime hostname (`appdev` is the standard convention)\n- whether a stage pair is wanted (`standard` mode) or a single container (`simple` / `dev` mode)\n\nClose-mode, git-push capability, and the actual `zerops.yaml` (including `deployFiles` shape) are decided in develop after the first deploy lands — not here.\n\n---\n\n### Confirm mode per service\n\nEvery runtime service needs a **mode**; confirm with the user before\nsubmitting the plan.\n\n- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.\n  The app runs ONLY via `zerops_dev_server` (no supervised `run.start`),\n  so the public URL **502s after any container cycle** until restarted.\n  Pick dev for hands-on iteration with no durable end-state — never as the\n  final state of a service the user wants to stay reachable.\n- **standard** — dev + stage pair. The envelope reports `stageHostname`\n  on the dev snapshot and a separate snapshot with `mode: stage` for\n  the stage service.\n  - **Plan MUST set `stageHostname` explicitly on every standard target**\n    (e.g. `{\"runtime\": {\"devHostname\": \"appdev\", \"type\": \"...\", \"bootstrapMode\": \"standard\", \"stageHostname\": \"appstage\"}}`).\n    A submission omitting `stageHostname` rejects with an actionable\n    error pointing back to `bootstrapMode=\"dev\"` if a single container\n    was the actual intent.\n- **simple** — single always-on container: `run.start` runs the real app\n  on every deploy, platform-supervised so it survives container cycles.\n  Pick simple for a durable single service the user wants reachable at a\n  URL (web app, API, dashboard) and for background workers.\n- **stage** — never bootstrapped alone; it is the stage half of a\n  standard pair.\n\nChoose on the OUTCOME, not iteration habit: a service that should stay\nreachable → **simple** (or **standard** for a dev+stage split); a scratch\nspace for hands-on iteration with no durable end-state → **dev**. For a\n\"build me X\" request that ends at a URL, **simple** is the safe default —\ndev's transience is a footgun for anything left running. The plan commits\nthe mode when you submit it; the envelope then exposes it as\n`ServiceSnapshot.Mode`. Changing mode later requires a mode-expansion\nbootstrap session, surfaced in develop when actionable.\n\n---\n\n### Runtime classes\n\nEach runtime type falls into one of four classes — pick the right class for each runtime in the plan:\n\n- **Dynamic** (nodejs, go, python, bun, ruby, …) — needs an explicit dev-server lifecycle in develop (container: `zerops_dev_server`; local: harness background task).\n- **Static** (nginx, static) — serves files from `deployFiles`; platform auto-starts after deploy.\n- **Implicit-webserver** (php-apache, php-nginx) — webserver is part of the runtime; platform auto-starts after deploy.\n- **Managed** (postgresql, mariadb, redis/valkey, keydb, rabbitmq, nats, object storage) — no deploy; scale and connect only.\n\nPick runtime types from the live Zerops catalog (check `zerops_knowledge` for current versions). Managed services initialize first (`priority: 10` in import YAML) so runtimes that depend on them can connect at start.\n\nLifecycle and `zerops.yaml` mechanics for each class (start commands, healthCheck, deployFiles, dev-server primitives) are delivered by the develop response at first-deploy time."},"message":"Step 1/3: discover","availableStacks":"## Available Service Stacks (live, active concrete versions)\nPick a concrete version (newest marked `(latest)`). Family aliases (`go@1`) and rolling tags (`latest`/`canary`) are omitted — they resolve at import and won't match. Want another active version? Pass it; if it's not available ZCP lists the alternatives.\nRuntime: bun@1.3.9 (latest) · 1.2.2 · 1.1.34 | docker@26.1.5 | dotnet@10 (latest) · 9 · 8 | elixir@1.16.3 (latest) · 1.16.2 | gleam@1.5.1 | go@1.22 | java@21 (latest) · 17 | nginx@1.22 | nodejs@24 (latest) · 22 · 20 | php-apache@8.5 (latest) · 8.4 · 8.3 · 8.1 | php-nginx@8.5 (latest) · 8.4 · 8.3 | python@3.14 (latest) · 3.12 · 3.11 | ruby@3.4 (latest) · 3.3 · 3.2 | rust@stable · nightly | static@1.0 | zero@0.1 | alpine@3.23 (latest) · 3.22 · 3.21 · 3.20 | deno@2.0.0 | ubuntu@24.04 (latest) · 22.04 | zcp@1\nManaged: clickhouse@25.3 | elasticsearch@9.2 (latest) · 8.16 | kafka@3.9 | keydb@6 | mariadb@10.6 | meilisearch@1.44 (latest) · 1.20 · 1.10 | nats@2.12 (latest) · 2.10 | postgresql@18 (latest) · 17 · 16 · 14 | qdrant@1.12 (latest) · 1.10 | typesense@30.2 (latest) · 27.1 | valkey@7.2\nShared storage: shared-storage\nObject storage: object-storage\n"}
```

---

## `workflow:bootstrap:start::route-menu`
scenario=cadence-multiservice-spec-run2-replay | bytes=3951 | input={"action": "start", "workflow": "bootstrap"}

```json
{"kind":"route-menu","intent":"Project management web app: Next.js 15 (App Router) with PostgreSQL 16. Need auth (NextAuth/Auth.js), task CRUD with Kanban board, dashboard with project overview. Drizzle ORM for DB. Tailwind CSS + Radix UI for frontend. Start with core: database schema, auth, dashboard page, and task board with drag \u0026 drop.","projectId":"2Biyb7d2TQeSum9HNtjLQQ","routeOptions":[{"route":"recipe","why":"A minimal Next.js 15 application using standalone output mode, connecting to PostgreSQL for server-side rendering. Demonstrates the complete SSR stack on Zerops: standalone build, idempotent database migration with `zsc execOnce`, and a live health check that queries the database on every request.","recipeSlug":"nextjs-ssr-hello-world","fit":"exact","retrievalScore":0.95,"importYaml":"# Next.js SSR Hello World - Local Environment\n#\n# Provides a cloud PostgreSQL database for local development. The\n# developer runs the Next.js dev server on their own machine and\n# connects to the Zerops database via `zcli vpn up`.\nproject:\n  name: nextjs-ssr-hello-world-local\n\nservices:\n  # Staging service: validates production builds before they go to\n  # a shared stage or production. Deploy from your local machine with\n  # `zcli push` - no separate dev service needed here.\n  - hostname: app\n    type: nodejs@22\n    zeropsSetup: prod\n    # buildFromGit pulls source code and zerops.yaml from this public\n    # repository and triggers the first build automatically.\n    buildFromGit: https://github.com/zerops-recipe-apps/nextjs-ssr-hello-world-app\n    # enableSubdomainAccess gives a public HTTPS URL on a Zerops\n    # subdomain for sharing previews with teammates.\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n      minFreeRamGB: 0.25\n\n  # PostgreSQL for application data. Priority 10 ensures the database\n  # is ready before the app container runs its migration in\n  # initCommands.\n  - hostname: db\n    type: postgresql@18\n    # NON_HA = single-node database - cost-effective for local dev\n    # where the database runs in the cloud for VPN access.\n    mode: NON_HA\n    priority: 10\n"},{"route":"classic","why":"Manual plan — user describes services directly, no recipe template."},{"route":"recipe","why":"INCOMPLETE STACK: missing [postgresql] you mentioned. A minimal Next.js application deployed as a static export on Zerops — built with Node.js and served by Nginx, with build-time environment variable injection via `NEXT_PUBLIC_*` prefix. Used within Next.js Hello World recipe for Zerops platform.","recipeSlug":"nextjs-static-hello-world","fit":"incomplete","fitMissing":["postgresql"],"retrievalScore":0.95}],"message":"This is the route-menu phase (kind=\"route-menu\") — NO session is open yet. Pick one option and call zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"\u003croute\u003e\" again to commit the route and open the session (kind=\"session-active\"). Recipe requires `recipeSlug`; resume requires existing session via `action=\"resume\"`.\n\nOptions:\n  1. route=\"recipe\" recipeSlug=\"nextjs-ssr-hello-world\" fit=\"exact\" — A minimal Next.js 15 application using standalone output mode, connecting to PostgreSQL for server-side rendering. Demonstrates the complete SSR stack on Zerops: standalone build, idempotent database migration with `zsc execOnce`, and a live health check that queries the database on every request.\n  2. route=\"classic\" — Manual plan — user describes services directly, no recipe template.\n  3. route=\"recipe\" recipeSlug=\"nextjs-static-hello-world\" fit=\"incomplete\" — INCOMPLETE STACK: missing [postgresql] you mentioned. A minimal Next.js application deployed as a static export on Zerops — built with Node.js and served by Nginx, with build-time environment variable injection via `NEXT_PUBLIC_*` prefix. Used within Next.js Hello World recipe for Zerops platform.\n"}
```

---

## `workflow:bootstrap/adopt:start::session-active`
scenario=launch-with-existing-cicd | bytes=5263 | input={"action": "start", "workflow": "bootstrap", "route": "adopt"}

```json
{"kind":"session-active","sessionId":"b9a69c80b91a5012","intent":"Adopt existing appdev/appstage Node.js pair with db so we can proceed with launch-production.","progress":{"total":3,"completed":0,"steps":[{"name":"discover","status":"in_progress"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"discover","index":0,"tools":["zerops_discover","zerops_knowledge","zerops_workflow"],"verification":"SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).","detailedGuide":"Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.\n\nThree routes:\n\n- **Recipe** — services come from a matched recipe's import YAML.\n- **Classic** — agent constructs the import YAML from the user's intent.\n- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.\n\nRoute is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action=\"status\"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)\n\n---\n\n### Adopting existing services\n\nAdoption attaches ZCP tracking to an existing runtime service without touching its code, configuration, or scale. After adopt close, the envelope reports each adopted hostname with `bootstrapped: true` and an empty close-mode / git-push capability — populated later when the develop session needs them.\n\nIf you reached this atom by way of the `ADOPT_REQUIRED` rejection on a service-scoped tool, the right reflex was to fire adopt directly from the discover warning. The bootstrap-adopt session opens with a single committed call:\n\n```\nzerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\" intent=\"\u003cone-line user task summary\u003e\"\n```\n\nUse the SAME `intent` string you'd pass to `workflow=\"develop\"` afterwards — the intent threads through so you don't re-type it on the next workflow call. Placeholder strings (`\"\u003ctask\u003e\"`, `\"adopt existing\"`) lose user context and break the develop-session continuity heuristic; phrase it as the actual scope the user requested.\n\nList what's there:\n\n```\nzerops_discover\n```\n\nThen complete the discover step naming the services you want to adopt in `scope` (the\n`adoptionState=\"adoptable\"` hostnames from discover that THIS task needs):\n\n```\nzerops_workflow action=\"complete\" step=\"discover\" scope=[\"appdev\",\"appstage\"]\n```\n\nYou do not hand-write the nested adopt plan — `scope` is just the hostname list; the plan is derived for you (each named service becomes a tracked runtime target, `adoptionState=\"managed-dep\"` services attach as shared dependencies). Naming the services keeps adoption scoped to YOUR task: in a project with other live work (or another agent session), an empty scope is ambiguous, so it returns the adoptable candidate list for you to pick from rather than silently adopting everything. The control-plane (`zcp-self` / `zcp@*`), already-`adopted`, and `resumable` (mid-bootstrap, owned by a prior session → use `resume`) services are never adopt targets. Hostnames stay verbatim — never rename an adopted service.\n\nWhen exactly two adoptable runtimes share one runtime type (`ServiceStackTypeVersionName`) — the dev/stage shape — the response hands back two ready-to-paste plan templates rather than one default: a `standard` dev/stage pair (one container builds, the other receives the cross-deploy promote) and two independent dev containers. Pick the shape matching the user's intent and resubmit it as `plan=[...]`."},"message":"Step 1/3: discover","availableStacks":"## Available Service Stacks (live, active concrete versions)\nPick a concrete version (newest marked `(latest)`). Family aliases (`go@1`) and rolling tags (`latest`/`canary`) are omitted — they resolve at import and won't match. Want another active version? Pass it; if it's not available ZCP lists the alternatives.\nRuntime: bun@1.3.9 (latest) · 1.2.2 · 1.1.34 | docker@26.1.5 | dotnet@10 (latest) · 9 · 8 | elixir@1.16.3 (latest) · 1.16.2 | gleam@1.5.1 | go@1.22 | java@21 (latest) · 17 | nginx@1.22 | nodejs@24 (latest) · 22 · 20 | php-apache@8.5 (latest) · 8.4 · 8.3 · 8.1 | php-nginx@8.5 (latest) · 8.4 · 8.3 | python@3.14 (latest) · 3.12 · 3.11 | ruby@3.4 (latest) · 3.3 · 3.2 | rust@stable · nightly | static@1.0 | zero@0.1 | alpine@3.23 (latest) · 3.22 · 3.21 · 3.20 | deno@2.0.0 | ubuntu@24.04 (latest) · 22.04 | zcp@1\nManaged: clickhouse@25.3 | elasticsearch@9.2 (latest) · 8.16 | kafka@3.9 | keydb@6 | mariadb@10.6 | meilisearch@1.44 (latest) · 1.20 · 1.10 | nats@2.12 (latest) · 2.10 | postgresql@18 (latest) · 17 · 16 · 14 | qdrant@1.12 (latest) · 1.10 | typesense@30.2 (latest) · 27.1 | valkey@7.2\nShared storage: shared-storage\nObject storage: object-storage\n"}
```

---

## `workflow:bootstrap/recipe:start::session-active`
scenario=kanban-laravel-minimal-dev-only | bytes=7059 | input={"action": "start", "workflow": "bootstrap", "route": "recipe", "recipeSlug": "laravel-minimal"}

```json
{"kind":"session-active","sessionId":"628e46b1f0449f57","intent":"Deploy zerops-laravel-minimal recipe as dev-only (no stage, no production) into project waAzEFn6SBaysG4YE4rv7A. Kanban app, iterate on code.","progress":{"total":3,"completed":0,"steps":[{"name":"discover","status":"in_progress"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"discover","index":0,"tools":["zerops_discover","zerops_knowledge","zerops_workflow"],"verification":"SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).","detailedGuide":"Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.\n\nThree routes:\n\n- **Recipe** — services come from a matched recipe's import YAML.\n- **Classic** — agent constructs the import YAML from the user's intent.\n- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.\n\nRoute is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action=\"status\"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)\n\n---\n\n### The recipe owns the shape — confirm, don't author\n\nThe matched recipe's import YAML is the authoritative shape. ZCP builds the plan\nfrom it — you do NOT write the plan or pick the service `type` or mode. Accept\nthe recipe as-is by completing discover with NO plan:\n\n`zerops_workflow action=\"complete\" step=\"discover\"` — omit `plan`.\n\n### Submit a plan ONLY to adjust what the recipe leaves to you\n\n| You can change | How | Stays the recipe's (→ `route=\"classic\"` to alter) |\n|---|---|---|\n| A runtime hostname that collides with an existing service | submit that runtime with a non-colliding `devHostname` (and `stageHostname` for a pair) inside `runtime` | `type`, `zeropsSetup` / mode, `buildFromGit`, dev/stage pairing |\n| A managed dependency you already have | submit it with `resolution: \"EXISTS\"`, keeping the recipe's hostname | managed `hostname` — the repo's `${hostname_*}` refs break on rename |\n\nA partial plan is fine — list only the runtime(s) you rename; everything else\nfills in from the recipe. A managed service of a different type than the recipe\nexpects → `route=\"classic\"`.\n\nDo not write code — `buildFromGit` pulls the app repo at import. (Container only;\nin local mode the recipe repo is cloned into your working directory instead.)\n\n---\n\n## Recipe — \"laravel-minimal\"\n\nZCP derives the provisioning plan from this recipe — you do NOT author or mode-tag a plan. To accept the recipe as-is, complete the discover step with NO plan:\n\n→ `zerops_workflow action=\"complete\" step=\"discover\"` (omit `plan`)\n\nSubmit a `plan` ONLY to adjust what the recipe leaves to you:\n- rename a runtime hostname that collides with an existing service, or\n- mark a managed dependency you already have as `resolution: \"EXISTS\"`.\n\nManaged-service hostnames cannot be renamed (the app repo references them via `${hostname_*}`).\n\nIf the user EXPLICITLY asked for dev only (skip the paid staging service), narrow a standard recipe by adding `recipeNarrow=\"dev-only\"` to the same complete call — ZCP provisions the dev container + managed deps and skips the stage. Do NOT narrow by default.\n\nThe recipe's canonical import YAML, for reference:\n\n```yaml\n#zeropsPreprocessor=on\n\n# AI agent environment provides a development space for AI agents to build and\n# version the app.\n# It includes a dev service with the code repository and necessary development\n# tools, a staging service, and a low-resource database.\n\n# APP_KEY is Laravel's AES-256-CBC encryption key (32 random bytes).\n# Project-level so session cookies and encrypted database columns remain valid\n# when the L7 balancer routes requests to any app container in the project.\nproject:\n  name: laravel-minimal-agent\n  envVariables:\n    APP_KEY: \u003c@generateRandomString(\u003c32\u003e)\u003e\n\nservices:\n  # AI agent workspace — zeropsSetup:dev deploys the full source tree so the\n  # agent can SSH in and edit PHP files over SSHFS. PHP-FPM reinterprets each\n  # request, no restart needed. Subdomain gives the agent a URL to verify output\n  # against.\n  - hostname: appdev\n    type: php-nginx@8.4\n    zeropsSetup: dev\n    buildFromGit: https://github.com/zerops-recipe-apps/laravel-minimal-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # Staging slot for AI agents — zeropsSetup:prod validates the production\n  # build pipeline (composer install --no-dev, Vite asset compilation,\n  # config:cache + route:cache + view:cache in initCommands) before the agent\n  # marks the task complete.\n  - hostname: appstage\n    type: php-nginx@8.4\n    zeropsSetup: prod\n    buildFromGit: https://github.com/zerops-recipe-apps/laravel-minimal-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # PostgreSQL backing store for schema, sessions, cache, and queued jobs —\n  # all Laravel drivers default to 'database' in the minimal tier. Shared by\n  # appdev and appstage. NON_HA is fine for agent workspaces; priority 10\n  # ensures db is ready before the app containers start.\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n    mode: NON_HA\n    verticalAutoscaling:\n      minRam: 0.25\n\n```\n"},"message":"Step 1/3: discover","availableStacks":"## Available Service Stacks (live, active concrete versions)\nPick a concrete version (newest marked `(latest)`). Family aliases (`go@1`) and rolling tags (`latest`/`canary`) are omitted — they resolve at import and won't match. Want another active version? Pass it; if it's not available ZCP lists the alternatives.\nRuntime: bun@1.3.9 (latest) · 1.2.2 · 1.1.34 | docker@26.1.5 | dotnet@10 (latest) · 9 · 8 | elixir@1.16.3 (latest) · 1.16.2 | gleam@1.5.1 | go@1.22 | java@21 (latest) · 17 | nginx@1.22 | nodejs@24 (latest) · 22 · 20 | php-apache@8.5 (latest) · 8.4 · 8.3 · 8.1 | php-nginx@8.5 (latest) · 8.4 · 8.3 | python@3.14 (latest) · 3.12 · 3.11 | ruby@3.4 (latest) · 3.3 · 3.2 | rust@stable · nightly | static@1.0 | zero@0.1 | alpine@3.23 (latest) · 3.22 · 3.21 · 3.20 | deno@2.0.0 | ubuntu@24.04 (latest) · 22.04 | zcp@1\nManaged: clickhouse@25.3 | elasticsearch@9.2 (latest) · 8.16 | kafka@3.9 | keydb@6 | mariadb@10.6 | meilisearch@1.44 (latest) · 1.20 · 1.10 | nats@2.12 (latest) · 2.10 | postgresql@18 (latest) · 17 · 16 · 14 | qdrant@1.12 (latest) · 1.10 | typesense@30.2 (latest) · 27.1 | valkey@7.2\nShared storage: shared-storage\nObject storage: object-storage\n"}
```

---

## `workflow:launch-production:start::status=source-control-required`
scenario=launch-production-dev-only | bytes=6583 | input={"action": "start", "workflow": "launch-production"}

```json
{"workflow":"launch-production","status":"source-control-required","phase":"launch-production-active","guidance":"### Source-control prerequisites — resolve before launch advances\n\nLaunch refuses to advance past scope-prompt while any promoted runtime fails the source-control gate. The production project clones from `buildFromGit:`; that URL must point at a repo you own AND match the live origin in `/var/www`, NOT the recipe template the service was bootstrapped from.\n\n**Resolve blockers top-down — one re-call between each step.** The gate re-runs on every re-call and surfaces only the still-failing blockers.\n\n| Blocker ID | What it means | Recovery |\n|---|---|---|\n| `git-push-unconfigured-\u003chostname\u003e` | `meta.GitPushState != configured` — no probe-proven remote is wired for this service yet. Production cannot build from \"whatever happens to be in the bootstrapped git config\"; that's how recipe templates accidentally end up as the production source. | `zerops_workflow action=\"git-push-setup\" service=\"\u003chostname\u003e\" remoteUrl=\"\u003curl\u003e\" gitToken=\"\u003cPAT\u003e\"` (container) or `... remoteUrl=\"\u003curl\u003e\"` (local). The handler probes the remote BEFORE writing project state. Then re-call launch. |\n| `remote-mismatch-\u003chostname\u003e` | Live `git remote get-url origin` differs from the recorded `meta.RemoteURL`. Could be a manual rewrite, a recipe-template leftover, or drift since last setup. | Re-run `zerops_workflow action=\"git-push-setup\" service=\"\u003chostname\u003e\" remoteUrl=\"\u003ccorrected-URL\u003e\" gitToken=\"\u003cPAT\u003e\"` — the handler probes the new URL and syncs origin on success. Then re-call launch. |\n| `dev-tree-dirty-\u003chostname\u003e` | `git status --porcelain` on the dev push source is non-empty — uncommitted / staged / untracked changes. Those changes will NOT make it to production (Zerops clones the remote's HEAD; git push only pushes commits). The deploy tool refuses to push a dirty tree; the commit step is yours. | Commit the working tree first, then push: `ssh \u003chostname\u003e \"cd /var/www \u0026\u0026 git add -A \u0026\u0026 git commit -m '\u003cmsg\u003e'\"` (container) or `git -C \u003cworkingDir\u003e add -A \u0026\u0026 git -C \u003cworkingDir\u003e commit -m '\u003cmsg\u003e'` (local). Then `zerops_deploy targetService=\"\u003chostname\u003e\" strategy=\"git-push\"`. Then re-call launch. |\n| `head-not-pushed-\u003chostname\u003e` | Local HEAD on the push source does not match the remote HEAD (or remote HEAD unreachable). Local commits are ahead of the configured remote; production would build stale code. | `zerops_deploy targetService=\"\u003chostname\u003e\" strategy=\"git-push\"` pushes the existing commits. If HEAD is reachable on the remote but the SHAs differ, you have unpushed local commits — `git log --oneline origin/HEAD..HEAD` shows them. Then re-call launch. |\n| `build-integration-recommended-\u003chostname\u003e` (warn) | `meta.BuildIntegration=none` — stage has no auto-build pipeline. Recommended to set up before promoting so the source pair behaves like production will after launch. Optional — does not block. | Ask the user: configure now (recommended) or skip? On configure: `zerops_workflow action=\"build-integration\" service=\"\u003chostname\u003e\" integration=\"actions\"` (or `webhook` for GitLab / policy-constrained repos). On skip: re-call launch with `skipBuildIntegration=[\"\u003chostname\u003e\"]` to acknowledge the choice; subsequent calls will not re-surface the warn. |\n| `service-not-bootstrapped` | No `ServiceMeta` exists for the chosen `targetService`. Bootstrap never ran (or the meta got deleted). | `zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"` to adopt the existing services, then re-call launch. |\n\n**Multi-runtime promotion.** When `Promotables` lists more than one runtime, each runtime's blockers appear with its hostname suffix. Resolve them in the order the gate emits — one chained call per step, then re-call launch. The handler is stateless; passing the same accumulated inputs each turn is sufficient.\n\n**Trust boundary.** All chained actions above run with the standing project-scoped `ZCP_API_KEY`. The one-shot launch-window token (`launchKey`) is requested only at `ready-to-launch`, after every source-control blocker is cleared.\n\n**Prefer the orchestrated flow.** `git-push-setup` probes auth before writing project state; `zerops_deploy strategy=\"git-push\"` pushes already-committed code via the project-level `GIT_TOKEN`. Running `git push` directly from outside this flow bypasses the gate's source-of-truth checks (meta.RemoteURL vs live origin) — the next launch re-call may still surface `remote-mismatch` until `git-push-setup` re-syncs the meta.\n\nAfter every blocker clears, re-call:\n\n```\nzerops_workflow action=\"start\" workflow=\"launch-production\"\n  productionProjectName=\"\u003cfrom inputs\u003e\"\n  targetService=\"\u003cfrom inputs\u003e\"\n  envClassifications=\u003cfrom inputs\u003e\n  [skipBuildIntegration=[...]]  // only when user explicitly opted out\n```\n\nThe next response advances to `classify-prompt` (envs classification) and onward through the canonical state machine.","blockers":[{"id":"git-push-unconfigured-api","severity":"block","category":"source-control","message":"Cannot promote \"api\" to production — no user-owned git remote is wired. The production project will clone from buildFromGit; ZCP must point that at a repo you control, not the recipe template the source service was bootstrapped from. Run git-push-setup for service=\"api\" with your repo URL + token.","recovery":{"tool":"zerops_workflow","action":"git-push-setup","args":{"service":"api"}}},{"id":"build-integration-recommended-api","severity":"warn","category":"source-control","message":"Stage CI/CD for \"api\" is not configured (meta.BuildIntegration=none). Recommended to set up before promoting — every push to your remote will then auto-build the source pair, matching the production CI/CD model you will configure post-launch. Ask the user: configure now, or skip? On skip, re-call launch with skipBuildIntegration=[\"api\"].","recovery":{"tool":"zerops_workflow","action":"build-integration","args":{"service":"api"}}}],"inputs":{"productionProjectName":"api-prod","region":"eu-central"},"sourceContext":{"sourceProjectName":"zcp-eval","suggestedTargetName":"zcp-eval-prod","availableRuntimes":[{"hostname":"api","type":"ubuntu/nodejs@22","mode":"dev"}],"promotionHeadline":"api"}}
```

---

## `verify`
scenario=greenfield-fullstack-multi-runtime | bytes=1140 | input={}

```json
{"hostname":"apidev","type":"runtime","typeVersion":"alpine/nodejs@22","runtimeClass":"dynamic","runtimeClassification":"classified HTTP runtime from deployed setup \"apidev\" (has HTTP)","status":"degraded","checks":[{"name":"service_running","status":"pass"},{"name":"error_logs","status":"pass"},{"name":"http_root","status":"fail","detail":"HTTP 404: \u003c!DOCTYPE html\u003e\n\u003chtml lang=\"en\"\u003e\n\u003chead\u003e\n\u003cmeta charset=\"utf-8\"\u003e\n\u003ctitle\u003eError\u003c/title\u003e\n\u003c/head\u003e\n\u003cbody\u003e\n\u003cpre\u003eCannot GET /\u003c/pre\u003e\n\u003c/body\u003e\n\u003c/html\u003e\n (server reachable but root path not serving a 2xx/3xx — verify a real endpoint or accept as cosmetic)","httpStatus":404,"bodyText":"Cannot GET /"}],"workSessionState":{"status":"open","progress":{"sessionId":"work-1510939","ready":1,"total":4,"pending":["apistage","appdev","appstage"],"autoCloseStatus":"gated","reason":"auto-close gated by close-mode: apidev, apistage, appdev, appstage. Set close-mode via zerops_workflow action=\"close-mode\" closeMode={...}, or close explicitly via action=\"close\"."}}}
```

---

## `workflow:export:start::status=classify-prompt`
scenario=export-buildfromgit-self-snapshot | bytes=23768 | input={"action": "start", "workflow": "export"}

```json
{"envClassificationTable":[{"currentBucket":"","key":"GIT_TOKEN","rationale":"ZCP control-plane / platform re-emits on import","suggestedBucket":"infrastructure"},{"currentBucket":"","key":"APP_KEY","rationale":"key matches credentialPattern (_KEY|_SECRET|_TOKEN|_PASS|APP_KEY suffix); verify state continuity for migrate-into-existing-project path","suggestedBucket":"auto-secret"},{"currentBucket":"","key":"ZCP_API_KEY","rationale":"ZCP control-plane / platform re-emits on import","suggestedBucket":"infrastructure"}],"fetchValuesVia":"zerops_discover service=\"appdev\" includeEnvs=true includeEnvValues=true","guidance":"You are exporting a deployed runtime so a fresh Zerops project can reproduce the same infrastructure from a single git repo. The output is one repository at the chosen runtime's `/var/www` containing source code, `zerops.yaml` (build/run/deploy pipeline), and `zerops-project-import.yaml` (project + service definitions with `buildFromGit:` pointing back at the same repo). Re-import on a new project happens via `zcli project project-import zerops-project-import.yaml` or the dashboard.\n\nThe export workflow is a three-call narrowing — probe, generate, publish — and `zerops_workflow workflow=\"export\"` carries each call. Some companion atoms refer to these as **Phase A** (probe — scope/variant prompts), **Phase B** (generate — classify/validate), and **Phase C** (publish — bundle + push).\n\n## Pick the runtime\n\nIf the project has multiple runtime services, the first call returns a `scope-prompt` listing hostnames; pass `targetService=\u003chostname\u003e` on the next call. For a project with a single runtime, the first call can already include `targetService` and skip this step.\n\n## Pick the variant (pair modes only)\n\nFor `mode=standard` and `mode=local-stage` pairs, pick `variant=dev` (packages the dev hostname's tree + zerops.yaml) or `variant=stage` (packages the stage hostname's tree). Both bundle entries emit Zerops scaling `mode=NON_HA` — the destination project's topology Mode is established by ZCP's bootstrap at re-import, not embedded in the bundle.\n\nSingle-half source modes (`dev`, `simple`, `local-only`) skip this prompt — the variant is forced.\n\n## What the next calls do\n\n| Call | Inputs you add | Returns `status=` |\n|---|---|---|\n| 2 | `targetService` + `variant` (if pair) | `classify-prompt` |\n| 3 | + `envClassifications` map (key → bucket per env) | `publish-ready` (or `validation-failed`) |\n\nThe status-specific section of the response carries content + commands; this table is a call-shape map, not a content cheatsheet.\n\nIf `/var/www/zerops.yaml` is missing or git remote is unconfigured, the response carries a status that walks the prereq (zerops.yaml scaffold or `git-push-setup`) instead — complete the prereq, then re-call export.\n\n---\n\nYou are at `status=\"classify-prompt\"`. Classify each project env into one of four buckets — `infrastructure`, `auto-secret`, `external-secret`, `plain-config` — before re-calling with `envClassifications` populated.\n\nThe export bundle's `project.envVariables` block holds the values that re-imported services see at boot. Each project env needs a bucket so the generator knows whether to drop it (managed services regenerate the value), inject a preprocessor directive (auto-secret or external-secret placeholder), or emit it verbatim. Classification is your job — `zerops_workflow` does NOT auto-bucket.\n\n## The four buckets\n\n| Bucket | Detection signal | Emit in `zerops-project-import.yaml` |\n|---|---|---|\n| `infrastructure` | Value (or a component thereof) comes from a managed-service reference (`${db_*}`, `${redis_*}`, `${mongo_*}`, plus documented per-service prefixes). Includes app-built compound URLs assembled in code from `${...}` components. | DROP from `project.envVariables`. The reference still lives in `zerops.yaml`'s `run.envVariables`, and the re-imported managed service emits a fresh value. |\n| `auto-secret` | Source code or framework convention uses the var as a local encryption / signing key. Even when the encryption call lives inside the framework. | `\u003c@generateRandomString(\u003c32\u003e)\u003e`. Each re-import gets a fresh secret. |\n| `external-secret` | Source calls a third-party SDK using the var (Stripe, OpenAI, Mailgun, GitHub, …). Includes aliased imports and webhook verification secrets. | Comment + `\u003c@pickRandom([\"REPLACE_ME\"])\u003e`. The new project's owner pastes the real key into the dashboard before deploying. |\n| `plain-config` | Source uses the var as literal runtime config (LOG_LEVEL, NODE_ENV, FEATURE_FLAGS, …). | The literal value verbatim. |\n\n`zerops_workflow workflow=\"export\"` returns each unclassified env's key but NOT its value — fetch values via `zerops_discover service=\"{targetHostname}\" includeEnvs=true includeEnvValues=true`, grep them against the source tree, then call back with an `envClassifications` map (key → bucket per env).\n\nEvery row carries `suggestedBucket` + `rationale` computed server-side from the env key NAME alone (never the value, per the no-leak invariant). Treat the suggestion as a starting point — the four-bucket detection table above remains authoritative when you override (e.g. a credential-pattern name whose value is plain config, or a plain-named env whose value resolves to a `${db_*}` reference).\n\n## Worked examples per bucket\n\n### Infrastructure\n\n```\nDB_HOST=${db_hostname}\nREDIS_URL=${redis_connectionString}\n```\n\nBoth resolve from a managed-service reference. Bucket is `infrastructure` even though the source code reads them. The re-imported `db` and `redis` services emit fresh `${db_hostname}` / `${redis_connectionString}` values at boot.\n\nCompound case: `DATABASE_URL` is built in app code from `${DB_USER}`, `${DB_PASSWORD}`, etc. The COMPONENT envs are `infrastructure`. The composed `DATABASE_URL` may itself be a project env or may be assembled in app code at runtime. If `DATABASE_URL` is a project env that resolves to a managed reference, bucket it `infrastructure`. If it's a project env you assembled manually with literal credentials, bucket it `external-secret` (the value is sensitive, not auto-derived).\n\n### Auto-secret\n\n```\nAPP_KEY=existing-key    # Laravel — encrypts cookies/session\nSECRET_KEY=django…      # Django — signs sessions, CSRF, password tokens\nJWT_SECRET=long-bytes   # Node/Express — signs tokens\n```\n\nSource code rarely shows the encryption call directly — the framework owns it. Detect via framework convention: Laravel `APP_KEY`, Django `SECRET_KEY`, Rails `SECRET_KEY_BASE`, Express `SESSION_SECRET` / `JWT_SECRET`. **Stability warning**: if any persisted state (encrypted cookies, signed session tokens, password reset links, encrypted DB columns) depends on the existing key, regenerating breaks it. When in doubt, ask the user before bucketing as `auto-secret` — the alternative is `plain-config` (carry the existing key forward).\n\n### External secret\n\n```\nSTRIPE_SECRET=sk_live_xyz…\nOPENAI_API_KEY=sk-proj-…\nMAILGUN_API_KEY=key-…\nGITHUB_TOKEN=ghp_…    # also: GH_TOKEN, GH_PAT\n```\n\nSource code contains the SDK call (`stripe(env.STRIPE_SECRET)`, `OpenAI({apiKey: env.OPENAI_API_KEY})`, `Mailgun.client({key: env.MAILGUN_API_KEY})`). **Aliased imports** still count: `from stripe import Stripe as PaymentProvider; client = PaymentProvider(env.SECRET)` — the SDK is Stripe even if the local name isn't. **Webhook verification secrets** (`stripe.webhooks.constructEvent`) also bucket `external-secret`. **Empty / sentinel values** (`STRIPE_SECRET=`, `disabled`, `sk_test_*`, `pk_test_*`, `rk_test_*`, `test_xxx`, `none`, `null`, `false`, `off`, `n/a`, `noop`) are review-required — do NOT blindly substitute `REPLACE_ME` for them; bucket as `external-secret` only if the value is a real production secret. The generator surfaces a warning when it detects sentinel patterns. **Test-fixture values** like `TEST_API_KEY=test_xxx` (M6) used only by mocked tests usually want `plain-config` — verify by grepping whether the env is read at runtime; if every reference is inside a test file, drop or comment it out before publish unless source proves runtime dependency.\n\n### Plain config\n\n```\nLOG_LEVEL=info\nNODE_ENV=production\nFEATURE_FLAGS=experiments_v2,beta_signups\nAPP_URL=${zeropsSubdomainHost}\n```\n\nLiteral runtime config. **Privacy flag**: real emails (`MAIL_FROM_ADDRESS=ops@acme.com`), customer names, internal domain names, webhook URLs, and sender identities are technically `plain-config` but emitting them verbatim into a public export bundle leaks PII. Surface the value to the user before bucketing — they may want to redact or rotate before publishing.\n\n## Source-tree grep commands\n\nUse `rg -n` (ripgrep) for paste-safe alternation; `grep -RInE` is the equivalent fallback. Both expand `(a|b)` without backslash quoting.\n\n| Language | Find env read | Find SDK + encryption |\n|---|---|---|\n| Node | `rg -n 'process\\.env\\.\u003cKEY\u003e' src/` | `rg -nE '(stripe\\|openai\\|mailgun\\|@octokit)' src/`; `rg -nE '(jwt\\.sign\\|bcrypt\\|crypto\\.create)' src/` |\n| Python | `rg -n 'os\\.(environ\\|getenv)' .` | `rg -nE 'import (stripe\\|openai\\|mailgun)' .`; `rg -nE '(Fernet\\|signing\\.dumps\\|cryptography\\.fernet)' .` |\n| PHP | `rg -n \"env\\('\u003cKEY\u003e'\\)\" app/ config/` | `rg -nE 'Stripe\\\\\\|OpenAI\\\\\\|Mailgun\\\\' app/`; `rg -nE 'Crypt::\\|Hash::' app/` |\n| Go | `rg -n 'os\\.Getenv\\(\"\u003cKEY\u003e\"\\)' .` | `rg -nE '(crypto/\\|jwt\\.New)' .` |\n\nTrace one alias hop — wrapper modules that re-export an SDK still count. Beyond two hops, ask the user instead of guessing.\n\n## The per-env review table\n\nThe Phase B response (`status=\"classify-prompt\"`) carries a row per project env:\n\n```\n{ \"key\": \"APP_KEY\",    \"currentBucket\": \"\", \"suggestedBucket\": \"auto-secret\",  \"rationale\": \"key matches credentialPattern …\" },\n{ \"key\": \"DB_HOST\",    \"currentBucket\": \"\", \"suggestedBucket\": \"plain-config\", \"rationale\": \"no credential-pattern match …\" },\n{ \"key\": \"STRIPE_KEY\", \"currentBucket\": \"\", \"suggestedBucket\": \"auto-secret\",  \"rationale\": \"key matches credentialPattern …\" }\n```\n\nBuild your classification map from the keys, then call back with `envClassifications`:\n\n```\nzerops_workflow workflow=\"export\" \\\n  targetService=\"{targetHostname}\" \\\n  variant=\"dev\" \\\n  envClassifications={\"APP_KEY\":\"auto-secret\",\"DB_HOST\":\"infrastructure\",\"STRIPE_KEY\":\"external-secret\"}\n```\n\nIf you skip an env, the next response re-prompts with the remaining unclassified keys. Extra keys that don't match any project env are informational — the generator ignores them.\n\n## Common mis-classification traps\n\n- **APP_KEY across a stateful app** (M3): auto-generating breaks existing encrypted columns / session cookies. If state continuity matters, bucket `plain-config` and carry the existing value forward.\n- **`STRIPE_SECRET=` empty in staging** (M4): the live value is empty because staging doesn't process payments. `REPLACE_ME` placeholder breaks startup if the app validates the key on init. Bucket `external-secret` only if a real value is needed; otherwise `plain-config` keeps the empty string.\n- **Compound DATABASE_URL with literal credentials in source** (M2): the value LOOKS like infrastructure but it's a hand-rolled URL. Bucket `external-secret` so the new project owner replaces it after import.\n- **`MAIL_FROM_ADDRESS=ops@acme.com`** (M5): literal config, but the email is real. Flag privacy concern; consider replacing with a placeholder before export.\n- **`TEST_API_KEY=test_xxx` consumed only by tests** (M6): bucket `plain-config` only if the env is read at runtime; if every reference is inside a test file or a fixture loader, drop the env entirely from the bundle (delete the project env in dashboard before re-running export, or skip the row in `envClassifications` and let the unset warning prompt a follow-up).\n- **Non-default managed-service prefixes** (M7): a custom Mongo/Postgres/MySQL service may emit envs as `${mongo_connectionString}` / `${postgres_password}` / `${mysql_dbName}` instead of the documented `${db_*}` shape. The protocol still buckets these `infrastructure` if the live `zerops_discover` shows the value resolving to a managed-service env — verify by inspecting the discover response's `services[].envs` array, not just the `${db_*}` sample. False-negative `plain-config` here would emit a literal hostname/password into the bundle.\n\nIf a row's bucket is genuinely ambiguous, the safest default is `plain-config` (carries the existing value) plus a follow-up review with the user — wrong-direction errors there are fixable post-import without breaking deploy.\n\n---\n\nThis atom fires across both `classify-prompt` (where `bundle.warnings` is the actionable signal — composer hints to act on before the next call) AND `validation-failed` (where `bundle.errors` is the blocker — schema validation failed, the bundle cannot publish). At classify-prompt, `bundle.errors` is empty and you act on warnings; at validation-failed, `bundle.errors` is non-empty and you fix those first. Read every relevant field before re-calling — corrections are cheaper here than after publish.\n\n## What the response carries\n\n| Field | What it contains | Why it matters |\n|---|---|---|\n| `bundle.importYaml` | The `zerops-project-import.yaml` body. | Inspect the runtime entry's `buildFromGit:`, `zeropsSetup:`, `enableSubdomainAccess:`, and `project.envVariables`. The `services:` list also carries managed deps so `${db_*}`/`${redis_*}` resolve at re-import. |\n| `bundle.zeropsYaml` | The repo's live `zerops.yaml` body, verbatim. | Confirm the chosen `setup:` block matches the variant. The `run.envVariables` references must resolve against envs that survived classification. |\n| `bundle.warnings` | Per-env hints from the composer (visible at classify-prompt). | M4 empty externals, sentinel patterns, unset classifications, and M2 indirect references all surface here. Don't publish with an unresolved warning. |\n| `bundle.errors` | Blocking JSON-Schema failures (visible at validation-failed). | Each entry has `path` (JSON pointer) + `message`. Fix each error at its source. |\n| `bundle.repoUrl` | Live `git remote get-url origin` from the chosen runtime container. | If wrong (stale remote, accidental fork), fix via `git remote set-url origin \u003curl\u003e` on the runtime container — or re-run `git-push-setup` to refresh the cached `RemoteURL`. |\n\n## Schema validation errors (validation-failed status)\n\nWhen `bundle.errors` is non-empty the handler returns `status=\"validation-failed\"` instead of `publish-ready`. Each entry carries a `path` (JSON pointer to the failing field) and a `message` (validator output). Fix each error at its source — env classification, zerops.yaml, or service shape — and re-call. The embedded validators are `import-project-yml-json-schema.json` + `zerops-yml-json-schema.json` (Phase 5); schema drift between the embedded copy and live Zerops schema is possible. If `zcli project project-import` rejects a bundle that the client validator accepted, the embedded testdata needs a refresh.\n\n**Fixing live `/var/www/zerops.yaml` requires the develop workflow**, not export. Export is stateless — `zerops_mount` returns `WORKFLOW_REQUIRED` during export. To edit the runtime container's zerops.yaml: start `zerops_workflow workflow=\"develop\" scope=[\u003cruntime\u003e]`, mount the service via `zerops_mount`, edit the file, deploy, then re-call export with the same `targetService` + `envClassifications`. The export workflow re-reads zerops.yaml fresh on every invocation, so the fix flows through automatically.\n\n## Three classes of warning to act on (classify-prompt status)\n\n### M2 — indirect infrastructure reference\n\n```\nenv \"DB_HOST\": classified Infrastructure (drops from project.envVariables) but zerops.yaml's run.envVariables references ${DB_HOST} — re-import will fail to resolve. Reclassify as PlainConfig or rewrite zerops.yaml to use managed-service refs (${db_*}/${redis_*}) directly.\n```\n\n`zerops.yaml` references the project env's name (e.g. `${DB_HOST}`), not the managed-service env's name (`${db_hostname}`). Dropping `DB_HOST` from `project.envVariables` makes the reference unresolvable at re-import. Two fixes:\n\n1. **Reclassify as `plain-config`** — the value `${db_hostname}` stays in the bundle, Zerops applies it at boot, and the runtime sees `DB_HOST=${db_hostname}` which resolves to the managed db's hostname. Preserves the indirection.\n2. **Rewrite `zerops.yaml`** so `run.envVariables` references managed-service envs directly: `DB_HOST: ${db_hostname}`. This shortens the resolution chain at the cost of editing the live `zerops.yaml` (which is then bundled with the export).\n\nPick (1) for quick exports; pick (2) if the new project's owner shouldn't need to know about `DB_HOST` as a separate env.\n\n### M4 — empty / sentinel external secret\n\n```\nenv \"STRIPE_SECRET\": empty external secret — review before publish\nenv \"STRIPE_KEY\": external secret value \"sk_test_xyz\" matches a known sentinel/test pattern — verify classification (PlainConfig may be more appropriate)\n```\n\nYou classified the env `external-secret` but the value is empty or matches a known test/sentinel pattern (`sk_test_*`, `pk_test_*`, `rk_test_*`, `disabled`, `none`, `null`, `false`, `off`, `n/a`, `noop`). Re-import would substitute `\u003c@pickRandom([\"REPLACE_ME\"])\u003e` for an empty production-like key — likely wrong. Two fixes:\n\n1. **Reclassify as `plain-config`** — carry the empty / sentinel value verbatim. Re-imported services boot with the same disabled / staging shape.\n2. **Confirm the bucket and edit the bundle**: if a real key SHOULD be set, bucket `external-secret`, accept the `REPLACE_ME` placeholder, and add a \"set this env in dashboard before deploy\" step to the new project's runbook.\n\n### Unclassified env\n\n```\nenv \"MYSTERY_VAR\": not classified — emitted as plain-config; classify before publish\n```\n\nYou did not send a bucket for this env. The bundle defaults to `plain-config` (emits the value verbatim), which may leak secrets. Re-call with the missing entry classified.\n\n## Spot-check before re-call\n\nWhether you're acting on warnings (classify-prompt) or fixing errors (validation-failed), spot-check the rendered shape before re-calling:\n\n- `services[].mode` is `NON_HA` (single-runtime bundles; `HA` requires explicit scaling fields).\n- `services[].buildFromGit` resolves to a HTTPS or SSH-form remote URL.\n- `services[].zeropsSetup` matches a `setup:` name in the bundled `zerops.yaml`.\n- `project.envVariables` keys are not duplicated.\n- `#zeropsPreprocessor=on` header is line 1 if any value contains `\u003c@...\u003e`.","nextSteps":["Re-call: zerops_workflow workflow=\"export\" targetService=\"appdev\"","        envClassifications={key:bucket,...}"],"phase":"export-active","status":"classify-prompt","targetService":"appdev","warnings":["env \"GIT_TOKEN\": not classified — emitted as plain-config; classify before publish (plan §3.4)","env \"APP_KEY\": not classified — emitted as plain-config; classify before publish (plan §3.4)","env \"ZCP_API_KEY\": not classified — emitted as plain-config; classify before publish (plan §3.4)"],"zeropsYaml":"zerops:\n  # Production setup — compile TypeScript to JS, deploy\n  # compiled artifacts with production dependencies only.\n  - setup: prod\n    build:\n      base: nodejs@22\n\n      buildCommands:\n        # npm ci installs exact versions from package-lock.json\n        # for reproducible, auditable production builds.\n        - npm ci\n        - npm run build\n        # Strip dev-only packages (TypeScript, ts-node, type\n        # definitions) after compilation — runtime only needs\n        # production dependencies.\n        - npm prune --omit=dev\n\n      deployFiles:\n        - ./dist          # compiled JS (index.js + migrate.js)\n        - ./node_modules  # production dependencies only\n        - ./package.json\n\n      # Cache node_modules between builds to avoid re-downloading\n      # unchanged packages on every build trigger.\n      cache:\n        - node_modules\n\n    # Readiness check: verifies new containers respond at /\n    # before the project balancer routes traffic to them.\n    # Prevents requests reaching containers still starting up.\n    deploy:\n      readinessCheck:\n        httpGet:\n          port: 3000\n          path: /\n\n    run:\n      base: nodejs@22\n\n      # Run migration once per deploy across all containers.\n      # initCommands (not buildCommands) keeps migration and code\n      # deployment atomic — a failed deploy won't leave a migrated\n      # schema paired with old application code.\n      # --retryUntilSuccessful handles the brief window when the\n      # database port isn't yet accepting connections after import.\n      initCommands:\n        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js\n\n      ports:\n        - port: 3000\n          httpSupport: true\n\n      envVariables:\n        NODE_ENV: production\n        # Cross-service references — ${hostname_key} resolves to the\n        # value generated by the 'db' service at container start.\n        DB_NAME: ${db_dbName}\n        DB_HOST: ${db_hostname}\n        DB_PORT: ${db_port}\n        DB_USER: ${db_user}\n        DB_PASS: ${db_password}\n\n      start: node dist/index.js\n\n      # Health check restarts unresponsive containers after the\n      # retry window expires — keeps production alive when the\n      # process hangs or the database connection is lost.\n      healthCheck:\n        httpGet:\n          port: 3000\n          path: /\n\n      verticalAutoscaling:\n        # V8 GC needs headroom for traffic spikes — reserve ~50%\n        # of minRam as free RAM to prevent OOM restarts.\n        minRam: 0.25\n        minFreeRamGB: 0.125\n\n  # Development setup — deploy full source for interactive\n  # development via SSH. With no run.start the container stays\n  # idle on its own, so the developer controls what runs.\n  - setup: dev\n    build:\n      base: nodejs@22\n\n      buildCommands:\n        # npm install (not npm ci) — works without a lock file,\n        # giving flexibility during early development stages.\n        - npm install\n\n      # Deploy the entire working directory — source code,\n      # node_modules (with devDependencies), and config files.\n      deployFiles: ./\n\n      cache:\n        - node_modules\n\n    run:\n      base: nodejs@22\n      # Ubuntu provides richer tooling (apt, curl, git, vim)\n      # for interactive development via SSH.\n      os: ubuntu\n\n      # Migration runs on every container start — execOnce\n      # ensures it only executes once per deploy version even\n      # when multiple containers are running.\n      initCommands:\n        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/migrate.ts\n\n      ports:\n        - port: 3000\n          httpSupport: true\n\n      envVariables:\n        NODE_ENV: development\n        DB_NAME: ${db_dbName}\n        DB_HOST: ${db_hostname}\n        DB_PORT: ${db_port}\n        DB_USER: ${db_user}\n        DB_PASS: ${db_password}\n\n      # No run.start — the dynamic dev container stays up as a\n      # workspace on its own; SSH in and run the process by hand:\n      #   npm run dev   (ts-node hot-reload via nodemon)\n      # or\n      #   npm start     (plain ts-node)"}
```

---

## `browser`
scenario=kanban-laravel-minimal-dev-only | bytes=2154 | input={}

```json
{"url":"https://appdev-2445.prg1.zerops.app","steps":[{"command":["open","https://appdev-2445.prg1.zerops.app"],"success":true,"result":{"title":"Kanban","url":"https://appdev-2445.prg1.zerops.app/"}},{"command":["click","@e2"],"success":false,"result":null,"error":"Unknown ref: e2"},{"command":["snapshot","-i","-c"],"success":true,"result":{"origin":"https://appdev-2445.prg1.zerops.app/","refs":{"e1":{"name":"Kanban Board","role":"heading"},"e2":{"name":"+ New Task","role":"button"},"e3":{"name":"TODO","role":"heading"},"e4":{"name":"IN PROGRESS","role":"heading"},"e5":{"name":"REVIEW","role":"heading"},"e6":{"name":"DONE","role":"heading"}},"snapshot":"- heading \"Kanban Board\" [level=1, ref=e1]\n- button \"+ New Task\" [ref=e2]\n- heading \"TODO\" [level=2, ref=e3]\n- heading \"IN PROGRESS\" [level=2, ref=e4]\n- heading \"REVIEW\" [level=2, ref=e5]\n- heading \"DONE\" [level=2, ref=e6]"}},{"command":["fill","@e3","Design homepage layout"],"success":true,"result":{"filled":"@e3"}},{"command":["fill","@e4","Create wireframes and mockups for the main page"],"success":true,"result":{"filled":"@e4"}},{"command":["click","@e6"],"success":true,"result":{"clicked":"@e6"}},{"command":["wait","500"],"success":true,"result":{"ms":500,"waited":"timeout"}},{"command":["snapshot","-i","-c"],"success":true,"result":{"origin":"https://appdev-2445.prg1.zerops.app/","refs":{"e1":{"name":"Kanban Board","role":"heading"},"e2":{"name":"+ New Task","role":"button"},"e3":{"name":"TODO","role":"heading"},"e4":{"name":"IN PROGRESS","role":"heading"},"e5":{"name":"REVIEW","role":"heading"},"e6":{"name":"DONE","role":"heading"}},"snapshot":"- heading \"Kanban Board\" [level=1, ref=e1]\n- button \"+ New Task\" [ref=e2]\n- heading \"TODO\" [level=2, ref=e3]\n- heading \"IN PROGRESS\" [level=2, ref=e4]\n- heading \"REVIEW\" [level=2, ref=e5]\n- heading \"DONE\" [level=2, ref=e6]"}},{"command":["errors"],"success":true,"result":{"errors":[]}},{"command":["console"],"success":true,"result":{"messages":[]}},{"command":["close"],"success":true,"result":{"closed":true}}],"durationMs":2415,"message":"agent-browser exited with error: exit status 1"}
```

---

## `deploy`
scenario=export-buildfromgit-self-snapshot | bytes=781 | input={}

```json
{"status":"DEPLOYED","mode":"ssh","sourceService":"appdev","targetService":"appdev","targetServiceId":"Ebj9g6V7Ql2kqQmqx0Cjkw","targetServiceType":"ubuntu/nodejs@22","message":"Successfully deployed to appdev. New container replaced old — prior SSH sessions are gone.","buildStatus":"ACTIVE","buildDuration":"58s","sshReady":true,"nextActions":"Dev-mode dynamic runtime is idle (no start command). Start the dev server: zerops_dev_server action=start. Then run zerops_verify.","warnings":["run.start is empty — app will not start after deploy"],"subdomainAccessEnabled":true,"subdomainUrl":"https://appdev-2445-3000.prg1.zerops.app","workSessionState":{"status":"open","progress":{"sessionId":"work-1071381","ready":0,"total":2,"pending":["appdev","appstage"],"enabled":true}}}
```

---

## `workflow:?:status::prose`
scenario=launch-production-existing-with-webhook | bytes=7112 | input={"action": "status"}

```json
## Status
Phase: idle
Services: appdev, appstage, db
  - appdev (ubuntu/nodejs@22) — not bootstrapped
  - appstage (alpine/nodejs@22) — not bootstrapped
  - db (postgresql:single@18) — managed
Guidance:
  ### Bootstrap is two-phase

  First call returns `kind: "route-menu"` listing `routeOptions[]` — no
  session is open yet. Second call opens the session and returns
  `kind: "session-active"`. Read `kind` on every response.

  ```
  zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"
     → kind="route-menu", routeOptions=[...]
  zerops_workflow action="start" workflow="bootstrap" route="<picked>" \
     recipeSlug="<slug>"   # if route="recipe"
     sessionId="<id>"      # if route="resume"
     → kind="session-active"
  ```

  ### Coexistence with existing services

  Existing services in the project (bootstrapped or not) are **independent**.
  Bootstrap creates **new** services alongside them — it does not modify,
  re-import, or replace existing ones. To add a service to a project that
  already has some, pick a non-colliding hostname and bootstrap normally.
  `zerops_import override=true` is destructive and reserved for explicit
  user requests (config change on a known service), never the default
  path when bootstrap surfaces a hostname collision.

  ### Ranked options

  | Route | Present when | Carries | Dispatch / rule |
  |---|---|---|---|
  | `resume` | Snapshot has `resumable: true` | `resumeSession`, `resumeServices` | Pick first unless intentionally overriding: `route="resume" sessionId="<resumeSession>"`. |
  | `adopt` | Runtime services lack bootstrap records (`not bootstrapped`) | `adoptServices[]` | Attach ZCP tracking to running services — no infra change. Use when the user's intent matches the listed `adoptServices[]`. To add NEW services alongside (instead of adopting these), use `classic`. |
  | `recipe` | Up to three recipe matches | `recipeSlug`, `confidence`, `collisions[]` | `route="recipe" recipeSlug="<value from routeOptions[].recipeSlug>"`. Copy the slug verbatim from the discover response — corpus slugs don't carry a `zerops-` prefix even when users name a recipe by its branded form (`"zerops-laravel-minimal"`). Collisions recover by runtime rename or same-type managed `resolution: EXISTS`; switch routes only for different-type managed collision or independent infra. |
  | `classic` | Always available | none | `route="classic"` for manual planning. Default path for creating new services in any project state — fresh project or alongside existing ones. |

  ### Explicit overrides

  Explicit `route` on the first call bypasses discovery. Use only after
  prior discovery or direct user route choice. Valid values:
  `adopt`, `recipe`, `classic`, `resume`. Empty route re-enters discovery.

  ### Collision semantics

  `collisions[]` annotates recipe options; enforcement happens at plan
  submission. Pre-plan hostnames: rename runtimes or set managed deps to
  `EXISTS` before submitting.
  Per-service `adoptionState` in `zerops_discover` output classifies each
  service into one of five states: `adopted` (ZCP-tracked, ready for
  develop/deploy), `adoptable` (live runtime without ServiceMeta — call
  bootstrap route=adopt), `resumable` (mid-bootstrap, owned by prior
  session — call bootstrap route=resume with the session ID surfaced in
  the warning), `managed-dep` (db/cache/storage, no adoption concept),
  `zcp-self` (control-plane container, never adopted). The discover
  response also surfaces directive warnings naming exact recovery calls
  per state — read them before deriving anything from per-service flags.

  **FIRST CALL when discover surfaces adoptable services:** open the
  bootstrap-adopt session immediately, with the SAME intent string you'd
  pass to develop/deploy later. Do NOT probe with `workflow="develop"`
  expecting an ADOPT_REQUIRED redirect — the redirect works but costs a
  wasted round-trip + clutters the session log with a rejected start.

  Concrete shape (commits the route on the first call — skip the menu
  when adopt intent is already clear from discover output):

  ```
  zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="<one-line user task summary>"
  ```

  Replace `<one-line user task summary>` with the actual task intent
  (e.g. `intent="redesign appdev homepage as tech blog"`) — NOT a
  placeholder, NOT a generic "adopt existing". The intent threads
  through to the develop session that follows, so phrasing it as the
  real task scope avoids a re-typed intent on the next call.

  Service-scoped tools (`workflow="develop"`, `zerops_deploy`,
  `zerops_verify`) reject with `ADOPT_REQUIRED` until adoption completes.
  That gate is structural backstop, not the primary path — read the
  warning, fire adopt directly.

  Services in project, `not bootstrapped`. Two primary paths, both
  legitimate; existing services stay independent either way:

  1. **Adopt the listed services** — attach ZCP tracking to running
     services without changing their code, config, or scale.
  2. **Create new services alongside** — pick non-colliding hostnames
     and bootstrap normally; existing services keep running untouched.

  Re-importing or rewriting the existing services is **not** one of
  these paths — `zerops_import override=true` is destructive and only
  runs on explicit user request for a known service.

  **Branch on intent.** Clear adopt intent ("adopt", "převzít", named
  existing hostnames) → run adopt, no menu. Clear add-new intent
  ("add another", "new service") → use `classic`, no menu. Unclear →
  offer routes, not workflows.

  - ✅ "Adopt existing appdev/appstage, or create new alongside?"
  - ❌ "Develop on appdev, finish staging, or something else?" — those
    workflows aren't reachable yet; framing presents the unreachable as
    available.

  Start (route-menu, no session yet):

  ```
  zerops_workflow action="start" workflow="bootstrap" intent="adopt existing"
  ```

  Commit (opens session):

  ```
  zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="adopt existing"
  ```

  Type field in the plan carries the full identifier from
  `zerops_discover` verbatim — `alpine/nodejs@22`, `postgresql:single@18`.
  Legacy `os:` + `mode:` sibling fields still accepted for BC but the
  composite `type` is canonical; don't split. Pair-OS mismatch
  (ubuntu/alpine) accepted silently — dev half's type is what the
  plan carries.

  Close: each adopted hostname stamps `bootstrapped: true`, mode preserved.
  Close-mode + git-push stay empty (develop configures on first use).

  After adopt completes the runtime becomes a valid `launch-production`
  source — the adopted ServiceMeta carries the pair identity + setup
  cascade state, so `zerops_workflow workflow="launch-production"
  targetService=<adopted-hostname>` lands on the canonical dev-half
  without an extra normalization round-trip.
Next:
  ▸ Primary: Adopt unmanaged runtimes — zerops_workflow action="start" route="adopt" workflow="bootstrap"

```

---

## `workflow:complete:close::session-active`
scenario=greenfield-fullstack-multi-runtime | bytes=1136 | input={"action": "complete", "step": "close"}

```json
{"kind":"session-active","sessionId":"f2dc683b48cf7162","intent":"Fullstack app: Next.js frontend + Node.js API + Postgres. Dev and staging environments for both the frontend and the API.","progress":{"total":3,"completed":3,"steps":[{"name":"discover","status":"complete"},{"name":"provision","status":"complete"},{"name":"close","status":"complete"}]},"message":"Bootstrap complete.\n\n## Services\n\n- **appdev** (nodejs@22, standard mode)\n  Stage: **appstage**\n  - db (postgresql@18)\n- **apidev** (nodejs@22, standard mode)\n  Stage: **apistage**\n  - db (postgresql@18)\n\nInfrastructure is provisioned — runtimes are mounted and managed dependencies are running. No application code has been written yet, and nothing has been deployed. Dev-mode runtimes are idle (startWithoutCode); stage runtimes wait at READY_TO_DEPLOY for the first cross-deploy.\n\nNext: `zerops_workflow action=\"start\" workflow=\"develop\"` — develop owns scaffolding, code, first deploy, and verify. Platform invariants (deploy-replaces-container, SSHFS mount path, sudo, build/run split) surface via the develop-active atoms on the first call.\n"}
```

---

## `workflow:?:git-push-setup::status=walkthrough`
scenario=git-push-setup-with-cicd-method-prompt | bytes=7001 | input={"action": "git-push-setup"}

```json
{"guidance":"Runtime containers have no user credentials, so pushes to an external git remote run under `GIT_TOKEN`. Collect three inputs, then one tool call that **verifies the token works against the remote before writing any project state**, then commit + push.\n\n## Collect three inputs (use `AskUserQuestion` when the harness exposes it)\n\nThe `git-push-setup` walkthrough response carries `inputsRequired` for exactly these three; render them as a structured picker rather than free-text questions. The walkthrough also carries `recommendedIntegration` (default `actions` for GitHub remotes — zero manual Zerops dashboard step; `webhook` for GitLab and policy-constrained repos).\n\n1. **Git remote URL (HTTPS only)** — `https://github.com/{owner}/{repo}.git`. Container mode authenticates via `.netrc` + PAT, which requires HTTPS. SSH (`git@host:owner/repo`) remotes are rejected — use the HTTPS clone URL.\n2. **GIT_TOKEN** (fine-grained PAT, secret) — single-repo scope. **Default token shape** (covers the recommended Actions integration as well as the push-only minimum): GitHub fine-grained PAT scoped ONLY to `{owner}/{repo}` with `Contents: Read and write` + `Secrets: Read and write` + `Workflows: Read and write`. GitLab equivalent: `write_repository` + `api`. Single-repo blast radius — the container can only mutate this one repo. PATs require an expiration; pick the longest you're comfortable with (max 1 year).\n3. **Build integration** — pick one of `actions` (recommended for GitHub remotes; agent writes workflow YAML + `gh secret set` from the terminal), `webhook` (Zerops dashboard OAuth — one manual dashboard step; fallback for GitLab / policy-constrained repos), or `none` (external CI/CD you already own).\n\n| Host | Recommended token scope |\n|---|---|\n| GitHub fine-grained | `Contents: Read and write` + `Secrets: Read and write` + `Workflows: Read and write` (covers push + actions integration). |\n| GitLab personal access | `write_repository` + `api` (covers push + webhook integration). |\n\n## 1. Verify + configure git-push capability in one call\n\n```\nzerops_workflow action=\"git-push-setup\" service=\"appdev\" \\\n  remoteUrl=\"{repoUrl}\" \\\n  gitToken=\"{token}\"\n```\n\nProbe-first: the handler runs `git ls-remote` against the supplied URL using the supplied token (transient `.netrc`, trap-cleaned). **No project state is touched until the probe passes.** On success: token is written to project env as sensitive (never echoed back), `origin` is synced in the working tree's git config, the runtime is restarted so `$GIT_TOKEN` is live in shell, and `meta.GitPushState=configured` + `meta.RemoteURL` are stamped. On failure (`GIT_TOKEN_INVALID`): project state is left untouched — fix the token or URL and re-call.\n\n## 2. Commit + first push\n\n```\nssh appdev \"cd /var/www \u0026\u0026 git add -A \u0026\u0026 git commit -m 'initial commit'\"\n\nzerops_deploy targetService=\"appdev\" strategy=\"git-push\" \\\n  branch=\"main\"\n```\n\n`git init` already ran at bootstrap time (`InitServiceGit`); the commit step lives outside ZCP because `zerops_deploy strategy=\"git-push\"` refuses to push an empty working tree. The deploy call uses the project-level `GIT_TOKEN` and the stamped `origin` — no extra plumbing needed.\n\n**Push must go via `zerops_deploy strategy=\"git-push\"`** — not a plain `git push` from another shell. The probe-time `.netrc` (Phase 1 setup) was ephemeral inside the SSH chain; `$GIT_TOKEN` is now live in the runtime container's shell after the setup-time restart, but it is NOT visible to shells outside the container (the ZCP host, the SSHFS mount, a separate terminal). Running `git push` from any of those will fail with \"could not read Username\" because no `.netrc` exists there and no helper is configured. `zerops_deploy strategy=\"git-push\"` re-creates the ephemeral `.netrc` inside the runtime container for the duration of the push command — that is the only push path supported here.\n\nThe `remoteUrl` arg is optional on the deploy call (the stamped meta carries it). On failure, read `failureClassification.category` — `credential` means the token was rejected by the remote (re-call git-push-setup with a fresh PAT), `config` with a committed-code cause means there is no commit on HEAD yet.","inputsRequired":[{"description":"HTTPS URL of the target repository (https://github.com/\u003cowner\u003e/\u003crepo\u003e). Container mode authenticates via .netrc + PAT, which requires HTTPS. SSH form (git@github.com:owner/repo) is rejected — use the HTTPS clone URL.","label":"Git remote URL","name":"remoteUrl","required":true},{"description":"Personal access token scoped to the single target repo. For GitHub: Contents:Read+Write; add Secrets+Workflows if you plan integration=actions (recommended). For GitLab: write_repository; add api for webhook. The handler probes this token against the remote BEFORE writing it as sensitive project env — value is never echoed back.","label":"GIT_TOKEN (fine-grained PAT)","name":"gitToken","required":true,"secret":true},{"description":"Which CI shape consumes the remote push. Actions = GitHub Actions workflow runs zcli push (recommended for GitHub — zero manual dashboard steps); webhook = Zerops dashboard OAuth pulls the repo (requires manual dashboard step); none = independent CI/CD you already own.","label":"CI integration","name":"integration","options":["actions","webhook","none"],"required":true}],"nextStep":"After collecting inputs: 1) confirm capability with all three values: zerops_workflow action=\"git-push-setup\" service=\"appdev\" remoteUrl=\u003curl\u003e gitToken=\u003cPAT\u003e. Handler probes auth, writes GIT_TOKEN as sensitive project env, restarts push-source, stamps configured. 2) wire CI: zerops_workflow action=\"build-integration\" service=\"appdev\" integration=\"actions|webhook|none\".","prompt":"Three inputs needed to wire git-push for appdev: (1) HTTPS remote repo URL, (2) fine-grained PAT, (3) CI integration. The setup call probes the token against the remote BEFORE writing project state — failed probe leaves project state untouched. Actions is the default for GitHub repos.","recommendedIntegration":"actions","service":"appdev","status":"walkthrough","steps":[{"n":1,"title":"Collect inputs from user","call":"\u003cno MCP call\u003e — gather remoteUrl + gitToken (fine-grained PAT) + integration choice from the user"},{"n":2,"title":"Probe-confirm + write capability","call":"zerops_workflow action=\"git-push-setup\" service=\"appdev\" remoteUrl=\u003curl\u003e gitToken=\u003cPAT\u003e"},{"n":3,"title":"Wire CI integration","call":"zerops_workflow action=\"build-integration\" service=\"appdev\" integration=\"actions|webhook|none\""}],"workSessionState":{"status":"none","note":"No active develop session — deploy not tracked. Start one via zerops_workflow action=\"start\" workflow=\"develop\" intent=\"...\" scope=[...] to pick up auto-close + verify tracking."}}
```

---

## `workflow:export:start::status=validation-failed`
scenario=export-buildfromgit-self-snapshot | bytes=13902 | input={"action": "start", "workflow": "export"}

```json
{"errors":[{"message":"additionalProperties 'verticalAutoscaling' not allowed","path":"/zerops/0/run"}],"guidance":"You are exporting a deployed runtime so a fresh Zerops project can reproduce the same infrastructure from a single git repo. The output is one repository at the chosen runtime's `/var/www` containing source code, `zerops.yaml` (build/run/deploy pipeline), and `zerops-project-import.yaml` (project + service definitions with `buildFromGit:` pointing back at the same repo). Re-import on a new project happens via `zcli project project-import zerops-project-import.yaml` or the dashboard.\n\nThe export workflow is a three-call narrowing — probe, generate, publish — and `zerops_workflow workflow=\"export\"` carries each call. Some companion atoms refer to these as **Phase A** (probe — scope/variant prompts), **Phase B** (generate — classify/validate), and **Phase C** (publish — bundle + push).\n\n## Pick the runtime\n\nIf the project has multiple runtime services, the first call returns a `scope-prompt` listing hostnames; pass `targetService=\u003chostname\u003e` on the next call. For a project with a single runtime, the first call can already include `targetService` and skip this step.\n\n## Pick the variant (pair modes only)\n\nFor `mode=standard` and `mode=local-stage` pairs, pick `variant=dev` (packages the dev hostname's tree + zerops.yaml) or `variant=stage` (packages the stage hostname's tree). Both bundle entries emit Zerops scaling `mode=NON_HA` — the destination project's topology Mode is established by ZCP's bootstrap at re-import, not embedded in the bundle.\n\nSingle-half source modes (`dev`, `simple`, `local-only`) skip this prompt — the variant is forced.\n\n## What the next calls do\n\n| Call | Inputs you add | Returns `status=` |\n|---|---|---|\n| 2 | `targetService` + `variant` (if pair) | `classify-prompt` |\n| 3 | + `envClassifications` map (key → bucket per env) | `publish-ready` (or `validation-failed`) |\n\nThe status-specific section of the response carries content + commands; this table is a call-shape map, not a content cheatsheet.\n\nIf `/var/www/zerops.yaml` is missing or git remote is unconfigured, the response carries a status that walks the prereq (zerops.yaml scaffold or `git-push-setup`) instead — complete the prereq, then re-call export.\n\n---\n\nThis atom fires across both `classify-prompt` (where `bundle.warnings` is the actionable signal — composer hints to act on before the next call) AND `validation-failed` (where `bundle.errors` is the blocker — schema validation failed, the bundle cannot publish). At classify-prompt, `bundle.errors` is empty and you act on warnings; at validation-failed, `bundle.errors` is non-empty and you fix those first. Read every relevant field before re-calling — corrections are cheaper here than after publish.\n\n## What the response carries\n\n| Field | What it contains | Why it matters |\n|---|---|---|\n| `bundle.importYaml` | The `zerops-project-import.yaml` body. | Inspect the runtime entry's `buildFromGit:`, `zeropsSetup:`, `enableSubdomainAccess:`, and `project.envVariables`. The `services:` list also carries managed deps so `${db_*}`/`${redis_*}` resolve at re-import. |\n| `bundle.zeropsYaml` | The repo's live `zerops.yaml` body, verbatim. | Confirm the chosen `setup:` block matches the variant. The `run.envVariables` references must resolve against envs that survived classification. |\n| `bundle.warnings` | Per-env hints from the composer (visible at classify-prompt). | M4 empty externals, sentinel patterns, unset classifications, and M2 indirect references all surface here. Don't publish with an unresolved warning. |\n| `bundle.errors` | Blocking JSON-Schema failures (visible at validation-failed). | Each entry has `path` (JSON pointer) + `message`. Fix each error at its source. |\n| `bundle.repoUrl` | Live `git remote get-url origin` from the chosen runtime container. | If wrong (stale remote, accidental fork), fix via `git remote set-url origin \u003curl\u003e` on the runtime container — or re-run `git-push-setup` to refresh the cached `RemoteURL`. |\n\n## Schema validation errors (validation-failed status)\n\nWhen `bundle.errors` is non-empty the handler returns `status=\"validation-failed\"` instead of `publish-ready`. Each entry carries a `path` (JSON pointer to the failing field) and a `message` (validator output). Fix each error at its source — env classification, zerops.yaml, or service shape — and re-call. The embedded validators are `import-project-yml-json-schema.json` + `zerops-yml-json-schema.json` (Phase 5); schema drift between the embedded copy and live Zerops schema is possible. If `zcli project project-import` rejects a bundle that the client validator accepted, the embedded testdata needs a refresh.\n\n**Fixing live `/var/www/zerops.yaml` requires the develop workflow**, not export. Export is stateless — `zerops_mount` returns `WORKFLOW_REQUIRED` during export. To edit the runtime container's zerops.yaml: start `zerops_workflow workflow=\"develop\" scope=[\u003cruntime\u003e]`, mount the service via `zerops_mount`, edit the file, deploy, then re-call export with the same `targetService` + `envClassifications`. The export workflow re-reads zerops.yaml fresh on every invocation, so the fix flows through automatically.\n\n## Three classes of warning to act on (classify-prompt status)\n\n### M2 — indirect infrastructure reference\n\n```\nenv \"DB_HOST\": classified Infrastructure (drops from project.envVariables) but zerops.yaml's run.envVariables references ${DB_HOST} — re-import will fail to resolve. Reclassify as PlainConfig or rewrite zerops.yaml to use managed-service refs (${db_*}/${redis_*}) directly.\n```\n\n`zerops.yaml` references the project env's name (e.g. `${DB_HOST}`), not the managed-service env's name (`${db_hostname}`). Dropping `DB_HOST` from `project.envVariables` makes the reference unresolvable at re-import. Two fixes:\n\n1. **Reclassify as `plain-config`** — the value `${db_hostname}` stays in the bundle, Zerops applies it at boot, and the runtime sees `DB_HOST=${db_hostname}` which resolves to the managed db's hostname. Preserves the indirection.\n2. **Rewrite `zerops.yaml`** so `run.envVariables` references managed-service envs directly: `DB_HOST: ${db_hostname}`. This shortens the resolution chain at the cost of editing the live `zerops.yaml` (which is then bundled with the export).\n\nPick (1) for quick exports; pick (2) if the new project's owner shouldn't need to know about `DB_HOST` as a separate env.\n\n### M4 — empty / sentinel external secret\n\n```\nenv \"STRIPE_SECRET\": empty external secret — review before publish\nenv \"STRIPE_KEY\": external secret value \"sk_test_xyz\" matches a known sentinel/test pattern — verify classification (PlainConfig may be more appropriate)\n```\n\nYou classified the env `external-secret` but the value is empty or matches a known test/sentinel pattern (`sk_test_*`, `pk_test_*`, `rk_test_*`, `disabled`, `none`, `null`, `false`, `off`, `n/a`, `noop`). Re-import would substitute `\u003c@pickRandom([\"REPLACE_ME\"])\u003e` for an empty production-like key — likely wrong. Two fixes:\n\n1. **Reclassify as `plain-config`** — carry the empty / sentinel value verbatim. Re-imported services boot with the same disabled / staging shape.\n2. **Confirm the bucket and edit the bundle**: if a real key SHOULD be set, bucket `external-secret`, accept the `REPLACE_ME` placeholder, and add a \"set this env in dashboard before deploy\" step to the new project's runbook.\n\n### Unclassified env\n\n```\nenv \"MYSTERY_VAR\": not classified — emitted as plain-config; classify before publish\n```\n\nYou did not send a bucket for this env. The bundle defaults to `plain-config` (emits the value verbatim), which may leak secrets. Re-call with the missing entry classified.\n\n## Spot-check before re-call\n\nWhether you're acting on warnings (classify-prompt) or fixing errors (validation-failed), spot-check the rendered shape before re-calling:\n\n- `services[].mode` is `NON_HA` (single-runtime bundles; `HA` requires explicit scaling fields).\n- `services[].buildFromGit` resolves to a HTTPS or SSH-form remote URL.\n- `services[].zeropsSetup` matches a `setup:` name in the bundled `zerops.yaml`.\n- `project.envVariables` keys are not duplicated.\n- `#zeropsPreprocessor=on` header is line 1 if any value contains `\u003c@...\u003e`.","nextSteps":["Fix each validation error at its source.","Re-call: zerops_workflow workflow=\"export\" targetService=\"appdev\"","        envClassifications=\u003cyour same map\u003e"],"phase":"export-active","preview":{"errors":[{"message":"additionalProperties 'verticalAutoscaling' not allowed","path":"/zerops/0/run"}],"importYaml":"#zeropsPreprocessor=on\nproject:\n    envVariables:\n        APP_KEY: \u003c@generateRandomString(\u003c32\u003e)\u003e\n    name: zcp-eval\nservices:\n    - buildFromGit: https://github.com/zerops-recipe-apps/nodejs-hello-world-app\n      enableSubdomainAccess: true\n      hostname: appdev\n      maxContainers: 3\n      minContainers: 1\n      mode: NON_HA\n      type: ubuntu/nodejs@22\n      verticalAutoscaling:\n        cpuMode: SHARED\n        maxCpu: 3\n        maxDisk: 100\n        maxRam: 8\n        minCpu: 1\n        minDisk: 1\n        minRam: 0.5\n      zeropsSetup: dev\n    - hostname: db\n      mode: NON_HA\n      priority: 10\n      type: postgresql:single@18\n","repoUrl":"https://github.com/zerops-recipe-apps/nodejs-hello-world-app","setupName":"dev","warnings":null,"zeropsYaml":"zerops:\n  # Production setup — compile TypeScript to JS, deploy\n  # compiled artifacts with production dependencies only.\n  - setup: prod\n    build:\n      base: nodejs@22\n\n      buildCommands:\n        # npm ci installs exact versions from package-lock.json\n        # for reproducible, auditable production builds.\n        - npm ci\n        - npm run build\n        # Strip dev-only packages (TypeScript, ts-node, type\n        # definitions) after compilation — runtime only needs\n        # production dependencies.\n        - npm prune --omit=dev\n\n      deployFiles:\n        - ./dist          # compiled JS (index.js + migrate.js)\n        - ./node_modules  # production dependencies only\n        - ./package.json\n\n      # Cache node_modules between builds to avoid re-downloading\n      # unchanged packages on every build trigger.\n      cache:\n        - node_modules\n\n    # Readiness check: verifies new containers respond at /\n    # before the project balancer routes traffic to them.\n    # Prevents requests reaching containers still starting up.\n    deploy:\n      readinessCheck:\n        httpGet:\n          port: 3000\n          path: /\n\n    run:\n      base: nodejs@22\n\n      # Run migration once per deploy across all containers.\n      # initCommands (not buildCommands) keeps migration and code\n      # deployment atomic — a failed deploy won't leave a migrated\n      # schema paired with old application code.\n      # --retryUntilSuccessful handles the brief window when the\n      # database port isn't yet accepting connections after import.\n      initCommands:\n        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js\n\n      ports:\n        - port: 3000\n          httpSupport: true\n\n      envVariables:\n        NODE_ENV: production\n        # Cross-service references — ${hostname_key} resolves to the\n        # value generated by the 'db' service at container start.\n        DB_NAME: ${db_dbName}\n        DB_HOST: ${db_hostname}\n        DB_PORT: ${db_port}\n        DB_USER: ${db_user}\n        DB_PASS: ${db_password}\n\n      start: node dist/index.js\n\n      # Health check restarts unresponsive containers after the\n      # retry window expires — keeps production alive when the\n      # process hangs or the database connection is lost.\n      healthCheck:\n        httpGet:\n          port: 3000\n          path: /\n\n      verticalAutoscaling:\n        # V8 GC needs headroom for traffic spikes — reserve ~50%\n        # of minRam as free RAM to prevent OOM restarts.\n        minRam: 0.25\n        minFreeRamGB: 0.125\n\n  # Development setup — deploy full source for interactive\n  # development via SSH. With no run.start the container stays\n  # idle on its own, so the developer controls what runs.\n  - setup: dev\n    build:\n      base: nodejs@22\n\n      buildCommands:\n        # npm install (not npm ci) — works without a lock file,\n        # giving flexibility during early development stages.\n        - npm install\n\n      # Deploy the entire working directory — source code,\n      # node_modules (with devDependencies), and config files.\n      deployFiles: ./\n\n      cache:\n        - node_modules\n\n    run:\n      base: nodejs@22\n      # Ubuntu provides richer tooling (apt, curl, git, vim)\n      # for interactive development via SSH.\n      os: ubuntu\n\n      # Migration runs on every container start — execOnce\n      # ensures it only executes once per deploy version even\n      # when multiple containers are running.\n      initCommands:\n        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/migrate.ts\n\n      ports:\n        - port: 3000\n          httpSupport: true\n\n      envVariables:\n        NODE_ENV: development\n        DB_NAME: ${db_dbName}\n        DB_HOST: ${db_hostname}\n        DB_PORT: ${db_port}\n        DB_USER: ${db_user}\n        DB_PASS: ${db_password}\n\n      # No run.start — the dynamic dev container stays up as a\n      # workspace on its own; SSH in and run the process by hand:\n      #   npm run dev   (ts-node hot-reload via nodemon)\n      # or\n      #   npm start     (plain ts-node)","zeropsYamlSource":"live"},"status":"validation-failed","targetService":"appdev"}
```

---

## `import`
scenario=recipe-nextjs-ssr-frontend-standard | bytes=825 | input={}

```json
{"projectId":"2Biyb7d2TQeSum9HNtjLQQ","projectName":"zcp-eval","processes":[{"processId":"M5ON5nGxQPCMbuImz48OgA","actionName":"stack.create","status":"FINISHED","service":"db","serviceId":"ya3l8ix7Q2GSYf1FxeeqCA"},{"processId":"czo9cmSLRmC4Ayzn4dtYIw","actionName":"stack.create","status":"FINISHED","service":"app","serviceId":"DCjvlXzMSfCji02rYRTqww"},{"processId":"tMdTYBaERm2sBrhogBcCfA","actionName":"stack.build","status":"FINISHED","service":"app","serviceId":"DCjvlXzMSfCji02rYRTqww"},{"processId":"P0ABlX7oQlmOUcaXCq1e8Q","actionName":"stack.enableSubdomainAccess","status":"FINISHED","service":"app","serviceId":"DCjvlXzMSfCji02rYRTqww"}],"summary":"All 4 processes completed successfully","nextActions":"Verify services: zerops_discover. Continue workflow: mount dev, discover env vars, write code, then deploy."}
```

---

## `workflow:launch-production:start::status=scope-prompt`
scenario=launch-with-existing-cicd | bytes=4612 | input={"action": "start", "workflow": "launch-production"}

```json
{"workflow":"launch-production","status":"scope-prompt","phase":"launch-production-active","guidance":"### Launch scope — collect production target details\n\n**This workflow is stateless multi-call narrowing.** Every response's `inputs` block is the running accumulator: pass all previously-accepted parameters forward on every next `action=\"start\"` call. `action=\"complete\"` is reserved for bootstrap and returns `BOOTSTRAP_NOT_ACTIVE` here.\n\n#### First — identify the launch path\n\nlaunch-production has two mutation paths in one workflow. Pick which one matches the user's intent BEFORE collecting scope params; the choice surfaces in `inputs` and dispatches the right mutation at the `ready-to-launch` step.\n\n| User intent signal | Path | Required token params |\n|---|---|---|\n| \"Create new prod project\", \"launch to fresh project\", or no existing project mentioned | **NEW-PROJECT** | `launchKey` (one-shot launch-window token with project-creation permission — surfaced at the `ready-to-launch` step via the launch-mutation-key-required atom) |\n| \"I have existing prod project\", explicit project ID/token supplied, \"deploy into project X\" | **EXISTING-PROJECT** | `existingProjectId` + `existingProdToken` (project-scoped token from target project's dashboard) |\n\nIf the user explicitly hands you an existing project ID OR a project-scoped token, pass `existingProjectId` + `existingProdToken` on this first `action=\"start\"` call alongside the scope params below — both will land in the `inputs` accumulator and the workflow will skip the `launchKey` prompt at `ready-to-launch`. Otherwise default to NEW-PROJECT and let the workflow ask for `launchKey` later.\n\n#### Then — apply suggestions from `sourceContext`\n\n- **`productionProjectName`** — `sourceContext.suggestedTargetName` (`\u003csource\u003e-dev` / `\u003csource\u003e-stage` → `\u003csource\u003e-prod`, else `\u003csource\u003e-prod` appended). Confirm name with user; don't silently rename.\n- **`targetService`** — `sourceContext.promotionHeadline` when single. For standard-mode pairs the headline is the stage hostname (validated last-known-good); `devHostname` field discloses the iteration half. Either half is accepted as input — the handler normalizes internally. When the canonical post-normalization differs, `sourceContext.targetServiceCanonical` echoes the form the bundle composer will use. Managed deps are bundled implicitly.\n- **`promotables`** — multi-runtime promotion. Pass an array of `{hostname, prodHostname?, prodSetupNameOverride?}` entries when more than one runtime is being promoted into the same prod project (monorepo with app + worker, or separate-repos with multiple services). Empty/absent → falls back to single-runtime from `targetService`. Production hostname derivation: `appdev`/`appstage` → `app`, `workerstage` → `worker`. Pass `prodHostname` to override.\n- **`region`** — optional, default `eu-central`.\n- **`customDomain`** — optional; ZCP emits DNS records + verification probes, user attaches in Zerops UI.\n- **`keepNonHA`** — optional `[]hostname` to keep at `NON_HA` (default: all managed deps go `HA`).\n- **`envOverrides`** — optional plain-config overrides. No secret values; ZCP never receives them.\n\nWhen `sourceContext.availableRuntimes` has multiple entries, the user must pick. Use `AskUserQuestion` if your harness exposes it (structured choice UI); else surface the choice inline and wait for the user's next turn. For multi-runtime, ask the user whether to promote all or pick a subset — defensive default is \"primary runtime + infra now, other runtimes as separate additive launches\" unless promotables share the same source repo (monorepo).\n\nAfter scope is complete, ZCP runs the source-control gate — every promoted runtime must carry `meta.GitPushState=configured` + a live remote that matches the recorded value + a clean working tree with pushed HEAD. Unresolved blockers surface as `source-control-required` with per-runtime Recovery hints chaining `git-push-setup` / `zerops_deploy strategy=git-push` / `build-integration`. Once green, ZCP advances to `classify-prompt` for env classification.","blockers":[{"id":"scope-missing-productionProjectName","severity":"block","category":"scope","message":"workflow input \"productionProjectName\" required to advance to classify-prompt"}],"inputs":{},"sourceContext":{"sourceProjectName":"zcp-eval","suggestedTargetName":"zcp-eval-prod","availableRuntimes":[{"hostname":"appstage","type":"alpine/nodejs@22"},{"hostname":"appdev","type":"ubuntu/nodejs@22"}]}}
```

---

## `dev_server:start`
scenario=develop-add-managed-dep-to-existing | bytes=365 | input={"action": "start"}

```json
{"action":"start","hostname":"appdev","running":true,"port":3000,"healthPath":"/","healthStatus":200,"startMillis":5097,"logTail":"\n\u003e nodejs-hello-world@1.0.0 dev\n\u003e ts-node src/index.ts\n\nServer running on 0.0.0.0:3000","logFile":"/tmp/zcp-dev-server.log","message":"Dev server on appdev started and responded 200 at http://localhost:3000/ in 5097ms."}
```

---

## `workflow:export:start::status=compose-ready`
scenario=export-buildfromgit-self-snapshot | bytes=7679 | input={"action": "start", "workflow": "export"}

```json
{"bundle":{"importFile":"zerops-project-import.yaml","importYaml":"#zeropsPreprocessor=on\nproject:\n    envVariables:\n        APP_KEY: \u003c@generateRandomString(\u003c32\u003e)\u003e\n    name: zcp-eval\nservices:\n    - buildFromGit: https://github.com/example/teamapi.git\n      enableSubdomainAccess: true\n      hostname: appdev\n      maxContainers: 3\n      minContainers: 1\n      mode: NON_HA\n      type: ubuntu/nodejs@22\n      verticalAutoscaling:\n        cpuMode: SHARED\n        maxCpu: 3\n        maxDisk: 100\n        maxRam: 8\n        minCpu: 1\n        minDisk: 1\n        minRam: 0.5\n      zeropsSetup: dev\n    - hostname: db\n      mode: NON_HA\n      priority: 10\n      type: postgresql:single@18\n","repoUrl":"https://github.com/example/teamapi.git","setupName":"dev","warnings":["ServiceMeta.RemoteURL cache for \"appdev\" drifted (cache=\"https://github.com/zerops-recipe-apps/nodejs-hello-world-app\", live=\"https://github.com/example/teamapi.git\") — live value wins for the bundle; cache refreshed."],"zeropsFile":"zerops.yaml","zeropsYaml":"zerops:\n  # Production setup — compile TypeScript to JS, deploy\n  # compiled artifacts with production dependencies only.\n  - setup: prod\n    build:\n      base: nodejs@22\n\n      buildCommands:\n        # npm ci installs exact versions from package-lock.json\n        # for reproducible, auditable production builds.\n        - npm ci\n        - npm run build\n        # Strip dev-only packages (TypeScript, ts-node, type\n        # definitions) after compilation — runtime only needs\n        # production dependencies.\n        - npm prune --omit=dev\n\n      deployFiles:\n        - ./dist          # compiled JS (index.js + migrate.js)\n        - ./node_modules  # production dependencies only\n        - ./package.json\n\n      # Cache node_modules between builds to avoid re-downloading\n      # unchanged packages on every build trigger.\n      cache:\n        - node_modules\n\n    # Readiness check: verifies new containers respond at /\n    # before the project balancer routes traffic to them.\n    # Prevents requests reaching containers still starting up.\n    deploy:\n      readinessCheck:\n        httpGet:\n          port: 3000\n          path: /\n\n    run:\n      base: nodejs@22\n\n      # Run migration once per deploy across all containers.\n      # initCommands (not buildCommands) keeps migration and code\n      # deployment atomic — a failed deploy won't leave a migrated\n      # schema paired with old application code.\n      # --retryUntilSuccessful handles the brief window when the\n      # database port isn't yet accepting connections after import.\n      initCommands:\n        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js\n\n      ports:\n        - port: 3000\n          httpSupport: true\n\n      envVariables:\n        NODE_ENV: production\n        # Cross-service references — ${hostname_key} resolves to the\n        # value generated by the 'db' service at container start.\n        DB_NAME: ${db_dbName}\n        DB_HOST: ${db_hostname}\n        DB_PORT: ${db_port}\n        DB_USER: ${db_user}\n        DB_PASS: ${db_password}\n\n      start: node dist/index.js\n\n      # Health check restarts unresponsive containers after the\n      # retry window expires — keeps production alive when the\n      # process hangs or the database connection is lost.\n      healthCheck:\n        httpGet:\n          port: 3000\n          path: /\n\n  # Development setup — deploy full source for interactive\n  # development via SSH. With no run.start the container stays\n  # idle on its own, so the developer controls what runs.\n  - setup: dev\n    build:\n      base: nodejs@22\n\n      buildCommands:\n        # npm install (not npm ci) — works without a lock file,\n        # giving flexibility during early development stages.\n        - npm install\n\n      # Deploy the entire working directory — source code,\n      # node_modules (with devDependencies), and config files.\n      deployFiles: ./\n\n      cache:\n        - node_modules\n\n    run:\n      base: nodejs@22\n      # Ubuntu provides richer tooling (apt, curl, git, vim)\n      # for interactive development via SSH.\n      os: ubuntu\n\n      # Migration runs on every container start — execOnce\n      # ensures it only executes once per deploy version even\n      # when multiple containers are running.\n      initCommands:\n        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/migrate.ts\n\n      ports:\n        - port: 3000\n          httpSupport: true\n\n      envVariables:\n        NODE_ENV: development\n        DB_NAME: ${db_dbName}\n        DB_HOST: ${db_hostname}\n        DB_PORT: ${db_port}\n        DB_USER: ${db_user}\n        DB_PASS: ${db_password}\n\n      # No run.start — the dynamic dev container stays up as a\n      # workspace on its own; SSH in and run the process by hand:\n      #   npm run dev   (ts-node hot-reload via nodemon)\n      # or\n      #   npm start     (plain ts-node)"},"guidance":"You are exporting a deployed runtime so a fresh Zerops project can reproduce the same infrastructure from a single git repo. The output is one repository at the chosen runtime's `/var/www` containing source code, `zerops.yaml` (build/run/deploy pipeline), and `zerops-project-import.yaml` (project + service definitions with `buildFromGit:` pointing back at the same repo). Re-import on a new project happens via `zcli project project-import zerops-project-import.yaml` or the dashboard.\n\nThe export workflow is a three-call narrowing — probe, generate, publish — and `zerops_workflow workflow=\"export\"` carries each call. Some companion atoms refer to these as **Phase A** (probe — scope/variant prompts), **Phase B** (generate — classify/validate), and **Phase C** (publish — bundle + push).\n\n## Pick the runtime\n\nIf the project has multiple runtime services, the first call returns a `scope-prompt` listing hostnames; pass `targetService=\u003chostname\u003e` on the next call. For a project with a single runtime, the first call can already include `targetService` and skip this step.\n\n## Pick the variant (pair modes only)\n\nFor `mode=standard` and `mode=local-stage` pairs, pick `variant=dev` (packages the dev hostname's tree + zerops.yaml) or `variant=stage` (packages the stage hostname's tree). Both bundle entries emit Zerops scaling `mode=NON_HA` — the destination project's topology Mode is established by ZCP's bootstrap at re-import, not embedded in the bundle.\n\nSingle-half source modes (`dev`, `simple`, `local-only`) skip this prompt — the variant is forced.\n\n## What the next calls do\n\n| Call | Inputs you add | Returns `status=` |\n|---|---|---|\n| 2 | `targetService` + `variant` (if pair) | `classify-prompt` |\n| 3 | + `envClassifications` map (key → bucket per env) | `publish-ready` (or `validation-failed`) |\n\nThe status-specific section of the response carries content + commands; this table is a call-shape map, not a content cheatsheet.\n\nIf `/var/www/zerops.yaml` is missing or git remote is unconfigured, the response carries a status that walks the prereq (zerops.yaml scaffold or `git-push-setup`) instead — complete the prereq, then re-call export.","nextSteps":["Write zerops-project-import.yaml + zerops.yaml into the repo and commit.","OPTIONAL — to publish via git-push:","  zerops_workflow action=\"git-push-setup\" service=\"appdev\" remoteUrl=\u003cURL\u003e","  then re-call export targetService=\"appdev\" (→ publish-ready)"],"phase":"export-active","status":"compose-ready","targetService":"appdev"}
```

---

## `workflow:?:status::session-active`
scenario=discover-adoption-state-resumable-uses-sessionid | bytes=2979 | input={"action": "status"}

```json
{"kind":"session-active","sessionId":"sess-stale-mid-bootstrap-2026-05-27","intent":"","progress":{"total":3,"completed":1,"steps":[{"name":"discover","status":"complete"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"provision","index":1,"tools":["zerops_import","zerops_process","zerops_discover"],"verification":"SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND service types match plan AND managed dependency env vars recorded in session state. Runtime services are auto-mounted on completion.","detailedGuide":"### Discover env vars during provision\n\nOnce newly-provisioned (classic) or newly-attached (adopt) services have reached RUNNING / ACTIVE, run discovery so the session records env-var KEYS for every managed service. This is authoritative — do not guess alternative spellings; unknown cross-service references become literal strings at runtime and fail silently.\n\n```\nzerops_discover includeEnvs=true\n```\n\nRecord one row per service in the provision attestation. Keys are enough — values stay redacted; discovery is for cataloguing, not consumption. The develop response covers per-service canonical key names plus cross-service reference syntax (`${hostname_varName}`) when wiring `run.envVariables` at first deploy.\n\n**Adopt route — skip when no new wiring:** adopted services already carry their env wiring in the running app, so this discovery is only needed if THIS task adds NEW cross-service references. For a code-only change to an already-wired app (edit / redesign / bugfix), skip it and fetch keys lazily at wiring time — running it now is a no-op round-trip.\n\n**Pre-first-deploy caveat (classic route)**: classic creates runtime services with `startWithoutCode: true` so they reach RUNNING before any code lands; env vars in such containers live in the project catalogue, not `process.env`, until develop runs the first deploy and references fire. Adopted services are usually ACTIVE.\n\nWhen `zerops_discover` shows a runtime stuck at `status=READY_TO_DEPLOY`, branch on whether it ever tried to build (check `zerops_events`):\n\n- **Never built** (created without `startWithoutCode: true`, no failed build in the timeline): re-import with `startWithoutCode: true` + `override: true` to reach ACTIVE. Safe — there is no deployed code to lose.\n- **Build FAILED** (the timeline shows a failed build / prior deploy attempt): the service still holds the buildFromGit code that failed to build. DIAGNOSE first — `zerops_events` then `zerops_logs` — fix the cause (e.g. add the missing managed dependency the build needed), then re-deploy. Do **NOT** `override`: it REPLACES the service stack and wipes the very source you need to fix. (`override=true` on a service with deploy history returns `DIAGNOSIS_REQUIRED`; acknowledging `confirmDestructive` still wipes — only do it if the code lives elsewhere, e.g. git.)"},"message":"Step 2/3: provision"}
```

---

## `workflow:?:close-mode::status=updated`
scenario=classic-go-simple | bytes=420 | input={"action": "close-mode"}

```json
{"services":"app=auto","status":"updated","workSessionState":{"status":"auto-closed","closedAt":"2026-06-04T20:04:54Z","closeReason":"auto-complete","note":"All declared services deployed + verified — scope is green. Keep deploying into this session for more changes (nothing is lost). Call zerops_workflow action=\"close\" workflow=\"develop\" when this task is done, or action=\"start\" to begin a different task."}}
```

---

## `workflow:export:start::status=git-push-setup-required`
scenario=export-buildfromgit-self-snapshot | bytes=11910 | input={"action": "start", "workflow": "export"}

```json
{"guidance":"You are exporting a deployed runtime so a fresh Zerops project can reproduce the same infrastructure from a single git repo. The output is one repository at the chosen runtime's `/var/www` containing source code, `zerops.yaml` (build/run/deploy pipeline), and `zerops-project-import.yaml` (project + service definitions with `buildFromGit:` pointing back at the same repo). Re-import on a new project happens via `zcli project project-import zerops-project-import.yaml` or the dashboard.\n\nThe export workflow is a three-call narrowing — probe, generate, publish — and `zerops_workflow workflow=\"export\"` carries each call. Some companion atoms refer to these as **Phase A** (probe — scope/variant prompts), **Phase B** (generate — classify/validate), and **Phase C** (publish — bundle + push).\n\n## Pick the runtime\n\nIf the project has multiple runtime services, the first call returns a `scope-prompt` listing hostnames; pass `targetService=\u003chostname\u003e` on the next call. For a project with a single runtime, the first call can already include `targetService` and skip this step.\n\n## Pick the variant (pair modes only)\n\nFor `mode=standard` and `mode=local-stage` pairs, pick `variant=dev` (packages the dev hostname's tree + zerops.yaml) or `variant=stage` (packages the stage hostname's tree). Both bundle entries emit Zerops scaling `mode=NON_HA` — the destination project's topology Mode is established by ZCP's bootstrap at re-import, not embedded in the bundle.\n\nSingle-half source modes (`dev`, `simple`, `local-only`) skip this prompt — the variant is forced.\n\n## What the next calls do\n\n| Call | Inputs you add | Returns `status=` |\n|---|---|---|\n| 2 | `targetService` + `variant` (if pair) | `classify-prompt` |\n| 3 | + `envClassifications` map (key → bucket per env) | `publish-ready` (or `validation-failed`) |\n\nThe status-specific section of the response carries content + commands; this table is a call-shape map, not a content cheatsheet.\n\nIf `/var/www/zerops.yaml` is missing or git remote is unconfigured, the response carries a status that walks the prereq (zerops.yaml scaffold or `git-push-setup`) instead — complete the prereq, then re-call export.\n\n---\n\nYou hit `status=\"git-push-setup-required\"`. Phase C cannot publish until `meta.GitPushState=configured` (and `meta.RemoteURL` is cached). Run the `git-push-setup` action below — it provisions GIT_TOKEN, .netrc, and the remote URL the same way the develop workflow does.\n\n## Why this fires\n\nEither (a) `git remote get-url origin` returned empty in the chosen container's `/var/www` (no remote configured), OR (b) `meta.GitPushState != configured` (capability not yet provisioned in ZCP). In both cases, the response carries the bundle preview so you can review the yamls while resolving the prereq — re-running export later picks up the same bundle if the live state hasn't moved.\n\n## Resolve in two steps\n\n### 1. Run `git-push-setup`\n\n```\nzerops_workflow action=\"git-push-setup\" service=\"{targetHostname}\" remoteUrl=\"{repoUrl}\"\n```\n\nIf `GIT_TOKEN` is not yet set on the runtime container, the response is the walkthrough atom — run the steps it lists (set the token via `zerops_env action=\"set\" project=true variables=[\"GIT_TOKEN={token}\"]`, push once to confirm), then re-call with the same `remoteUrl` to stamp `GitPushState=configured`.\n\n`git-push-setup` confirm mode validates URL format and writes `meta.GitPushState=configured` + `meta.RemoteURL`, but it does NOT verify that `GIT_TOKEN` actually authenticates against the remote. A subsequent push (during export Phase C, or any later `zerops_deploy strategy=\"git-push\"`) can still surface `failureClassification.category=credential` if the token is rejected — re-run `git-push-setup` to rotate the token and try again.\n\nThe walkthrough returned by `git-push-setup` is selected by the current ZCP runtime environment, not the chosen service's mode. If you are running zcp inside a Zerops container, you get the container walkthrough; if you are running locally, you get the local walkthrough. For a runtime that lives on the local machine (`mode=local-stage` / `mode=local-only`), invoke `git-push-setup` from a local zcp invocation so the local walkthrough fires.\n\n### 2. Re-call export with the same inputs\n\n```\nzerops_workflow workflow=\"export\" \\\n  targetService=\"{targetHostname}\" \\\n  variant=\"\u003cyour-pick\u003e\" \\\n  envClassifications=\u003cyour map: each project env mapped to its bucket\u003e\n```\n\nThe handler re-runs Phase A → Phase B with the same inputs, re-checks `meta.GitPushState`, and SHOULD land at `status=\"publish-ready\"` if no other prereq changed. If state moved in the meantime (new envs added to the project, `zerops.yaml` removed, scaling change), the response may instead be `scaffold-required`, `classify-prompt`, or another chain. Read the new `status` and `nextSteps` and re-supply the same inputs (re-classify any new envs surfaced in the prompt) — never assume the second call publishes.\n\nThe bundle preview you saw before the chain may differ slightly if the project state shifted in between — diff the new `bundle.importYaml` against the prior preview before writing.\n\n## What if the remote URL has changed\n\n`meta.RemoteURL` is cached when `git-push-setup` confirm mode runs (`zerops_workflow action=\"git-push-setup\"` with `remoteUrl=\u003curl\u003e` writes the cache). If `git remote get-url origin` now returns a different URL than `meta.RemoteURL`, run `git-push-setup` again with the corrected `remoteUrl=` — that overwrites the cache with the new value. The export workflow always reads the live remote (not the cache), so after the cache is fixed both sources agree and the publish step unblocks. The export handler also refreshes `meta.RemoteURL` from the live remote on every pass (and surfaces a warning when they diverged) — so a manual `git-push-setup` re-run is reserved for intentional remote-URL changes, not ordinary cache drift.\n\n## What if you cannot resolve the prereq\n\nIf the runtime is intentionally pull-only (no push capability) and you still want to export the bundle for review, the workflow does not yet support a \"compose-only / no-publish\" mode. The Phase A + Phase B body is in the current response (`bundle.importYaml`, `bundle.zeropsYaml`) — you may copy those bodies out manually for review, BUT the bundle is a snapshot of the project's state at the moment this response was generated. If you act on it later (e.g. paste into a new project's repo days later), the snapshot may have drifted from live state (new envs, scaling, schema changes). Always re-run export immediately before manual extraction; do not act on a stored copy.","nextSteps":["Run zerops_workflow action=\"git-push-setup\" service=\"appdev\" remoteUrl=\u003cURL\u003e","After confirm, re-call: zerops_workflow workflow=\"export\" targetService=\"appdev\""],"phase":"export-active","preview":{"importYaml":"#zeropsPreprocessor=on\nproject:\n    envVariables:\n        APP_KEY: \u003c@generateRandomString(\u003c32\u003e)\u003e\n    name: zcp-eval\nservices:\n    - buildFromGit: https://github.com/example/teamapi\n      enableSubdomainAccess: true\n      hostname: appdev\n      mode: NON_HA\n      type: ubuntu/nodejs@22\n      zeropsSetup: dev\n    - hostname: db\n      mode: NON_HA\n      priority: 10\n      type: postgresql:single@18\n","repoUrl":"https://github.com/example/teamapi","setupName":"dev","warnings":["ServiceMeta.RemoteURL cache for \"appdev\" drifted (cache=\"https://github.com/zerops-recipe-apps/nodejs-hello-world-app\", live=\"https://github.com/example/teamapi\") — live value wins for the bundle; cache refreshed."],"zeropsYaml":"zerops:\n  # Production setup — compile TypeScript to JS, deploy\n  # compiled artifacts with production dependencies only.\n  - setup: prod\n    build:\n      base: nodejs@22\n\n      buildCommands:\n        # npm ci installs exact versions from package-lock.json\n        # for reproducible, auditable production builds.\n        - npm ci\n        - npm run build\n        # Strip dev-only packages (TypeScript, ts-node, type\n        # definitions) after compilation — runtime only needs\n        # production dependencies.\n        - npm prune --omit=dev\n\n      deployFiles:\n        - ./dist          # compiled JS (index.js + migrate.js)\n        - ./node_modules  # production dependencies only\n        - ./package.json\n\n      # Cache node_modules between builds to avoid re-downloading\n      # unchanged packages on every build trigger.\n      cache:\n        - node_modules\n\n    # Readiness check: verifies new containers respond at /\n    # before the project balancer routes traffic to them.\n    # Prevents requests reaching containers still starting up.\n    deploy:\n      readinessCheck:\n        httpGet:\n          port: 3000\n          path: /\n\n    run:\n      base: nodejs@22\n\n      # Run migration once per deploy across all containers.\n      # initCommands (not buildCommands) keeps migration and code\n      # deployment atomic — a failed deploy won't leave a migrated\n      # schema paired with old application code.\n      # --retryUntilSuccessful handles the brief window when the\n      # database port isn't yet accepting connections after import.\n      initCommands:\n        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js\n\n      ports:\n        - port: 3000\n          httpSupport: true\n\n      envVariables:\n        NODE_ENV: production\n        # Cross-service references — ${hostname_key} resolves to the\n        # value generated by the 'db' service at container start.\n        DB_NAME: ${db_dbName}\n        DB_HOST: ${db_hostname}\n        DB_PORT: ${db_port}\n        DB_USER: ${db_user}\n        DB_PASS: ${db_password}\n\n      start: node dist/index.js\n\n      # Health check restarts unresponsive containers after the\n      # retry window expires — keeps production alive when the\n      # process hangs or the database connection is lost.\n      healthCheck:\n        httpGet:\n          port: 3000\n          path: /\n\n  # Development setup — deploy full source for interactive\n  # development via SSH. With no run.start the container stays\n  # idle on its own, so the developer controls what runs.\n  - setup: dev\n    build:\n      base: nodejs@22\n\n      buildCommands:\n        # npm install (not npm ci) — works without a lock file,\n        # giving flexibility during early development stages.\n        - npm install\n\n      # Deploy the entire working directory — source code,\n      # node_modules (with devDependencies), and config files.\n      deployFiles: ./\n\n      cache:\n        - node_modules\n\n    run:\n      base: nodejs@22\n      # Ubuntu provides richer tooling (apt, curl, git, vim)\n      # for interactive development via SSH.\n      os: ubuntu\n\n      # Migration runs on every container start — execOnce\n      # ensures it only executes once per deploy version even\n      # when multiple containers are running.\n      initCommands:\n        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/migrate.ts\n\n      ports:\n        - port: 3000\n          httpSupport: true\n\n      envVariables:\n        NODE_ENV: development\n        DB_NAME: ${db_dbName}\n        DB_HOST: ${db_hostname}\n        DB_PORT: ${db_port}\n        DB_USER: ${db_user}\n        DB_PASS: ${db_password}\n\n      # No run.start — the dynamic dev container stays up as a\n      # workspace on its own; SSH in and run the process by hand:\n      #   npm run dev   (ts-node hot-reload via nodemon)\n      # or\n      #   npm start     (plain ts-node)","zeropsYamlSource":"live"},"reason":"GitPushState != configured","status":"git-push-setup-required","targetService":"appdev"}
```

---

## `workflow:launch-production:start::status=classify-prompt`
scenario=launch-with-existing-cicd | bytes=8540 | input={"action": "start", "workflow": "launch-production"}

```json
{"workflow":"launch-production","status":"classify-prompt","phase":"launch-production-active","guidance":"### Launch classify — bucket source envs before production publish\n\nYou are at `status=\"classify-prompt\"`. The launch composer needs every source `project.envVariables` entry classified into one of four buckets — `infrastructure`, `auto-secret`, `external-secret`, `plain-config` — before it can emit the production import bundle.\n\n**Call shape — `action=\"start\"` always.** Launch-production is stateless multi-call narrowing: every advance is another `zerops_workflow action=\"start\" workflow=\"launch-production\"` with the FULL accumulated `inputs` block from the prior response plus `envClassifications`. There is NO `action=\"classify\"` step (that's the recipe-fact workflow — wrong tool). There is NO `action=\"complete\"` step (that's bootstrap). Re-call `action=\"start\"` with the accumulated inputs and the new classification map:\n\n```\nzerops_workflow action=\"start\" workflow=\"launch-production\" \\\n  productionProjectName=\"\u003cfrom inputs\u003e\" \\\n  targetService=\"\u003cfrom inputs\u003e\" \\\n  region=\"\u003cfrom inputs\u003e\" \\\n  envClassifications={\"APP_KEY\":\"auto-secret\",\"DB_HOST\":\"infrastructure\",\"STRIPE_KEY\":\"external-secret\"}\n```\n\nIf you skip an env, the next response re-prompts with the remaining unclassified keys. Extra keys that don't match any source env are informational — the composer ignores them.\n\n## The four buckets\n\n| Bucket | Detection signal | Emit in production project |\n|---|---|---|\n| `infrastructure` | Value (or component) resolves from a managed-service reference (`${db_*}`, `${redis_*}`, `${mongo_*}`, plus per-service prefixes). Includes app-built compound URLs assembled at runtime from `${...}` components. | DROP from `project.envVariables`. The reference still lives in `zerops.yaml`'s `run.envVariables`; the re-imported managed service emits a fresh value at boot. |\n| `auto-secret` | Source code uses the var as a local encryption / signing key (framework owns the call; rarely visible in app code). | `\u003c@generateRandomString(\u003c32\u003e)\u003e`. Each launch gets a fresh secret. |\n| `external-secret` | Source calls a third-party SDK with the var (Stripe, OpenAI, Mailgun, GitHub, …). Includes aliased imports + webhook verification secrets. | Comment + `\u003c@pickRandom([\"REPLACE_ME\"])\u003e`. New project's owner pastes the real key into the dashboard before deploy. |\n| `plain-config` | Source uses the var as literal runtime config (LOG_LEVEL, NODE_ENV, FEATURE_FLAGS, …). | Literal value verbatim. |\n\n`zerops_workflow` returns each unclassified env's key but NOT its value — fetch values via `zerops_discover service=\"{targetHostname}\" includeEnvs=true includeEnvValues=true`, then grep them against the mounted source tree (when accessible) before bucketing.\n\nEvery row carries `suggestedBucket` + `rationale` computed server-side from the env key NAME alone (never the value, per the no-leak invariant). Treat the suggestion as a starting point — the four-bucket detection table below remains authoritative when you override. Common reasons to override: a credential-pattern match (`*_KEY`, `*_TOKEN`) that's actually plain-config in your app, or a plain-config name (`DB_HOST`) whose value resolves to a managed-service reference (`${db_*}`) and should bucket `infrastructure`.\n\n## Worked examples per bucket\n\n### Infrastructure\n\n```\nDB_HOST=${db_hostname}\nREDIS_URL=${redis_connectionString}\n```\n\nBoth resolve from managed-service references — bucket `infrastructure`. The new prod project's `db` and `redis` services emit fresh values at boot. Compound case: `DATABASE_URL` assembled in app code from `${DB_USER}`, `${DB_PASSWORD}` — the COMPONENT envs are `infrastructure`. If `DATABASE_URL` is itself a project env resolving to managed refs, bucket it `infrastructure`; if assembled manually with literal credentials, bucket `external-secret`.\n\n### Auto-secret\n\n```\nAPP_KEY=existing-key    # Laravel — encrypts cookies/session\nSECRET_KEY=django…      # Django — signs sessions, CSRF\nJWT_SECRET=long-bytes   # Node — signs tokens\n```\n\nFramework convention drives detection: Laravel `APP_KEY`, Django `SECRET_KEY`, Rails `SECRET_KEY_BASE`, Express `SESSION_SECRET` / `JWT_SECRET`. **Stability warning**: if persisted state (encrypted cookies, signed tokens, encrypted DB columns) depends on the existing key, regenerating breaks it. Ask the user before bucketing `auto-secret` for a non-greenfield prod migration — the alternative is `plain-config` (carry the existing key forward).\n\n### External secret\n\n```\nSTRIPE_SECRET=sk_live_xyz…\nOPENAI_API_KEY=sk-proj-…\nMAILGUN_API_KEY=key-…\nGITHUB_TOKEN=ghp_…\n```\n\nSource contains the SDK call (`stripe(env.STRIPE_SECRET)`, etc.). Aliased imports still count: `from stripe import Stripe as PaymentProvider; PaymentProvider(env.SECRET)`. Webhook-verification secrets (`stripe.webhooks.constructEvent`) also bucket `external-secret`. Empty / sentinel values (`STRIPE_SECRET=`, `disabled`, `sk_test_*`, `test_xxx`, `none`) are review-required — `REPLACE_ME` breaks startup if the app validates on init. Bucket `external-secret` only if a real prod value is needed; otherwise `plain-config` keeps the existing.\n\n### Plain config\n\n```\nLOG_LEVEL=info\nNODE_ENV=production\nFEATURE_FLAGS=experiments_v2,beta_signups\nAPP_URL=${zeropsSubdomainHost}\n```\n\nLiteral runtime config. Privacy flag: real emails (`MAIL_FROM_ADDRESS=ops@acme.com`), customer names, internal domain names, sender identities are technically `plain-config` but emitting them into a fresh prod project leaks PII. Surface to the user before bucketing — they may want to redact or rotate.\n\n## Platform-injected tokens\n\n`GIT_TOKEN` and `ZCP_API_KEY` appear in source-project envs but are ZCP-side infrastructure (re-injected by the launch handler for the new project's git push + MCP session). Bucket both as `infrastructure` — they will be DROPPED from `project.envVariables` and the prod project re-receives them via its own launch flow. Do NOT bucket them as `external-secret` (`REPLACE_ME` would break the prod project's first git push).\n\n## Common mis-classification traps\n\n- **APP_KEY across a stateful app** (M3): auto-generating breaks existing encrypted columns / session cookies. If state continuity matters, bucket `plain-config` and carry the existing value forward.\n- **`STRIPE_SECRET=` empty in staging** (M4): `REPLACE_ME` placeholder breaks startup if the app validates on init. Bucket `external-secret` only if a real prod value is needed; otherwise `plain-config`.\n- **Compound `DATABASE_URL` with literal credentials** (M2): looks like infrastructure but it's a hand-rolled URL. Bucket `external-secret`.\n- **`MAIL_FROM_ADDRESS=ops@acme.com`** (M5): literal config, but the email is real. Flag privacy; consider placeholder before launch.\n- **Test-fixture values** (`TEST_API_KEY=test_xxx` consumed only by tests, M6): bucket `plain-config` only if read at runtime; if every reference is inside a test file, drop the env entirely before launch.\n- **Non-default managed-service prefixes** (M7): a custom Mongo/Postgres/MySQL may emit envs as `${mongo_connectionString}` / `${postgres_*}` / `${mysql_*}` instead of `${db_*}`. Inspect the discover response's `services[].envs` array — false-negative `plain-config` here emits literal hostname/password into the prod project.\n\nIf a row is genuinely ambiguous, the safest default is `plain-config` (carries the existing value) plus a follow-up review with the user — wrong-direction errors there are fixable post-launch without breaking deploy.","classifications":[{"key":"APP_KEY","currentBucket":"","suggestedBucket":"auto-secret","rationale":"key matches credentialPattern (_KEY|_SECRET|_TOKEN|_PASS|APP_KEY suffix); verify state continuity for migrate-into-existing-project path"},{"key":"GIT_TOKEN","currentBucket":"","suggestedBucket":"infrastructure","rationale":"ZCP control-plane / platform re-emits on import"},{"key":"ZCP_API_KEY","currentBucket":"","suggestedBucket":"infrastructure","rationale":"ZCP control-plane / platform re-emits on import"}],"sourceContext":{"sourceProjectName":"zcp-eval","suggestedTargetName":"zcp-eval-prod","availableRuntimes":[{"hostname":"appstage","type":"alpine/nodejs@22","mode":"standard","devHostname":"appdev"}],"promotionHeadline":"appstage","targetServiceCanonical":"appdev"}}
```

---

## `workflow:?:build-integration::status=configured`
scenario=launch-with-existing-cicd | bytes=4143 | input={"action": "build-integration"}

```json
{"alternateWorkflowFiles":[{"content":"name: Zerops deploy\non:\n  push:\n    branches: [main]\njobs:\n  deploy:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - uses: zeropsio/actions@v1.0.2\n        with:\n          access-token: ${{ secrets.ZEROPS_TOKEN }}\n          service-id: ${{ secrets.ZEROPS_SERVICE_ID }}\n","description":"Use only when zerops.yaml has a single setup and no explicit --setup selection is required; zeropsio/actions exposes service-id/access-token only.","path":".github/workflows/zerops.yml","variant":"single-setup-action"}],"buildIntegration":"actions","buildSetup":"prod","buildTarget":"appstage","ghAuthPrecondition":{"description":"The `gh secret set` commands below require an authenticated `gh` CLI. Fresh containers + workstations do NOT have `gh auth` cached. Before running the secret commands, authenticate `gh` with a PAT that has `Secrets: Read and write` on krls2020/eval2 (the same PAT used for git-push-setup works if its scope covers Secrets+Workflows — the recommended default).","failureSymptom":"HTTP 401: Bad credentials on the first `gh secret set` invocation = `gh` was not authenticated.","required":true,"setupCommand":"echo \"$ZCP_E2E_GITHUB_PAT\" | gh auth login --with-token  # container: token from env-var passed via Bash by the user","verifyCommand":"gh auth status"},"ghPatRecommendation":"Default to a fine-grained GitHub PAT scoped ONLY to krls2020/eval2 with `Secrets: Read and write` (single-repo blast radius). GitHub PATs require an expiration — pick the longest you're comfortable with (max 1 year); set a calendar reminder to regenerate + re-run `gh secret set` before it lapses.","nextStep":"1) Authenticate `gh` (see ghAuthPrecondition.setupCommand). 2) Write workflowFile.content at .github/workflows/zerops.yml. 3) Run the two `gh secret set` commands above. 4) Push the workflow file. From then on every push to main triggers the GitHub Actions deploy. Keep the default setup-aware zcli workflow unless you are certain the repository has only one setup.","pushSource":"appdev","secrets":[{"command":"gh secret set ZEROPS_TOKEN -b \"$ZCP_API_KEY\" -R krls2020/eval2","name":"ZEROPS_TOKEN","reuse":"Same Zerops PAT as ZCP_API_KEY — DON'T generate a new token. ZCP already holds the value; reuse it as the GitHub secret to keep one credential, one rotation surface.","source":"ZCP runs in a Zerops container; ZCP_API_KEY is in the container env. The command below substitutes via $ZCP_API_KEY at shell-expansion time — the literal value never crosses the MCP wire."},{"command":"gh secret set ZEROPS_SERVICE_ID -b \"kg97DBbCQ4KCBHqTtoFIRA\" -R krls2020/eval2","name":"ZEROPS_SERVICE_ID","value":"kg97DBbCQ4KCBHqTtoFIRA"}],"service":"appdev","status":"configured","topologyNote":"Standard-pair build-integration: configured per-pair from the dev half \"appdev\" (push source = meta-mutation target); CI runs `zcli push --setup prod` so the build lands on the stage half \"appstage\" (build target). Actions secrets/deep-links below reflect \"appstage\".","workSessionState":{"status":"none","note":"No active develop session — deploy not tracked. Start one via zerops_workflow action=\"start\" workflow=\"develop\" intent=\"...\" scope=[...] to pick up auto-close + verify tracking."},"workflowFile":{"content":"name: Zerops deploy\non:\n  push:\n    branches: [main]\njobs:\n  deploy:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@v4\n      - name: Install zcli\n        run: |\n          curl -sSL https://zerops.io/zcli/install.sh | sh\n          echo \"$HOME/.local/bin\" \u003e\u003e \"$GITHUB_PATH\"\n      - name: Deploy to Zerops\n        run: |\n          zcli login \"$ZEROPS_TOKEN\"\n          zcli push --service-id \"${{ secrets.ZEROPS_SERVICE_ID }}\" --setup \"prod\"\n        env:\n          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}\n","description":"Default workflow: installs zcli directly and passes --setup, so it works when zerops.yaml has multiple setups or the setup must be selected explicitly.","path":".github/workflows/zerops.yml","setup":"prod","variant":"setup-aware-zcli"}}
```

---

## `workflow:launch-production:status::prose`
scenario=launch-production-pipeline-not-configured | bytes=7112 | input={"action": "status", "workflow": "launch-production"}

```json
## Status
Phase: idle
Services: appdev, appstage, db
  - appdev (ubuntu/nodejs@22) — not bootstrapped
  - appstage (alpine/nodejs@22) — not bootstrapped
  - db (postgresql:single@18) — managed
Guidance:
  ### Bootstrap is two-phase

  First call returns `kind: "route-menu"` listing `routeOptions[]` — no
  session is open yet. Second call opens the session and returns
  `kind: "session-active"`. Read `kind` on every response.

  ```
  zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"
     → kind="route-menu", routeOptions=[...]
  zerops_workflow action="start" workflow="bootstrap" route="<picked>" \
     recipeSlug="<slug>"   # if route="recipe"
     sessionId="<id>"      # if route="resume"
     → kind="session-active"
  ```

  ### Coexistence with existing services

  Existing services in the project (bootstrapped or not) are **independent**.
  Bootstrap creates **new** services alongside them — it does not modify,
  re-import, or replace existing ones. To add a service to a project that
  already has some, pick a non-colliding hostname and bootstrap normally.
  `zerops_import override=true` is destructive and reserved for explicit
  user requests (config change on a known service), never the default
  path when bootstrap surfaces a hostname collision.

  ### Ranked options

  | Route | Present when | Carries | Dispatch / rule |
  |---|---|---|---|
  | `resume` | Snapshot has `resumable: true` | `resumeSession`, `resumeServices` | Pick first unless intentionally overriding: `route="resume" sessionId="<resumeSession>"`. |
  | `adopt` | Runtime services lack bootstrap records (`not bootstrapped`) | `adoptServices[]` | Attach ZCP tracking to running services — no infra change. Use when the user's intent matches the listed `adoptServices[]`. To add NEW services alongside (instead of adopting these), use `classic`. |
  | `recipe` | Up to three recipe matches | `recipeSlug`, `confidence`, `collisions[]` | `route="recipe" recipeSlug="<value from routeOptions[].recipeSlug>"`. Copy the slug verbatim from the discover response — corpus slugs don't carry a `zerops-` prefix even when users name a recipe by its branded form (`"zerops-laravel-minimal"`). Collisions recover by runtime rename or same-type managed `resolution: EXISTS`; switch routes only for different-type managed collision or independent infra. |
  | `classic` | Always available | none | `route="classic"` for manual planning. Default path for creating new services in any project state — fresh project or alongside existing ones. |

  ### Explicit overrides

  Explicit `route` on the first call bypasses discovery. Use only after
  prior discovery or direct user route choice. Valid values:
  `adopt`, `recipe`, `classic`, `resume`. Empty route re-enters discovery.

  ### Collision semantics

  `collisions[]` annotates recipe options; enforcement happens at plan
  submission. Pre-plan hostnames: rename runtimes or set managed deps to
  `EXISTS` before submitting.
  Per-service `adoptionState` in `zerops_discover` output classifies each
  service into one of five states: `adopted` (ZCP-tracked, ready for
  develop/deploy), `adoptable` (live runtime without ServiceMeta — call
  bootstrap route=adopt), `resumable` (mid-bootstrap, owned by prior
  session — call bootstrap route=resume with the session ID surfaced in
  the warning), `managed-dep` (db/cache/storage, no adoption concept),
  `zcp-self` (control-plane container, never adopted). The discover
  response also surfaces directive warnings naming exact recovery calls
  per state — read them before deriving anything from per-service flags.

  **FIRST CALL when discover surfaces adoptable services:** open the
  bootstrap-adopt session immediately, with the SAME intent string you'd
  pass to develop/deploy later. Do NOT probe with `workflow="develop"`
  expecting an ADOPT_REQUIRED redirect — the redirect works but costs a
  wasted round-trip + clutters the session log with a rejected start.

  Concrete shape (commits the route on the first call — skip the menu
  when adopt intent is already clear from discover output):

  ```
  zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="<one-line user task summary>"
  ```

  Replace `<one-line user task summary>` with the actual task intent
  (e.g. `intent="redesign appdev homepage as tech blog"`) — NOT a
  placeholder, NOT a generic "adopt existing". The intent threads
  through to the develop session that follows, so phrasing it as the
  real task scope avoids a re-typed intent on the next call.

  Service-scoped tools (`workflow="develop"`, `zerops_deploy`,
  `zerops_verify`) reject with `ADOPT_REQUIRED` until adoption completes.
  That gate is structural backstop, not the primary path — read the
  warning, fire adopt directly.

  Services in project, `not bootstrapped`. Two primary paths, both
  legitimate; existing services stay independent either way:

  1. **Adopt the listed services** — attach ZCP tracking to running
     services without changing their code, config, or scale.
  2. **Create new services alongside** — pick non-colliding hostnames
     and bootstrap normally; existing services keep running untouched.

  Re-importing or rewriting the existing services is **not** one of
  these paths — `zerops_import override=true` is destructive and only
  runs on explicit user request for a known service.

  **Branch on intent.** Clear adopt intent ("adopt", "převzít", named
  existing hostnames) → run adopt, no menu. Clear add-new intent
  ("add another", "new service") → use `classic`, no menu. Unclear →
  offer routes, not workflows.

  - ✅ "Adopt existing appdev/appstage, or create new alongside?"
  - ❌ "Develop on appdev, finish staging, or something else?" — those
    workflows aren't reachable yet; framing presents the unreachable as
    available.

  Start (route-menu, no session yet):

  ```
  zerops_workflow action="start" workflow="bootstrap" intent="adopt existing"
  ```

  Commit (opens session):

  ```
  zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="adopt existing"
  ```

  Type field in the plan carries the full identifier from
  `zerops_discover` verbatim — `alpine/nodejs@22`, `postgresql:single@18`.
  Legacy `os:` + `mode:` sibling fields still accepted for BC but the
  composite `type` is canonical; don't split. Pair-OS mismatch
  (ubuntu/alpine) accepted silently — dev half's type is what the
  plan carries.

  Close: each adopted hostname stamps `bootstrapped: true`, mode preserved.
  Close-mode + git-push stay empty (develop configures on first use).

  After adopt completes the runtime becomes a valid `launch-production`
  source — the adopted ServiceMeta carries the pair identity + setup
  cascade state, so `zerops_workflow workflow="launch-production"
  targetService=<adopted-hostname>` lands on the canonical dev-half
  without an extra normalization round-trip.
Next:
  ▸ Primary: Adopt unmanaged runtimes — zerops_workflow action="start" route="adopt" workflow="bootstrap"

```

---

## `workflow:bootstrap/resume:start::session-active`
scenario=discover-adoption-state-resumable-uses-sessionid | bytes=2979 | input={"action": "start", "workflow": "bootstrap", "route": "resume"}

```json
{"kind":"session-active","sessionId":"sess-stale-mid-bootstrap-2026-05-27","intent":"","progress":{"total":3,"completed":1,"steps":[{"name":"discover","status":"complete"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"provision","index":1,"tools":["zerops_import","zerops_process","zerops_discover"],"verification":"SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND service types match plan AND managed dependency env vars recorded in session state. Runtime services are auto-mounted on completion.","detailedGuide":"### Discover env vars during provision\n\nOnce newly-provisioned (classic) or newly-attached (adopt) services have reached RUNNING / ACTIVE, run discovery so the session records env-var KEYS for every managed service. This is authoritative — do not guess alternative spellings; unknown cross-service references become literal strings at runtime and fail silently.\n\n```\nzerops_discover includeEnvs=true\n```\n\nRecord one row per service in the provision attestation. Keys are enough — values stay redacted; discovery is for cataloguing, not consumption. The develop response covers per-service canonical key names plus cross-service reference syntax (`${hostname_varName}`) when wiring `run.envVariables` at first deploy.\n\n**Adopt route — skip when no new wiring:** adopted services already carry their env wiring in the running app, so this discovery is only needed if THIS task adds NEW cross-service references. For a code-only change to an already-wired app (edit / redesign / bugfix), skip it and fetch keys lazily at wiring time — running it now is a no-op round-trip.\n\n**Pre-first-deploy caveat (classic route)**: classic creates runtime services with `startWithoutCode: true` so they reach RUNNING before any code lands; env vars in such containers live in the project catalogue, not `process.env`, until develop runs the first deploy and references fire. Adopted services are usually ACTIVE.\n\nWhen `zerops_discover` shows a runtime stuck at `status=READY_TO_DEPLOY`, branch on whether it ever tried to build (check `zerops_events`):\n\n- **Never built** (created without `startWithoutCode: true`, no failed build in the timeline): re-import with `startWithoutCode: true` + `override: true` to reach ACTIVE. Safe — there is no deployed code to lose.\n- **Build FAILED** (the timeline shows a failed build / prior deploy attempt): the service still holds the buildFromGit code that failed to build. DIAGNOSE first — `zerops_events` then `zerops_logs` — fix the cause (e.g. add the missing managed dependency the build needed), then re-deploy. Do **NOT** `override`: it REPLACES the service stack and wipes the very source you need to fix. (`override=true` on a service with deploy history returns `DIAGNOSIS_REQUIRED`; acknowledging `confirmDestructive` still wipes — only do it if the code lives elsewhere, e.g. git.)"},"message":"Step 2/3: provision"}
```

---

## `events`
scenario=resume-after-compaction | bytes=1047 | input={}

```json
{"projectId":"2Biyb7d2TQeSum9HNtjLQQ","events":[{"timestamp":"2026-06-04T19:54:01Z","type":"build","action":"build","status":"ACTIVE","service":"appdev","hint":"DEPLOYED: App version is deployed and running. Build pipeline complete. No further polling needed."},{"timestamp":"2026-06-04T19:54:01.049Z","type":"process","action":"subdomain-enable","status":"FINISHED","service":"appdev","duration":"0s","user":"zcp-zcp-eval","processId":"ENPBG3VHRKaqAdHkejqewA","hint":"COMPLETE: Process finished successfully."},{"timestamp":"2026-06-04T19:54:01.024Z","type":"process","action":"build","status":"FINISHED","service":"appdev","duration":"47s","user":"zcp-zcp-eval","processId":"KzGVTWw5RnSOZTtMuKCB3g","hint":"COMPLETE: Process finished successfully."},{"timestamp":"2026-06-04T19:54:01.007Z","type":"process","action":"stack.create","status":"FINISHED","service":"appdev","duration":"0s","user":"zcp-zcp-eval","processId":"9ln5lvWARiy5dvJCxiRW2g","hint":"COMPLETE: Process finished successfully."}],"summary":{"total":4,"processes":3,"deploys":1}}
```

---

## `workflow:launch-production:start::status=launched`
scenario=launch-with-existing-cicd | bytes=5603 | input={"action": "start", "workflow": "launch-production"}

```json
{"workflow":"launch-production","status":"launched","phase":"launch-production-active","guidance":"### Delete the launch-window API key\n\nThe production project is live. **Delete the launch-window key now** so ZCP has no further path to mutate prod:\n\n1. Open [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management).\n2. Find the token named `zcp-launch-\u003cproduction-project-name\u003e`.\n3. Click **Revoke** (or **Delete**).\n\nZCP has already discarded the in-memory copy. Revoking the key in Zerops dashboard closes the trust boundary completely.\n\n### Configure CD pipeline in Zerops dashboard\n\nThe production runtime has no CD pipeline yet — ongoing pushes will NOT auto-build. Configure it once via dashboard. (ZCP cannot do this through the launch-window key; see `plans/backlog/launch-pipeline-close-loop-oauth.md` for the Path A future.)\n\nFor each runtime listed in the `pipeline-not-configured-*` blockers:\n\n1. Open the **deep-link** from the blocker (`https://app.zerops.io/service-stack/\u003csvcID\u003e/deploy`).\n2. Click **Connect to GitHub** (or GitLab). Authorize Zerops if asked — uses your existing org-level grant, no extra setup.\n3. Select the source repository listed in the blocker's `recommendation.repositoryFullName`.\n4. Set the trigger:\n   - **Event type:** `Tag`\n   - **Tag regex:** the value from `recommendation.tagRegex` (default `^v\\d+\\.\\d+\\.\\d+$` per Zerops production-checklist).\n   - **Zerops YAML setup:** `prod` (matches the setup block written during launch).\n5. Save.\n\nRepeat for each runtime in the blockers list. When done, re-call `workflow=\"launch-production\"` with the same `launchKey` — ZCP reads the live integration status and clears the blockers from the response.\n\nTo deploy after setup: `git tag v1.0.0 \u0026\u0026 git push --tags` (matching your tag regex).\n\n### Launch complete — user-owned steps remaining\n\nZCP has imported services and validated first deploy. The following steps require the user to act in the Zerops dashboard. ZCP cannot perform them (no standing prod access).\n\n**Production L7 exposure baseline — production has NO HTTP access enabled by default.**\n\n`{hostname}_zeropsSubdomain` env vars are populated on every HTTP-eligible runtime (platform always emits them), but the launch composer strips `enableSubdomainAccess` from the production import YAML per P-PROD-2 — so no L7 backend is registered. `curl` to that URL returns 502 until you either attach a custom domain OR explicitly enable the zerops.app subdomain in the prod project's dashboard.\n\nThis is intentional, not a bug. Production prefers a custom domain over the `*.zerops.app` developer URL. Pick ONE path below before treating the launch as user-reachable; both paths require dashboard action against the prod project.\n\n1. **Delete the launch-window key** — open Settings → Access Tokens Management and revoke the token named `zcp-launch-\u003cproduction-project-name\u003e`.\n2. **Set external secrets** — open the production project, navigate to each service that needs Stripe/OpenAI/SMTP/etc. values, and set them under Env Variables → Secret. ZCP listed the keys needed in the prior response.\n3. **Establish HTTP exposure (MANDATORY before smoke test)** — pick one:\n   - **Custom domain (recommended for prod)** — Project → Public Access → HTTP Routing → Add Domain in the prod project's dashboard. Use the DNS records ZCP emitted when the launch input carried `customDomain`. Add at the registrar, click Verify in dashboard.\n   - **zerops.app subdomain (explicit opt-in)** — Project → Service → Public Access → Enable Subdomain in the prod project's dashboard. ZCP cannot do this from the source-project MCP session because `zerops_subdomain` is bound to the current project; explicit enable requires either a new MCP session against the prod project (with a project-scoped `ZCP_API_KEY` for that project) or the dashboard click-through.\n   - **No public access** — leave the runtime reachable only via internal hostname for backend / worker services. Skip step 4.\n4. **Smoke test** — hit the URL from step 3 with a known request shape; check response and logs in dashboard. If step 3 is \"no public access\", skip directly to step 5 (services reachable only via internal hostname from peer services in the same project).\n5. **Pipeline trigger (if launched response had no `pipeline-not-configured-*` blockers)** — push a release tag to deploy: `git tag v1.0.0 \u0026\u0026 git push --tags` (matching the integration's tag regex, default `^v\\d+\\.\\d+\\.\\d+$`). If the launched response carried such blockers, configure each runtime via Zerops dashboard first using the deep-link the blocker provides.\n\nAfter step 5 passes, the launch is complete. For ongoing prod iteration: generate a separate project-scoped `ZCP_API_KEY` (Custom access per project, this one project, Full access) and configure a fresh ZCP MCP session against the production project.","blockers":[{"id":"pipeline-not-configured-app","severity":"warn","category":"other","message":"Runtime \"app\" has no CD pipeline integration. Configure in Zerops dashboard. Deep-link: https://app.zerops.io/service-stack/H5t8DsRTTZ6mJzlIotY15g/deploy Recommended: repositoryFullName=krls2020/eval2 eventType=TAG tagRegex=^v\\d+\\.\\d+\\.\\d+$ zeropsYamlSetup=prod"}],"inputs":{"productionProjectName":"zcp-eval-prod"},"productionProjectId":"7Ss2VHCwSnAFkYgC5vBZUw","warnings":["prod policy: app minContainers 1→2 (HA floor)","prod policy: app cpuMode SHARED→DEDICATED"]}
```

---

## `workflow:bootstrap:status::prose`
scenario=adopt-existing-standard-pair | bytes=5453 | input={"action": "status", "workflow": "bootstrap"}

```json
## Status
Phase: idle
Services: appdev, appstage, db
  - appdev (ubuntu/nodejs@22) — bootstrapped=true, mode=standard, closeMode=unset, gitPush=unconfigured, buildIntegration=none, stage=appstage, deployed=true
  - appstage (alpine/nodejs@22) — bootstrapped=true, mode=stage, closeMode=unset, gitPush=unconfigured, buildIntegration=none, deployed=true
  - db (postgresql:single@18) — managed
Guidance:
  ### Bootstrap is two-phase

  First call returns `kind: "route-menu"` listing `routeOptions[]` — no
  session is open yet. Second call opens the session and returns
  `kind: "session-active"`. Read `kind` on every response.

  ```
  zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"
     → kind="route-menu", routeOptions=[...]
  zerops_workflow action="start" workflow="bootstrap" route="<picked>" \
     recipeSlug="<slug>"   # if route="recipe"
     sessionId="<id>"      # if route="resume"
     → kind="session-active"
  ```

  ### Coexistence with existing services

  Existing services in the project (bootstrapped or not) are **independent**.
  Bootstrap creates **new** services alongside them — it does not modify,
  re-import, or replace existing ones. To add a service to a project that
  already has some, pick a non-colliding hostname and bootstrap normally.
  `zerops_import override=true` is destructive and reserved for explicit
  user requests (config change on a known service), never the default
  path when bootstrap surfaces a hostname collision.

  ### Ranked options

  | Route | Present when | Carries | Dispatch / rule |
  |---|---|---|---|
  | `resume` | Snapshot has `resumable: true` | `resumeSession`, `resumeServices` | Pick first unless intentionally overriding: `route="resume" sessionId="<resumeSession>"`. |
  | `adopt` | Runtime services lack bootstrap records (`not bootstrapped`) | `adoptServices[]` | Attach ZCP tracking to running services — no infra change. Use when the user's intent matches the listed `adoptServices[]`. To add NEW services alongside (instead of adopting these), use `classic`. |
  | `recipe` | Up to three recipe matches | `recipeSlug`, `confidence`, `collisions[]` | `route="recipe" recipeSlug="<value from routeOptions[].recipeSlug>"`. Copy the slug verbatim from the discover response — corpus slugs don't carry a `zerops-` prefix even when users name a recipe by its branded form (`"zerops-laravel-minimal"`). Collisions recover by runtime rename or same-type managed `resolution: EXISTS`; switch routes only for different-type managed collision or independent infra. |
  | `classic` | Always available | none | `route="classic"` for manual planning. Default path for creating new services in any project state — fresh project or alongside existing ones. |

  ### Explicit overrides

  Explicit `route` on the first call bypasses discovery. Use only after
  prior discovery or direct user route choice. Valid values:
  `adopt`, `recipe`, `classic`, `resume`. Empty route re-enters discovery.

  ### Collision semantics

  `collisions[]` annotates recipe options; enforcement happens at plan
  submission. Pre-plan hostnames: rename runtimes or set managed deps to
  `EXISTS` before submitting.
  The project has at least one bootstrapped service ready to receive
  code. Start a develop session:

  ```
  zerops_workflow action="start" workflow="develop" intent="{task-description}" scope=["appdev",…]
  ```

  The envelope will flip to `phase: develop-active`; subsequent status
  calls show `workSession.deploys[]` and `workSession.verifies[]` as
  you iterate. Once the develop session is active, auto-close semantics
  land in the develop response.

  **To add a NEW service to this project** — run bootstrap workflow
  again with a non-colliding hostname (`route="classic"` or
  `route="recipe"`). The new service exists alongside the bootstrapped
  ones; bootstrap never modifies or replaces existing services.
  The project has bootstrapped services with at least one successful deploy — a legitimate candidate for promotion to a SEPARATE production Zerops project. When the user's intent is "go live", "deploy to prod", "launch production", "promote to prod", or the Czech equivalents ("nasaď to na prod", "udělej produkční projekt"), use the launch-production workflow rather than running `zcli project create` or hand-writing an import.yaml:

  ```
  zerops_workflow action="start" workflow="launch-production" intent="<one-line>" targetService="<dev-hostname>"
  ```

  The workflow handles bundle composition (managed deps promoted to HA, production scaling tier), source-control mutation (appending `setup: prod` block to `zerops.yaml`), a one-shot launch-window token with project-creation permission (validated, never persisted), and a post-launch checklist (delete the key, attach domain). Multi-call narrowing: `scope-prompt` → `classify-prompt` → `ready-to-launch` → `launching` → `configuring-pipeline` → `launched`.

  For standard-mode dev/stage pairs, pass the dev-half hostname as `targetService` (stage-half input fires a corrective scope-prompt blocker). Continue developing the existing services through the develop entry instead when the user's intent is iteration, not promotion.
Next:
  ▸ Primary: Start a develop task — zerops_workflow action="start" intent="..." workflow="develop"
  · Alternatives:
      - Add more services — zerops_workflow action="start" workflow="bootstrap"

```

---

## `env:get`
scenario=kanban-laravel-minimal-standard-pair | bytes=1888 | input={"action": "get"}

```json
{"service":{"hostname":"appdev","serviceId":"Za7OM1WISSih6iq8E4N3ag","type":"alpine/php-nginx@8.4","status":"ACTIVE"},"envs":[{"key":"ZEROPS_DEBUG_BUILD","value":"NONE"},{"key":"appVersionName","value":"main"},{"key":"serviceId","value":"Za7OM1WISSih6iq8E4N3ag"},{"key":"projectId","value":"2Biyb7d2TQeSum9HNtjLQQ"},{"isPlatformInjected":true,"key":"zeropsSubdomain","value":"https://appdev-2445.prg1.zerops.app"},{"key":"PATH","value":"/home/zerops/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/zerops/bin:/var/www/node_modules/.bin:/usr/local/go/bin:/home/zerops/.cargo/bin"},{"key":"appVersionId","value":"WbLRxhtJTViYidiPh0rxiw"},{"key":"ZEROPS_DEBUG_RUNTIME_PREPARE","value":"NONE"},{"key":"hostname","value":"appdev"},{"key":"documentRoot","value":"/var/www/public"},{"key":"LOG_SYSLOG_FACILITY","source":"zerops.yaml","value":"local0"},{"isReference":true,"key":"DB_DATABASE","source":"zerops.yaml","value":"${db_dbName}"},{"isReference":true,"key":"DB_PASSWORD","source":"zerops.yaml","value":"${db_password}"},{"key":"LOG_CHANNEL","source":"zerops.yaml","value":"syslog"},{"key":"DB_CONNECTION","source":"zerops.yaml","value":"pgsql"},{"isReference":true,"key":"APP_URL","source":"zerops.yaml","value":"${zeropsSubdomain}"},{"key":"APP_DEBUG","source":"zerops.yaml","value":"true"},{"key":"CACHE_STORE","source":"zerops.yaml","value":"database"},{"isReference":true,"key":"DB_HOST","source":"zerops.yaml","value":"${db_hostname}"},{"isReference":true,"key":"DB_USERNAME","source":"zerops.yaml","value":"${db_user}"},{"key":"SESSION_DRIVER","source":"zerops.yaml","value":"database"},{"key":"APP_ENV","source":"zerops.yaml","value":"local"},{"key":"APP_NAME","source":"zerops.yaml","value":"Laravel Zerops"},{"key":"LOG_LEVEL","source":"zerops.yaml","value":"debug"},{"isReference":true,"key":"DB_PORT","source":"zerops.yaml","value":"${db_port}"}]}
```

---

## `env:set`
scenario=kanban-laravel-minimal-dev-only | bytes=537 | input={"action": "set"}

```json
{"process":{"id":"oYBj9rKwTiyuqweYzS321w","actionName":"stack.updateProjectEnvs","status":"FINISHED","created":"2026-06-05T02:30:31.053Z","started":"2026-06-05T02:30:31.92Z","finished":"2026-06-05T02:30:32.882Z"},"stored":[{"key":"APP_KEY","value":"DZQ8NORG4wDQ8k.I1vrWValMuqGgdc6-","replaced":true}],"restartSkipped":true,"nextActions":"skipRestart=true — the value lands in the env store (~5-10s) but the RUNNING process keeps its boot env until it restarts. Restart manually (zerops_manage action=restart) or deploy to pick it up."}
```

---

## `logs`
scenario=recover-failed-buildfromgit-missing-dep | bytes=30 | input={}

```json
{"entries":[],"hasMore":false}
```

---

## `workflow:?:git-push-setup::status=configured`
scenario=launch-with-existing-cicd | bytes=1006 | input={"action": "git-push-setup"}

```json
{"gitPushState":"configured","nextStep":"git-push read-auth + wiring verified: GIT_TOKEN authenticates against the remote (read probe), origin synced on /var/www/.git/config, GIT_TOKEN live in container shell. Write/push permission is NOT proven yet — the first push itself verifies it (a divergent-remote or permission error surfaces at deploy, not here). Wire CI (integration=\"actions\" recommended for GitHub; \"webhook\" for GitLab; \"none\" for external CI/CD): zerops_workflow action=\"build-integration\" service=\"appdev\" integration=\"actions|webhook|none\". Then push via: zerops_deploy targetService=\"appdev\" strategy=\"git-push\".","recommendedIntegration":"actions","remoteUrl":"https://github.com/krls2020/eval2","service":"appdev","status":"configured","workSessionState":{"status":"none","note":"No active develop session — deploy not tracked. Start one via zerops_workflow action=\"start\" workflow=\"develop\" intent=\"...\" scope=[...] to pick up auto-close + verify tracking."}}
```

---

## `workflow:?:build-integration::status=needsGitPushSetup`
scenario=launch-production-pipeline-not-configured | bytes=508 | input={"action": "build-integration"}

```json
{"nextStep":"Run zerops_workflow action=\"git-push-setup\" service=\"appdev\" first; then re-run this build-integration call.","reason":"Build integration \"webhook\" requires git-push capability (current state: unconfigured).","service":"appdev","status":"needsGitPushSetup","workSessionState":{"status":"none","note":"No active develop session — deploy not tracked. Start one via zerops_workflow action=\"start\" workflow=\"develop\" intent=\"...\" scope=[...] to pick up auto-close + verify tracking."}}
```

---

## `workflow:launch-production:start::status=ready-to-launch`
scenario=launch-with-existing-cicd | bytes=2310 | input={"action": "start", "workflow": "launch-production"}

```json
{"workflow":"launch-production","status":"ready-to-launch","phase":"launch-production-active","guidance":"### One-shot API key required for publish\n\n**Note**: this guidance applies to the **NEW-PROJECT** launch path only. If you're deploying into an existing prod project (the user supplied `existingProjectId` + `existingProdToken` at the scope-prompt step), you'll have advanced past this point — the workflow uses the project-scoped token instead and goes straight to `launching`. See the scope-prompt's path-selection table for which params trigger which path.\n\nZCP cannot create a NEW production project with its standing token (project-scoped, no project-creation permission). Walk the user through generating a temporary launch-window token — and wait for them to paste the value back before calling the workflow again:\n\n1. Open [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management).\n2. Click **Create token**. Name it `zcp-launch-\u003cproduction-project-name\u003e`.\n3. Under **Primary Access**, select **Custom access per project**.\n4. Turn ON the **Allow creating projects** toggle that appears below — this is the gate that lets the token create the new prod project. Without it, the launch call will fail at create-project.\n5. Leave **Per Project Access Customization** empty — the launch-window token only needs project-creation; it does not need read/write access to any existing project.\n6. Copy the token value (shown once).\n7. Paste the value back into the conversation.\n\nWhen the value lands, re-call the launch workflow with the publish action and the token value passed as `launchKey`. Do NOT invent or guess a value, and do NOT proceed without it — the key is the gate.\n\nThe key flows through the workflow handler only — never persisted to state, logs, or transcripts. Once the launch reaches `launched` status, ZCP returns a mandatory checklist that includes **deleting the key** at the same dashboard URL.","inputs":{"productionProjectName":"zcp-eval-prod"},"sourceContext":{"sourceProjectName":"zcp-eval","suggestedTargetName":"zcp-eval-prod","availableRuntimes":[{"hostname":"appstage","type":"alpine/nodejs@22","mode":"standard","devHostname":"appdev"}],"promotionHeadline":"appstage","targetServiceCanonical":"appdev"}}
```

---

## `workflow:?:iterate::session-active`
scenario=discover-adoption-state-resumable-uses-sessionid | bytes=1144 | input={"action": "iterate"}

```json
{"kind":"session-active","sessionId":"sess-stale-mid-bootstrap-2026-05-27","intent":"","progress":{"total":3,"completed":1,"steps":[{"name":"discover","status":"complete"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"provision","index":1,"tools":["zerops_import","zerops_process","zerops_discover"],"verification":"SUCCESS WHEN: all plan services exist in API with ACTIVE/RUNNING status AND service types match plan AND managed dependency env vars recorded in session state. Runtime services are auto-mounted on completion.","detailedGuide":"STOP — bootstrap retry attempted.\n\nInfrastructure verification does not iterate. Bootstrap owns provisioning and registration only; if the provision step failed, something is wrong with the plan or the Zerops API — rerunning the same step will not fix it.\n\nReport to the user:\n1. The current error (from the CheckResult or recent API responses).\n2. What you attempted in the failing step.\n3. Ask whether to adjust the plan, debug the API failure, or escalate.\n\nDo NOT rerun the step without user input."},"message":"Step 2/3: provision"}
```

---

## `dev_server:restart`
scenario=existing-standard-appdev-only-reminders | bytes=367 | input={"action": "restart"}

```json
{"action":"restart","hostname":"appdev","running":true,"port":3000,"healthPath":"/","healthStatus":200,"startMillis":4427,"logTail":"\n\u003e nodejs-hello-world@1.0.0 dev\n\u003e ts-node src/index.ts\n\nServer running on 0.0.0.0:3000","logFile":"/tmp/zcp-dev-server.log","message":"Dev server on appdev started and responded 200 at http://localhost:3000/ in 4427ms."}
```

---

## `subdomain:enable`
scenario=verify-subdomain-recovery-before-browser | bytes=674 | input={"action": "enable"}

```json
{"process":{"id":"ZdojXY3vQG2ttEkhE1KxWg","actionName":"stack.enableSubdomainAccess","status":"FINISHED","serviceStacks":[{"id":"IHDWraF4SP6XIq1usfuItA","name":"appdev"}],"created":"2026-06-04T23:16:41.797Z","started":"2026-06-04T23:16:41.892Z","finished":"2026-06-04T23:16:42.327Z"},"serviceHostname":"appdev","serviceId":"IHDWraF4SP6XIq1usfuItA","action":"enable","subdomainUrls":["https://appdev-2445.prg1.zerops.app"],"nextActions":"Subdomain active. Verify: zerops_verify.","warnings":["subdomain https://appdev-2445.prg1.zerops.app not HTTP-ready after wait: http not ready on https://appdev-2445.prg1.zerops.app after 10s: HTTP 502 (agent may need to retry verify)"]}
```

---

## `workflow:?:record-deploy::obj`
scenario=recipe-nextjs-ssr-frontend-standard | bytes=571 | input={"action": "record-deploy"}

```json
{"hostname":"app","stamped":true,"firstDeployedAt":"2026-06-05T03:02:43Z","note":"FirstDeployedAt freshly stamped — ServiceSnapshot.Deployed flips to true on next envelope build","subdomainAccessEnabled":true,"subdomainUrl":"https://app-2445-3000.prg1.zerops.app","workSessionState":{"status":"open","progress":{"sessionId":"work-1516832","ready":0,"total":1,"pending":["app"],"autoCloseStatus":"gated","reason":"auto-close gated by close-mode: app. Set close-mode via zerops_workflow action=\"close-mode\" closeMode={...}, or close explicitly via action=\"close\"."}}}
```

---

## `scale`
scenario=greenfield-fullstack-multi-runtime | bytes=393 | input={}

```json
{"process":{"id":"jABOAAwFQNi5QnQu01DBWg","actionName":"stack.updateAutoscaling","status":"FINISHED","serviceStacks":[{"id":"zaKBjw4IQ1qVt6L9ZQuTgA","name":"appdev"}],"created":"2026-06-04T21:11:57.071Z","started":"2026-06-04T21:11:57.118Z","finished":"2026-06-04T21:11:58.071Z"},"serviceHostname":"appdev","serviceId":"zaKBjw4IQ1qVt6L9ZQuTgA","nextActions":"Verify scaling: zerops_discover."}
```

---

## `workflow:?:reset::obj`
scenario=discover-adoption-state-resumable-uses-sessionid | bytes=152 | input={"action": "reset"}

```json
{"cleared":{"bootstrapSessionId":"sess-stale-mid-bootstrap-2026-05-27","completedSteps":1,"incompleteMetas":["appdev"]},"preserved":{"liveServices":11}}
```

---

## `workflow:bootstrap:reset::obj`
scenario=discover-adoption-state-resumable-uses-sessionid | bytes=152 | input={"action": "reset", "workflow": "bootstrap"}

```json
{"cleared":{"bootstrapSessionId":"sess-stale-mid-bootstrap-2026-05-27","completedSteps":1,"incompleteMetas":["appdev"]},"preserved":{"liveServices":10}}
```

---

## `export`
scenario=verify-subdomain-recovery-before-browser | bytes=145 | input={}

```json
services:
  - hostname: appdev
    type: alpine/static@1.0
    verticalAutoscaling:
      minRam: 0.25
    minContainers: 1
    maxContainers: 1

```

---

## `workflow:?:close::prose`
scenario=cross-deploy-stage-promote-from-dev | bytes=20 | input={"action": "close"}

```json
Work session closed.
```

---

## `workflow:develop:close::prose`
scenario=verify-subdomain-recovery-before-browser | bytes=20 | input={"action": "close", "workflow": "develop"}

```json
Work session closed.
```

---

## `workflow:?:list::nondict`
scenario=launch-production-pipeline-configured | bytes=2 | input={"action": "list"}

```json
[]
```

---

## `workflow:bootstrap:start::obj`
scenario=classic-go-simple | bytes=3157 | input={"action": "start", "workflow": "bootstrap"}

```json
{"intent":"Simple Go HTTP service, single container, public subdomain","projectId":"waAzEFn6SBaysG4YE4rv7A","routeOptions":[{"route":"adopt","why":"Adopt existing runtime service `zcp` — has no ZCP metadata.","adoptServices":["zcp"]},{"route":"recipe","why":"Minimal Go HTTP server that connects to a PostgreSQL database, runs an idempotent schema migration on startup, and serves a health check endpoint demonstrating both connectivity and a live query from the database. Used within Go Hello World recipe for Zerops platform.","recipeSlug":"go-hello-world","confidence":0.85,"importYaml":"# AI agent environment provides a development space for AI agents\n# to build and version the app. Comes with a dev service with\n# source code and development tools, a staging service, and a\n# low-resource database.\n\nproject:\n  name: go-hello-world-agent\n\nservices:\n  # Set up the AI agent development environment — Zerops pulls\n  # source from the 'buildFromGit' repo using the 'dev' setup,\n  # which deploys full source code and the Go toolchain.\n  # 'zeropsSetup' selects which setup (prod/dev) from zerops.yaml\n  # in the source repo. SSH in to compile and run the app.\n  - hostname: appdev\n    type: golang@1.22\n    zeropsSetup: dev\n    buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # Staging app — validates the production build pipeline. Zerops\n  # pulls source and zerops.yaml from the 'buildFromGit' repo,\n  # using the 'prod' zeropsSetup to compile binaries and deploy.\n  # Subdomain access provides a public HTTPS URL for testing.\n  - hostname: appstage\n    type: golang@1.22\n    zeropsSetup: prod\n    buildFromGit: https://github.com/zerops-recipe-apps/go-hello-world-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # PostgreSQL for app data. Priority 10 starts data services\n  # before app containers, preventing connection errors on startup.\n  # Accessible as 'db' hostname from 'appdev' and 'appstage'.\n  # NON_HA is a single-node instance — suitable for dev/staging\n  # where HA durability isn't required.\n  - hostname: db\n    type: postgresql@16\n    mode: NON_HA\n    priority: 10\n"},{"route":"classic","why":"Manual plan — user describes services directly, no recipe template."}],"message":"Bootstrap route discovery — pick one by calling zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"\u003croute\u003e\" (recipe requires `recipeSlug`; resume requires existing session via `action=\"resume\"`).\n\nOptions:\n  1. route=\"adopt\" — Adopt existing runtime service `zcp` — has no ZCP metadata.\n  2. route=\"recipe\" recipeSlug=\"go-hello-world\" (confidence 0.85) — Minimal Go HTTP server that connects to a PostgreSQL database, runs an idempotent schema migration on startup, and serves a health check endpoint demonstrating both connectivity and a live query from the database. Used within Go Hello World recipe for Zerops platform.\n  3. route=\"classic\" — Manual plan — user describes services directly, no recipe template.\n"}
```

---

## `workflow:bootstrap/classic:start::error`
scenario=greenfield-node-postgres-dev-stage | bytes=356 | input={"action": "start", "workflow": "bootstrap", "route": "classic"}

```json
{"code":"INVALID_PARAMETER","error":"plan is not accepted in action=start; submit it via action=\"complete\" step=\"discover\" plan=[...]","suggestion":"Start commits the route only. The discover step is the reasoning space where the plan is produced from route-specific materials; commit it there.","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:bootstrap/classic:start::obj`
scenario=classic-static-nginx-simple | bytes=7530 | input={"action": "start", "workflow": "bootstrap", "route": "classic"}

```json
{"sessionId":"e91a461840ded07f","intent":"Static HTML landing page served by nginx","progress":{"total":3,"completed":0,"steps":[{"name":"discover","status":"in_progress"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"discover","index":0,"tools":["zerops_discover","zerops_knowledge","zerops_workflow"],"verification":"SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).","detailedGuide":"Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.\n\nThree routes:\n\n- **Recipe** — services come from a matched recipe's import YAML.\n- **Classic** — agent constructs the import YAML from the user's intent.\n- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.\n\nRoute is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action=\"status\"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)\n\n---\n\n### Dynamic runtime plan\n\nIf the plan you're about to submit includes a dynamic runtime (Node, Go, Python, Bun, Ruby, …), apply this section. Classic bootstrap creates the runtime + managed services with `startWithoutCode: true` so dev containers reach RUNNING with an empty filesystem; `workflow=develop` then scaffolds `zerops.yaml`, writes the application, and runs the first deploy.\n\nConfirm dev/stage pairing with the user before submitting the plan. Mode + close-mode + git-push capability decisions all happen later in develop, not here.\n\n---\n\n### Static runtime plan\n\nIf the plan you're about to submit includes a static-runtime container (`nginx`, `static`), apply this section. Static-runtime containers come up serving an empty document root after bootstrap. The first build artifact lands in develop via `zerops_deploy`; bootstrap creates the empty container and stops there.\n\nBefore submitting the plan, confirm with the user:\n\n- the chosen runtime hostname (`appdev` is the standard convention)\n- whether a stage pair is wanted (`standard` mode) or a single container (`simple` / `dev` mode)\n\nClose-mode, git-push capability, and the actual `zerops.yaml` (including `deployFiles` shape) are decided in develop after the first deploy lands — not here.\n\n---\n\n### Read `apiMeta` on every error response\n\nAny `zerops_*` tool surfacing a Zerops API 4xx may include `apiMeta`.\nMissing key = no server detail; present key = exact rejected fields.\n\nShape:\n\n```json\n{\n  \"code\": \"API_ERROR\",\n  \"apiCode\": \"projectImportInvalidParameter\",\n  \"error\": \"Invalid parameter provided.\",\n  \"suggestion\": \"Zerops flagged specific fields — see apiMeta for each field's failure reason.\",\n  \"apiMeta\": [\n    {\n      \"code\": \"projectImportInvalidParameter\",\n      \"error\": \"Invalid parameter provided.\",\n      \"metadata\": {\n        \"storage.mode\": [\"mode not supported\"]\n      }\n    }\n  ]\n}\n```\n\nEach `apiMeta[].metadata` key is a **field path** (`\u003chost\u003e.mode`,\n`build.base`, `parameter`); values list reasons. Fix those YAML fields\nand retry — do not guess.\n\nCommon `apiCode` shapes:\n\n| `apiCode` | `metadata` key | Meaning |\n|---|---|---|\n| `projectImportInvalidParameter` | `\u003chost\u003e.mode` | type/mode combination not allowed |\n| `projectImportMissingParameter` | `parameter` (value `\u003chost\u003e.mode`) | required field missing |\n| `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |\n| `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |\n| `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |\n\nPer-service import failures use `serviceErrors[].meta` with the same\nshape, one entry per failing service-stack.\n\n---\n\n### Confirm mode per service\n\nEvery runtime service needs a **mode**; confirm with the user before\nsubmitting the plan.\n\n- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.\n  Best for active iteration.\n- **standard** — dev + stage pair. The envelope reports `stageHostname`\n  on the dev snapshot and a separate snapshot with `mode: stage` for\n  the stage service.\n  - **Plan MUST set `stageHostname` explicitly on every standard target**\n    (e.g. `{\"runtime\": {\"devHostname\": \"appdev\", \"type\": \"...\", \"bootstrapMode\": \"standard\", \"stageHostname\": \"appstage\"}}`).\n    Hostname-suffix derivation (`appdev` → `appstage`) was removed in\n    Release B.4. A submission omitting `stageHostname` rejects with an\n    actionable error pointing back to `bootstrapMode=\"dev\"` if a single\n    container was the actual intent.\n- **simple** — single runtime container that starts real code on every redeploy;\n  no SSHFS mutation lifecycle.\n- **stage** — never bootstrapped alone; it is the stage half of a\n  standard pair.\n\nDefault to **dev** for services under active iteration, **simple** for\nimmutable workers. The plan commits the mode when you submit it; after\nbootstrap closes, the envelope exposes the chosen mode as\n`ServiceSnapshot.Mode`. Changing mode later requires a mode-expansion\nbootstrap session, surfaced in develop when actionable.\n\n---\n\n### Runtime classes\n\nEach runtime type falls into one of four classes — pick the right class for each runtime in the plan:\n\n- **Dynamic** (nodejs, go, python, bun, ruby, …) — needs an explicit dev-server lifecycle in develop (container: `zerops_dev_server`; local: harness background task).\n- **Static** (nginx, static) — serves files from `deployFiles`; platform auto-starts after deploy.\n- **Implicit-webserver** (php-apache, php-nginx) — webserver is part of the runtime; platform auto-starts after deploy.\n- **Managed** (postgresql, mariadb, redis/valkey, keydb, rabbitmq, nats, object storage) — no deploy; scale and connect only.\n\nPick runtime types from the live Zerops catalog (check `zerops_knowledge` for current versions). Managed services initialize first (`priority: 10` in import YAML) so runtimes that depend on them can connect at start.\n\nLifecycle and `zerops.yaml` mechanics for each class (start commands, healthCheck, deployFiles, dev-server primitives) are delivered by the develop response at first-deploy time."},"message":"Step 1/3: discover","availableStacks":"## Available Service Stacks (live)\nRuntime: docker@26.1 | runtime | go@1 | nginx@1.22 | static | java@{17,21} | bun@{canary,nightly,1.1.34,1.2,1.3} | deno@{1,2} | elixir@1.16 | gleam@1.5 | nodejs@{18,20,22,24} | python@{3.11,3.12,3.14} | php-apache@{8.1,8.3,8.4,8.5} | php-nginx@{8.1,8.3,8.4,8.5} | ubuntu@{22.04,24.04} | alpine@{3.17,3.18,3.19,3.20,3.21,3.22,3.23} | dotnet@{10,6,7,8,9} | rust@{nightly,stable} | ruby@{3.2,3.3,3.4} | zcp@1\nManaged: mariadb@10.6 | postgresql@{14,16,17,18} | keydb@6 | valkey@7.2 | qdrant@{1.10,1.12} | nats@{2.10,2.12} | kafka@3.9 | elasticsearch@{8.16,9.2} | typesense@{27.1,30.2} | meilisearch@{1.10,1.20} | clickhouse@25.3\nShared storage: shared-storage\nObject storage: object-storage\n"}
```

---

## `workflow:?:complete::obj`
scenario=classic-python-postgres-dev-only | bytes=6226 | input={"action": "complete", "step": "provision"}

```json
{"sessionId":"7c6bf7e702ea0ec0","intent":"","progress":{"total":3,"completed":2,"steps":[{"name":"discover","status":"complete"},{"name":"provision","status":"complete"},{"name":"close","status":"in_progress"}]},"current":{"name":"close","index":2,"tools":["zerops_workflow"],"verification":"SUCCESS WHEN: bootstrap administratively closed (metas written, transition to develop presented).","detailedGuide":"### Read `apiMeta` on every error response\n\nAny `zerops_*` tool surfacing a Zerops API 4xx may include `apiMeta`.\nMissing key = no server detail; present key = exact rejected fields.\n\nShape:\n\n```json\n{\n  \"code\": \"API_ERROR\",\n  \"apiCode\": \"projectImportInvalidParameter\",\n  \"error\": \"Invalid parameter provided.\",\n  \"suggestion\": \"Zerops flagged specific fields — see apiMeta for each field's failure reason.\",\n  \"apiMeta\": [\n    {\n      \"code\": \"projectImportInvalidParameter\",\n      \"error\": \"Invalid parameter provided.\",\n      \"metadata\": {\n        \"storage.mode\": [\"mode not supported\"]\n      }\n    }\n  ]\n}\n```\n\nEach `apiMeta[].metadata` key is a **field path** (`\u003chost\u003e.mode`,\n`build.base`, `parameter`); values list reasons. Fix those YAML fields\nand retry — do not guess.\n\nCommon `apiCode` shapes:\n\n| `apiCode` | `metadata` key | Meaning |\n|---|---|---|\n| `projectImportInvalidParameter` | `\u003chost\u003e.mode` | type/mode combination not allowed |\n| `projectImportMissingParameter` | `parameter` (value `\u003chost\u003e.mode`) | required field missing |\n| `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |\n| `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |\n| `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |\n\nPer-service import failures use `serviceErrors[].meta` with the same\nshape, one entry per failing service-stack.\n\n---\n\n### Verify infrastructure before closing bootstrap\n\nBootstrap is infra-only: no code, no deploy, no HTTP probe. Close must\nconfirm the **platform layer** is healthy before develop starts.\n\n```\nzerops_discover\n```\n\nRequired state for every planned service:\n\n- Platform `status` = `RUNNING` for managed services (databases, caches,\n  object storage). A managed service that never reached `RUNNING` means\n  the import failed silently — investigate `zerops_process` logs, do\n  not close.\n- Runtime services may appear as `NOT_YET_DEPLOYED` — that is expected.\n  Code and the first deploy happen in the develop workflow.\n- Env vars discovered during provisioning must be recorded in the\n  session so develop can wire them without re-discovering.\n\nDo **not** run `zerops_verify` here — that tool probes the app layer\n(HTTP reachability, `/status` endpoints) which only makes sense **after**\ndevelop writes code and runs the first deploy. Running it during\nbootstrap will report every runtime as failing and is noise.\n\nIf a managed service is stuck in a non-`RUNNING` state, bootstrap\nhard-stops: surface the failure to the user rather than retrying —\ninfrastructure issues require the user's judgment.\n\n---\n\n### Closing bootstrap\n\nBootstrap is **infrastructure-only**. After\n`action=\"complete\" step=\"close\"`, planned runtimes show\n`bootstrapped: true`: managed services are `RUNNING`, runtimes are\nregistered, dev containers are SSH-mount-ready, and managed env vars\nare discoverable. Classic and recipe-with-first-deploy-later services\nshow `deployed: false` and enter develop's first-deploy branch. Adopted\nservices and recipes that deployed during bootstrap show `deployed: true`.\n\nNo application code is written, no `zerops.yaml` generated, and no\ndeploy runs as part of bootstrap close itself.\n\n**Next step — `zerops_workflow action=\"start\" workflow=\"develop\"`.** Develop owns code, the first deploy, verify, iteration, and close-mode setup. Services with `deployed: false` enter the first-deploy branch on develop entry.\n\nDirect tools (`zerops_scale`, `zerops_env`, `zerops_subdomain`, `zerops_discover`) stay callable without a workflow wrapper for one-shot infra changes.\n\nComplete this step before starting develop.\n\n---\n\n## Discovered Managed-Service Env Var Catalog\n\nRecorded at provision via `zerops_discover includeEnvs=true`. **These are the authoritative names** — do not guess alternative spellings; unknown cross-service references resolve to literal strings at runtime and fail silently.\n\n| Service | Keys | Cross-service reference shape |\n|---|---|---|\n| `db` | connectionString, serviceId, BACKUP_PERIOD, dbName, connectionTlsString, port, ZEROPS_PROMETHEUS_PORT, projectId, superUser, hostname, superUserPassword, user, portTls, password | `${db_connectionString}` `${db_serviceId}` `${db_BACKUP_PERIOD}` `${db_dbName}` `${db_connectionTlsString}` `${db_port}` `${db_ZEROPS_PROMETHEUS_PORT}` `${db_projectId}` `${db_superUser}` `${db_hostname}` `${db_superUserPassword}` `${db_user}` `${db_portTls}` `${db_password}` |\n\n**Usage**: reference these in `run.envVariables` of your app's zerops.yaml. They resolve at deploy time — they are NOT active as OS env vars on a dev container that was started with `startWithoutCode: true`.\n","priorContext":{"plan":{"targets":[{"runtime":{"devHostname":"appdev","type":"python@3.12","bootstrapMode":"dev"},"dependencies":[{"hostname":"db","type":"postgresql@18","mode":"NON_HA","resolution":"CREATE"}]}],"createdAt":"2026-05-03T21:21:07Z"},"attestations":{"discover":"[complete: Planned targets: appdev (python@3.12), db (postgresql@18, NON_HA)]","provision":"All services provisioned and ACTIVE. db (postgresql@18 NON_HA): env keys include hostname, port, user, password, dbName, connectionString. appdev (python@3.12): ACTIVE with subdomain enabled, SSHFS-mountable."}},"planMode":"dev"},"message":"Step 3/3: close","checkResult":{"passed":true,"checks":[{"name":"appdev_status","status":"pass"},{"name":"db_status","status":"pass"},{"name":"db_env_vars","status":"pass","detail":"14 env vars"}],"summary":"all services provisioned"},"autoMounts":[{"hostname":"appdev","mountPath":"/var/www/appdev","status":"MOUNTED"}]}
```

---

## `dev_server:stop`
scenario=cadence-multiservice-spec-run2-replay | bytes=162 | input={"action": "stop"}

```json
{"action":"stop","hostname":"appdev","running":false,"port":3000,"message":"Dev server stopped on appdev (matched \"\"). Port 3000 is free (verified after 0ms)."}
```

---

## `workflow:?:complete::error`
scenario=adopt-existing-standard-pair | bytes=1307 | input={"action": "complete", "step": "discover"}

```json
{"code":"INVALID_PARAMETER","error":"Adopt plan failed: adopt: ambiguous dev/stage pairing: \"appdev\" and \"appstage\" are both ubuntu/nodejs@22 — likely a dev/stage pair, which ZCP will not guess. Resubmit action=complete step=discover with ONE of these as an explicit plan:\n\n• dev/stage pair (cross-deploy promote — pick this if \"appdev\" deploys to \"appstage\"):\nplan=[{\"runtime\":{\"devHostname\":\"appdev\",\"type\":\"ubuntu/nodejs@22\",\"isExisting\":true,\"bootstrapMode\":\"standard\",\"stageHostname\":\"appstage\"},\"dependencies\":[{\"hostname\":\"db\",\"type\":\"postgresql:single@18\",\"resolution\":\"EXISTS\"}]}]\n\n• two independent dev containers:\nplan=[{\"runtime\":{\"devHostname\":\"appdev\",\"type\":\"ubuntu/nodejs@22\",\"isExisting\":true,\"bootstrapMode\":\"dev\"},\"dependencies\":[{\"hostname\":\"db\",\"type\":\"postgresql:single@18\",\"resolution\":\"EXISTS\"}]},{\"runtime\":{\"devHostname\":\"appstage\",\"type\":\"ubuntu/nodejs@22\",\"isExisting\":true,\"bootstrapMode\":\"dev\"},\"dependencies\":[{\"hostname\":\"db\",\"type\":\"postgresql:single@18\",\"resolution\":\"EXISTS\"}]}]","suggestion":"Omit plan and pass scope=[\"hostname\",...] to adopt exactly those services, or submit an explicit plan.","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:bootstrap/recipe:start::obj`
scenario=recipe-laravel-minimal-standard | bytes=7820 | input={"action": "start", "workflow": "bootstrap", "route": "recipe", "recipeSlug": "laravel-minimal"}

```json
{"sessionId":"84b780a7d29c6b44","intent":"","progress":{"total":3,"completed":0,"steps":[{"name":"discover","status":"in_progress"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"discover","index":0,"tools":["zerops_discover","zerops_knowledge","zerops_workflow"],"verification":"SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).","detailedGuide":"Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.\n\nThree routes:\n\n- **Recipe** — services come from a matched recipe's import YAML.\n- **Classic** — agent constructs the import YAML from the user's intent.\n- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.\n\nRoute is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action=\"status\"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)\n\n---\n\n### Field mutability (change an immutable → `route=\"classic\"`)\n\n| Mutable | Immutable |\n|---|---|\n| Runtime `hostname` via `devHostname`/`stageHostname` | `type`, `zeropsSetup`, `buildFromGit`, `priority`, `mode`, autoscaling, env vars |\n| Managed `resolution` (CREATE ↔ EXISTS) | Managed `hostname` — repo's `${hostname_*}` refs break on rename |\n\n### Plan shape (no collisions)\n\nPer runtime pair: `devHostname`/`stageHostname` from recipe's `zeropsSetup: dev`/`prod` services; `type` + `bootstrapMode` verbatim (mode from banner); `dependencies[]` hostname+type verbatim with `resolution: \"CREATE\"`; `isExisting: false`.\n\n### Collision recovery (route option has `collisions: [...]`)\n\n- **Runtime** → non-colliding `devHostname`/`stageHostname`; ZCP rewrites YAML at provision.\n- **Managed, same type** → `resolution: \"EXISTS\"`, keep recipe's hostname. Entry drops from YAML; existing service reused via `${hostname_*}`.\n- **Managed, different type** → `route=\"classic\"`.\n\nUnrecovered collision → plan rejected.\n\nDo not write code — `buildFromGit` pulls the app repo at import.\n\n---\n\n### Read `apiMeta` on every error response\n\nAny `zerops_*` tool surfacing a Zerops API 4xx may include `apiMeta`.\nMissing key = no server detail; present key = exact rejected fields.\n\nShape:\n\n```json\n{\n  \"code\": \"API_ERROR\",\n  \"apiCode\": \"projectImportInvalidParameter\",\n  \"error\": \"Invalid parameter provided.\",\n  \"suggestion\": \"Zerops flagged specific fields — see apiMeta for each field's failure reason.\",\n  \"apiMeta\": [\n    {\n      \"code\": \"projectImportInvalidParameter\",\n      \"error\": \"Invalid parameter provided.\",\n      \"metadata\": {\n        \"storage.mode\": [\"mode not supported\"]\n      }\n    }\n  ]\n}\n```\n\nEach `apiMeta[].metadata` key is a **field path** (`\u003chost\u003e.mode`,\n`build.base`, `parameter`); values list reasons. Fix those YAML fields\nand retry — do not guess.\n\nCommon `apiCode` shapes:\n\n| `apiCode` | `metadata` key | Meaning |\n|---|---|---|\n| `projectImportInvalidParameter` | `\u003chost\u003e.mode` | type/mode combination not allowed |\n| `projectImportMissingParameter` | `parameter` (value `\u003chost\u003e.mode`) | required field missing |\n| `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |\n| `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |\n| `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |\n\nPer-service import failures use `serviceErrors[].meta` with the same\nshape, one entry per failing service-stack.\n\n---\n\n## Recipe import YAML — \"laravel-minimal\" (mode: standard)\n\nThis recipe is **standard mode**. Every runtime target in your plan must carry `bootstrapMode: \"standard\"` verbatim — deviating strips mode-specific provisioning rules (e.g. `startWithoutCode`) and fails plan validation.\n\nThis is the canonical project-import YAML for the matched recipe. It is authoritative — do NOT rewrite services or adjust fields unless the user explicitly asks.\n\nSteps:\n\n1. Read the YAML below. If it contains a `project:` block with `envVariables`, set those at the project level FIRST using `zerops_env action=\"set\" scope=\"project\" ...`.\n2. Call `zerops_import` with the `services:` section ONLY — the import tool rejects YAML that includes `project:`.\n3. Poll `zerops_discover` until every service reports `ACTIVE`. Recipes build from `buildFromGit` URLs, so first provision can take 2–5 minutes while Zerops clones and builds.\n\n```yaml\n#zeropsPreprocessor=on\n\n# AI agent environment provides a development space for AI agents to build and\n# version the app.\n# It includes a dev service with the code repository and necessary development\n# tools, a staging service, and a low-resource database.\n\n# APP_KEY is Laravel's AES-256-CBC encryption key (32 random bytes).\n# Project-level so session cookies and encrypted database columns remain valid\n# when the L7 balancer routes requests to any app container in the project.\nproject:\n  name: laravel-minimal-agent\n  envVariables:\n    APP_KEY: \u003c@generateRandomString(\u003c32\u003e)\u003e\n\nservices:\n  # AI agent workspace — zeropsSetup:dev deploys the full source tree so the\n  # agent can SSH in and edit PHP files over SSHFS. PHP-FPM reinterprets each\n  # request, no restart needed. Subdomain gives the agent a URL to verify output\n  # against.\n  - hostname: appdev\n    type: php-nginx@8.4\n    zeropsSetup: dev\n    buildFromGit: https://github.com/zerops-recipe-apps/laravel-minimal-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # Staging slot for AI agents — zeropsSetup:prod validates the production\n  # build pipeline (composer install --no-dev, Vite asset compilation,\n  # config:cache + route:cache + view:cache in initCommands) before the agent\n  # marks the task complete.\n  - hostname: appstage\n    type: php-nginx@8.4\n    zeropsSetup: prod\n    buildFromGit: https://github.com/zerops-recipe-apps/laravel-minimal-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # PostgreSQL backing store for schema, sessions, cache, and queued jobs —\n  # all Laravel drivers default to 'database' in the minimal tier. Shared by\n  # appdev and appstage. NON_HA is fine for agent workspaces; priority 10\n  # ensures db is ready before the app containers start.\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n    mode: NON_HA\n    verticalAutoscaling:\n      minRam: 0.25\n\n```\n"},"message":"Step 1/3: discover","availableStacks":"## Available Service Stacks (live)\nRuntime: docker@26.1 | runtime | go@1 | nginx@1.22 | static | java@{17,21} | bun@{canary,nightly,1.1.34,1.2,1.3} | deno@{1,2} | elixir@1.16 | gleam@1.5 | nodejs@{18,20,22,24} | python@{3.11,3.12,3.14} | php-apache@{8.1,8.3,8.4,8.5} | php-nginx@{8.1,8.3,8.4,8.5} | ubuntu@{22.04,24.04} | alpine@{3.17,3.18,3.19,3.20,3.21,3.22,3.23} | dotnet@{10,6,7,8,9} | rust@{nightly,stable} | ruby@{3.2,3.3,3.4} | zcp@1\nManaged: mariadb@10.6 | postgresql@{14,16,17,18} | keydb@6 | valkey@7.2 | qdrant@{1.10,1.12} | nats@{2.10,2.12} | kafka@3.9 | elasticsearch@{8.16,9.2} | typesense@{27.1,30.2} | meilisearch@{1.10,1.20} | clickhouse@25.3\nShared storage: shared-storage\nObject storage: object-storage\n"}
```

---

## `dev_server:logs`
scenario=recipe-nextjs-ssr-frontend-standard | bytes=811 | input={"action": "logs"}

```json
{"action":"logs","hostname":"appdev","running":false,"logTail":"\n\u003e nextjs-ssr-zerops@0.1.0 dev\n\u003e next dev --hostname 0.0.0.0 --port 3000\n\n   ▲ Next.js 15.5.15\n   - Local:        http://localhost:3000\n   - Network:      http://0.0.0.0:3000\n\n ✓ Starting...\nAttention: Next.js now collects completely anonymous telemetry regarding usage.\nThis information is used to shape Next.js' roadmap and prioritize features.\nYou can learn more, including how to opt-out if you'd not like to participate in this anonymous program, by visiting the following URL:\nhttps://nextjs.org/telemetry\n\n   Downloading swc package @next/swc-wasm-nodejs... to /home/zerops/.cache/next-swc\n\u001b[?25h","logFile":"/tmp/zcp-dev-server.log","message":"Tailing last 60 lines of /tmp/zcp-dev-server.log on appdev."}
```

---

## `dev_server:status`
scenario=api-node-postgres-classic-dev | bytes=169 | input={"action": "status"}

```json
{"action":"status","hostname":"appdev","running":true,"port":3000,"healthPath":"/status","healthStatus":200,"message":"Dev server on appdev:3000 responding (HTTP 200)."}
```

---

## `workflow:develop:start::error`
scenario=verify-subdomain-recovery-before-browser | bytes=288 | input={"action": "start", "workflow": "develop"}

```json
{"code":"ADOPT_REQUIRED","error":"No bootstrapped services found","suggestion":"Run bootstrap first: action=\"start\" workflow=\"bootstrap\" (route=\"adopt\" if services already live)","recovery":{"tool":"zerops_workflow","args":{"action":"start","route":"adopt","workflow":"bootstrap"}}}
```

---

## `workflow:bootstrap/adopt:start::obj`
scenario=existing-standard-appdev-only-reminders | bytes=4405 | input={"action": "start", "workflow": "bootstrap", "route": "adopt"}

```json
{"sessionId":"d1e4a5f93abc061e","intent":"Adopt existing appdev/appstage nodejs@22 services with postgresql db","progress":{"total":3,"completed":0,"steps":[{"name":"discover","status":"in_progress"},{"name":"provision","status":"pending"},{"name":"close","status":"pending"}]},"current":{"name":"discover","index":0,"tools":["zerops_discover","zerops_knowledge","zerops_workflow"],"verification":"SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets (hostnames, types, resolution, modes validated against live catalog).","detailedGuide":"Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.\n\nThree routes:\n\n- **Recipe** — services come from a matched recipe's import YAML.\n- **Classic** — agent constructs the import YAML from the user's intent.\n- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.\n\nRoute is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action=\"status\"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)\n\n---\n\n### Adopting existing services\n\nAdoption attaches ZCP tracking to an existing runtime service without touching its code, configuration, or scale. After adopt close, the envelope reports each adopted hostname with `bootstrapped: true` and an empty close-mode / git-push capability — populated later when the develop session needs them.\n\nList what's there:\n\n```\nzerops_discover\n```\n\nRead every user (non-system, non-managed) service. For each, note:\n\n- the hostname (keep verbatim; do not rename)\n- the runtime type (`ServiceStackTypeVersionName`)\n- whether ports are exposed (dynamic/implicit-web vs static)\n\n---\n\n### Read `apiMeta` on every error response\n\nAny `zerops_*` tool surfacing a Zerops API 4xx may include `apiMeta`.\nMissing key = no server detail; present key = exact rejected fields.\n\nShape:\n\n```json\n{\n  \"code\": \"API_ERROR\",\n  \"apiCode\": \"projectImportInvalidParameter\",\n  \"error\": \"Invalid parameter provided.\",\n  \"suggestion\": \"Zerops flagged specific fields — see apiMeta for each field's failure reason.\",\n  \"apiMeta\": [\n    {\n      \"code\": \"projectImportInvalidParameter\",\n      \"error\": \"Invalid parameter provided.\",\n      \"metadata\": {\n        \"storage.mode\": [\"mode not supported\"]\n      }\n    }\n  ]\n}\n```\n\nEach `apiMeta[].metadata` key is a **field path** (`\u003chost\u003e.mode`,\n`build.base`, `parameter`); values list reasons. Fix those YAML fields\nand retry — do not guess.\n\nCommon `apiCode` shapes:\n\n| `apiCode` | `metadata` key | Meaning |\n|---|---|---|\n| `projectImportInvalidParameter` | `\u003chost\u003e.mode` | type/mode combination not allowed |\n| `projectImportMissingParameter` | `parameter` (value `\u003chost\u003e.mode`) | required field missing |\n| `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |\n| `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |\n| `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |\n\nPer-service import failures use `serviceErrors[].meta` with the same\nshape, one entry per failing service-stack."},"message":"Step 1/3: discover","availableStacks":"## Available Service Stacks (live)\nRuntime: docker@26.1 | runtime | go@1 | nginx@1.22 | static | java@{17,21} | bun@{canary,nightly,1.1.34,1.2,1.3} | deno@{1,2} | elixir@1.16 | gleam@1.5 | nodejs@{18,20,22,24} | python@{3.11,3.12,3.14} | php-apache@{8.1,8.3,8.4,8.5} | php-nginx@{8.1,8.3,8.4,8.5} | ubuntu@{22.04,24.04} | alpine@{3.17,3.18,3.19,3.20,3.21,3.22,3.23} | dotnet@{10,6,7,8,9} | rust@{nightly,stable} | ruby@{3.2,3.3,3.4} | zcp@1\nManaged: mariadb@10.6 | postgresql@{14,16,17,18} | keydb@6 | valkey@7.2 | qdrant@{1.10,1.12} | nats@{2.10,2.12} | kafka@3.9 | elasticsearch@{8.16,9.2} | typesense@{27.1,30.2} | meilisearch@{1.10,1.20} | clickhouse@25.3\nShared storage: shared-storage\nObject storage: object-storage\n"}
```

---

## `mount:status`
scenario=cross-deploy-stage-promote-from-dev | bytes=666 | input={"action": "status"}

```json
{"mounts":[{"hostname":"core","mountPath":"/var/www/core","mounted":false},{"hostname":"zcp","mountPath":"/var/www/zcp","mounted":false},{"hostname":"db","mountPath":"/var/www/db","mounted":false},{"hostname":"buildappstagev1777894234","mountPath":"/var/www/buildappstagev1777894234","mounted":false},{"hostname":"buildappdevv1777894234","mountPath":"/var/www/buildappdevv1777894234","mounted":false},{"hostname":"appdev","mountPath":"/var/www/appdev","mounted":false,"pending":true,"message":"Systemd unit exists but FUSE mount is not active. Use unmount to clean up, or mount to recreate."},{"hostname":"appstage","mountPath":"/var/www/appstage","mounted":false}]}
```

---

## `mount:mount`
scenario=recover-failed-buildfromgit-missing-dep | bytes=120 | input={"action": "mount"}

```json
{"status":"MOUNTED","hostname":"api","mountPath":"/var/www/api","writable":true,"message":"Mounted api at /var/www/api"}
```

---

## `workflow:bootstrap:discover::route-menu`
scenario=greenfield-node-postgres-dev-stage | bytes=6768 | input={"action": "discover", "workflow": "bootstrap"}

```json
{"kind":"route-menu","intent":"Team-notes dashboard: Node.js backend with Postgres database. Dev and stage runtime services (standard bootstrapMode).","projectId":"waAzEFn6SBaysG4YE4rv7A","routeOptions":[{"route":"recipe","why":"A minimal NestJS application with a PostgreSQL connection, demonstrating database connectivity, TypeORM migrations, and a health endpoint. Used within NestJS Minimal recipe for Zerops platform.","recipeSlug":"nestjs-minimal","fit":"exact","retrievalScore":0.85,"importYaml":"# AI agent environment provides a development space for AI agents to build and\n# version the app.\n# It includes a dev service with the code repository and necessary development\n# tools, a staging service, and a low-resource database.\n\n# No project-level secrets needed — NestJS minimal uses no encryption keys or\n# CSRF tokens. Database credentials are auto-generated by the managed PostgreSQL\n# service and wired via cross-service references in zerops.yaml.\nproject:\n  name: nestjs-minimal-agent\n\nservices:\n  # AI agent workspace — zeropsSetup:dev deploys the full source tree so the\n  # agent can SSH in, edit TypeScript over SSHFS, and run `npm run start:dev`\n  # for hot-reload. Subdomain provides a public URL for the agent to verify\n  # dashboard output and health endpoints.\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n    buildFromGit: https://github.com/zerops-recipe-apps/nestjs-minimal-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # Staging slot for the agent — cross-deploy with zeropsSetup:prod compiles\n  # TypeScript via `npm run build`, prunes devDependencies, and runs the\n  # compiled JS with `node dist/main.js` to validate the production build before\n  # finishing the task.\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n    buildFromGit: https://github.com/zerops-recipe-apps/nestjs-minimal-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # PostgreSQL — stores the greetings table used by the dashboard CRUD demo.\n  # TypeORM synchronize handles schema in dev; the migrate.ts script handles it\n  # in prod. NON_HA is appropriate for a dev/staging database. Priority 10\n  # ensures db accepts connections before app containers attempt migrations.\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n    mode: NON_HA\n    verticalAutoscaling:\n      minRam: 0.25\n\n"},{"route":"recipe","why":"A Node.js 22 application built with Express and TypeScript, connected to a PostgreSQL database. Demonstrates idempotent migrations via `zsc execOnce` and a health endpoint at `/` that queries migrated data to confirm both database connectivity and schema integrity. Used within Node.js Hello World recipe for Zerops platform.","recipeSlug":"nodejs-hello-world","fit":"exact","retrievalScore":0.85,"importYaml":"# AI agent environment provides a development space for AI\n# agents to build and version the app. Includes a dev service\n# with the code repository and development tools, a staging\n# service to validate builds, and a low-resource database.\nproject:\n  name: nodejs-hello-world-agent\n\nservices:\n  # Set up the AI agent development environment — Zerops pulls\n  # source and zerops.yaml from the 'buildFromGit' repo, using\n  # the 'dev' setup which installs Node.js 22 and all\n  # dependencies. SSH in and start developing.\n  # Subdomain access gives the dev workspace a public HTTPS URL\n  # so AI agents can verify endpoints during development.\n  - hostname: appdev\n    type: nodejs@22\n    zeropsSetup: dev\n    buildFromGit: https://github.com/zerops-recipe-apps/nodejs-hello-world-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      # Low allocation for an idle dev workspace — scale up\n      # via verticalAutoscaling settings when needed.\n      minRam: 0.5\n\n  # Deploy the staging service — Zerops pulls source from\n  # the 'buildFromGit' repo and uses the 'prod' zeropsSetup\n  # to compile TypeScript and deploy optimized artifacts.\n  # Subdomain provides a public HTTPS URL for testing builds.\n  - hostname: appstage\n    type: nodejs@22\n    zeropsSetup: prod\n    buildFromGit: https://github.com/zerops-recipe-apps/nodejs-hello-world-app\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  # PostgreSQL single-node database shared by 'appdev' and\n  # 'appstage'. Priority 10 starts data services before app\n  # containers, preventing connection errors on first boot.\n  # NON_HA is appropriate for dev/staging where HA durability\n  # isn't required.\n  - hostname: db\n    type: postgresql@18\n    mode: NON_HA\n    priority: 10\n    verticalAutoscaling:\n      minRam: 0.25\n"},{"route":"classic","why":"Manual plan — user describes services directly, no recipe template."}],"nextCalls":[{"tool":"zerops_workflow","args":{"action":"start","intent":"Team-notes dashboard: Node.js backend with Postgres database. Dev and stage runtime services (standard bootstrapMode).","recipeSlug":"nestjs-minimal","route":"recipe","workflow":"bootstrap"}},{"tool":"zerops_workflow","args":{"action":"start","intent":"Team-notes dashboard: Node.js backend with Postgres database. Dev and stage runtime services (standard bootstrapMode).","recipeSlug":"nodejs-hello-world","route":"recipe","workflow":"bootstrap"}},{"tool":"zerops_workflow","args":{"action":"start","intent":"Team-notes dashboard: Node.js backend with Postgres database. Dev and stage runtime services (standard bootstrapMode).","route":"classic","workflow":"bootstrap"}}],"message":"This is the route-menu phase (kind=\"route-menu\") — NO session is open yet. Pick one option and invoke the matching `nextCalls[i]` envelope (an `action=\"start\"` call) to commit the chosen route and open the session (kind=\"session-active\"). Each `nextCalls[i]` carries the full `args` map (route, recipeSlug, sessionId, intent) — agents pass it through verbatim.\n\nOptions:\n  1. route=\"recipe\" recipeSlug=\"nestjs-minimal\" fit=\"exact\" — A minimal NestJS application with a PostgreSQL connection, demonstrating database connectivity, TypeORM migrations, and a health endpoint. Used within NestJS Minimal recipe for Zerops platform.\n  2. route=\"recipe\" recipeSlug=\"nodejs-hello-world\" fit=\"exact\" — A Node.js 22 application built with Express and TypeScript, connected to a PostgreSQL database. Demonstrates idempotent migrations via `zsc execOnce` and a health endpoint at `/` that queries migrated data to confirm both database connectivity and schema integrity. Used within Node.js Hello World recipe for Zerops platform.\n  3. route=\"classic\" — Manual plan — user describes services directly, no recipe template.\n"}
```

---

## `workflow:?:classify::error`
scenario=launch-production-from-standard-pair | bytes=372 | input={"action": "classify"}

```json
{"code":"INVALID_PARAMETER","error":"factType is required for action=classify","suggestion":"Pass factType=\u003cone of gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract\u003e. The type comes from the fact record the writer sub-agent is classifying.","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:launch-production:classify::error`
scenario=launch-production-from-standard-pair | bytes=372 | input={"action": "classify", "workflow": "launch-production"}

```json
{"code":"INVALID_PARAMETER","error":"factType is required for action=classify","suggestion":"Pass factType=\u003cone of gotcha_candidate, ig_item_candidate, verified_behavior, platform_observation, fix_applied, cross_codebase_contract\u003e. The type comes from the fact record the writer sub-agent is classifying.","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:launch-production:complete::error`
scenario=launch-production-laravel-showcase | bytes=190 | input={"action": "complete", "workflow": "launch-production"}

```json
{"code":"INVALID_PARAMETER","error":"Step is required for complete action","suggestion":"Specify step name (e.g., step=\"discover\")","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:launch-production:status::launch-active`
scenario=launch-production-laravel-showcase | bytes=1967 | input={"action": "status", "workflow": "launch-production"}

```json
{"kind":"launch-active","workflow":"launch-production","status":"ready-to-launch","launchId":"1eefb0765cd0e94b","sourceProjectId":"waAzEFn6SBaysG4YE4rv7A","targetProjectName":"api-prod","targetServiceHostname":"api","lastUpdate":"2026-05-15T14:58:28Z","ambiguousChoices":[{"targetProjectName":"api-prod","status":"ready-to-launch","lastUpdate":"2026-05-15T14:58:28Z"},{"targetProjectName":"myapp-prod","status":"ready-to-launch","lastUpdate":"2026-05-15T14:54:22Z"}],"guidance":"### Launch status — mid-flight recovery\n\nWhen `action=\"status\"` returns `kind: \"launch-active\"`, a launch-production workflow is mid-flight for this source project. Conversation context was likely lost (compaction, restart). The envelope carries enough state to resume:\n\n| Field | Use |\n|---|---|\n| `targetProjectName` | Pass back as `productionProjectName` on the resume call. |\n| `status` | Tells you which phase to expect on the next response (e.g. `ready-to-launch` means you still need `launchKey`; `launching` / `configuring-pipeline` means polling). |\n| `lastUpdate` | Sanity-check that this is the launch you remember — if minutes old, it's the active one; if days old, the user may have abandoned it (ask before resuming). |\n| `ambiguousChoices` | When present, multiple non-terminal launches exist for this source. Pick a `productionProjectName` from the list before the resume call. |\n\nResume call shape:\n\n```\nzerops_workflow workflow=\"launch-production\" productionProjectName=\"\u003cfrom envelope\u003e\"\n```\n\nThe `launchKey` is NOT required at the status step — only generate and pass it when the workflow re-enters `ready-to-launch` and you intend to advance to `launching`. Status is read-only; ZCP never constructs a project-admin client on this path. (Multiple active launches detected — pick one productionProjectName from ambiguousChoices.)","nextCall":"zerops_workflow workflow=\"launch-production\" productionProjectName=\"api-prod\""}
```

---

## `workflow:?:status::launch-active`
scenario=launch-production-pipeline-configured | bytes=1967 | input={"action": "status"}

```json
{"kind":"launch-active","workflow":"launch-production","status":"ready-to-launch","launchId":"1eefb0765cd0e94b","sourceProjectId":"waAzEFn6SBaysG4YE4rv7A","targetProjectName":"api-prod","targetServiceHostname":"api","lastUpdate":"2026-05-15T14:58:28Z","ambiguousChoices":[{"targetProjectName":"api-prod","status":"ready-to-launch","lastUpdate":"2026-05-15T14:58:28Z"},{"targetProjectName":"myapp-prod","status":"ready-to-launch","lastUpdate":"2026-05-15T14:54:22Z"}],"guidance":"### Launch status — mid-flight recovery\n\nWhen `action=\"status\"` returns `kind: \"launch-active\"`, a launch-production workflow is mid-flight for this source project. Conversation context was likely lost (compaction, restart). The envelope carries enough state to resume:\n\n| Field | Use |\n|---|---|\n| `targetProjectName` | Pass back as `productionProjectName` on the resume call. |\n| `status` | Tells you which phase to expect on the next response (e.g. `ready-to-launch` means you still need `launchKey`; `launching` / `configuring-pipeline` means polling). |\n| `lastUpdate` | Sanity-check that this is the launch you remember — if minutes old, it's the active one; if days old, the user may have abandoned it (ask before resuming). |\n| `ambiguousChoices` | When present, multiple non-terminal launches exist for this source. Pick a `productionProjectName` from the list before the resume call. |\n\nResume call shape:\n\n```\nzerops_workflow workflow=\"launch-production\" productionProjectName=\"\u003cfrom envelope\u003e\"\n```\n\nThe `launchKey` is NOT required at the status step — only generate and pass it when the workflow re-enters `ready-to-launch` and you intend to advance to `launching`. Status is read-only; ZCP never constructs a project-admin client on this path. (Multiple active launches detected — pick one productionProjectName from ambiguousChoices.)","nextCall":"zerops_workflow workflow=\"launch-production\" productionProjectName=\"api-prod\""}
```

---

## `workflow:?:git-push-setup::error`
scenario=launch-production-pipeline-skip | bytes=452 | input={"action": "git-push-setup"}

```json
{"code":"GIT_TOKEN_INVALID","error":"git-push-setup probe against https://github.com/myorg/myapp.git failed: ssh appdev: exit status 128","suggestion":"Verify: (1) PAT is correct and unexpired, (2) PAT has Contents: Read+Write on this repo (add Secrets/Workflows if integration=actions), (3) Remote URL exists and is reachable. Then re-call with corrected inputs. NO project state was modified.","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:launch-production:start::status=failed`
scenario=launch-production-dev-only | bytes=496 | input={"action": "start", "workflow": "launch-production"}

```json
{"workflow":"launch-production","status":"failed","phase":"launch-production-active","guidance":"ProjectAdminClient construction failed: Not authorized [notAuthorized] Check token validity","blockers":[{"id":"launch-key-invalid","severity":"block","category":"auth","message":"ProjectAdminClient construction failed: Not authorized [notAuthorized] Check token validity","suggestion":"Check token validity","apiCode":"notAuthorized","apiMeta":[{"code":"notAuthorized","error":"Not authorized"}]}]}
```

---

## `workflow:?:build-integration::error`
scenario=launch-production-pipeline-not-configured | bytes=287 | input={"action": "build-integration"}

```json
{"code":"ADOPT_REQUIRED","error":"Service \"appstage\" is not bootstrapped","suggestion":"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\"","recovery":{"tool":"zerops_workflow","action":"start","args":{"route":"adopt","workflow":"bootstrap"}}}
```

---

## `recipe:start`
scenario=kanban-laravel-minimal-dev-only | bytes=8856 | input={"action": "start"}

```json
{"ok":true,"action":"start","slug":"zerops-laravel-minimal","status":{"slug":"zerops-laravel-minimal","current":"research","completed":[],"codebases":0,"services":0,"factsCount":0},"parentStatus":"absent","guidance":"# Research phase\n\nNext call: `zerops_recipe action=update-plan slug=\u003cslug\u003e plan=\u003cpayload\u003e`.\nDon't call `zerops_knowledge` — the tool's own description says it's the\nwrong tool during recipe authoring, and this atom supplies the\nauthoritative service set + runtime table below.\n\n## Where to write outputs\n\nPass `outputRoot=/var/www/zcprecipator/\u003cslug\u003e/` at `action=start`. The\nworkspace at `/var/www/` holds the SSHFS dev mounts (`apidev/`,\n`appdev/`, `workerdev/` — one per codebase) — recipe outputs nest one\nlevel down under `/var/www/zcprecipator/\u003cslug\u003e/` so they don't shadow\nthe dev codebases. The engine refuses `outputRoot=/var/www` directly.\n\n## Canonical services — authoritative versions\n\nDo not guess versions. Use these exactly. Do not add services not in\nthis table.\n\n| Kind    | Hostname  | Type             |\n|---------|-----------|------------------|\n| db      | `db`      | `postgresql@18`  |\n| cache   | `cache`   | `valkey@7.2`     |\n| broker  | `broker`  | `nats@2.12`      |\n| storage | `storage` | `object-storage` |\n| search  | `search`  | `meilisearch@1.20` |\n\nMail / SMTP / Mailpit are NOT canonical showcase services. Production\nusers bring their own SMTP. The research gate rejects non-canonical\nhostnames at `complete-phase`.\n\n## Runtime types\n\nMatch the framework family: `nodejs@22` / `php-nginx@8.4` / `python@3.14`\n/ `go@1` / `rust@stable` / `java@21` / `dotnet@9` / `ruby@3.4` /\n`bun@1.2` / `deno@2` / `elixir@1.16`. Pick the latest from the family\nyour framework uses.\n\n## Service set per tier\n\n- `hello-world-{lang}` → runtime only, **0** managed services.\n- `{framework}-minimal` → runtime + `db` (1 managed service).\n- `{framework}-showcase` → runtime + all **5** canonical services above.\n\n## Classification — full-stack vs API-first\n\nApply to pick codebase shape. Use your framework knowledge:\n\n- **Full-stack** (built-in view engine — Laravel/Blade, Rails/ERB,\n  Django/Jinja2, Phoenix/HEEx, SvelteKit+server, Next.js+server):\n  → **shape 1** (monolith). One codebase, `role=monolith`.\n- **API-first** (JSON-only — NestJS, Express, Fastify, Hono, FastAPI,\n  Flask API, Spring Boot, Gin, Axum, Actix): → **shape 2 or 3**.\n  - Shape 2: 2 codebases. `role=api` + `role=frontend`. Worker shares\n    api's codebase (queue library runs as sibling process).\n  - Shape 3: 3 codebases. Same two + separate worker codebase.\n    Use when the framework's worker needs a first-class long-lived\n    context distinct from the API (NestJS `createApplicationContext`,\n    Express standalone worker). For NestJS specifically: shape 3.\n\nhello-world and minimal tiers are always shape 1 regardless of\nframework family — they test runtime + platform, not service fan-out.\n\n## Frontend default (shape 2/3 only)\n\nSvelte + Vite compiled to static. Deploys on `static` runtime in prod.\nBuild: `npm ci \u0026\u0026 npm run build`. Don't pick React/Vue/Angular unless\nthe user asked for one by name.\n\n## Parent recipe handling\n\nThe `parentStatus` field in the `start` response tells you whether a\nparent recipe is available for your slug — it's a prediction, not a\nloaded body. Resolution is lazy: the parent `.md` body is loaded on\nfirst scaffold-brief dispatch and surfaced inline to that sub-agent\n(and the codebase-content / env-content / refinement composers\ndownstream) as the \"Parent recipe baseline (embedded)\" section.\n\n- **`\"embedded\"`**: parent recipe `internal/knowledge/recipes/\n  \u003cparent-slug\u003e.md` exists in the binary's embedded knowledge corpus.\n  The scaffold sub-agent will see the full body inline when its\n  brief composes. At research phase the body is NOT in the start\n  response — if you want to read it now for convention inheritance\n  (setup naming, project-secret posture, codebase yaml shape), call\n  `zerops_knowledge recipe=\u003cparent-slug\u003e`. This is the one\n  legitimate parent-content use of `zerops_knowledge` at\n  recipe-authoring time. Inherit setup names, project-env posture,\n  and structural patterns — don't re-invent. The embedded minimal\n  recipe has been deployment-verified; your showcase extension\n  should be a superset of its conventions, not a parallel one.\n- **`\"absent\"`**: no parent for this slug — the recipe genuinely\n  has none (`hello-world-*`, `*-minimal`) OR the parent hasn't been\n  published yet (no embedded `.md`). Three cases govern what's\n  allowed:\n  - **For the canonical service set + runtime versions**: proceed\n    from this atom only. Don't call `zerops_knowledge` to substitute\n    for these — they're authoritative whether or not parent exists.\n  - **For convention inheritance** (setup naming, project-secret\n    posture, comment style, codebase yaml shape): without an\n    embedded or mounted parent, the agent extrapolates from\n    `zerops_knowledge query=\u003ctopic\u003e` guides.\n  - **For platform mechanics** (env-var rules, alias contracts,\n    L7 balancer behavior): use `zerops_knowledge query=\u003ctopic\u003e`\n    for the relevant guide. Always preferred over agent\n    extrapolation.\n- **`\"mounted\"`**: parent recipe came from a filesystem-mounted\n  tree (`~/recipes/\u003cparent-slug\u003e/` with full per-codebase READMEs +\n  per-tier import.yamls). Legacy CDE shape. Read\n  `parent.codebases[].readme` and `parent.envImports[\"0\"]` verbatim\n  in addition to the embedded body.\n- **`\"absent\"`**: no parent for this slug — the recipe genuinely\n  has none (`hello-world-*`, `*-minimal`) OR the parent hasn't been\n  published yet (no embedded `.md` AND no filesystem mount). Three\n  cases govern what's allowed:\n  - **For the canonical service set + runtime versions**: proceed\n    from this atom only. Don't call `zerops_knowledge` to substitute\n    for these — they're authoritative whether or not parent exists.\n  - **For convention inheritance** (setup naming, project-secret\n    posture, comment style, codebase yaml shape): without an\n    embedded or mounted parent, the agent extrapolates from\n    `zerops_knowledge query=\u003ctopic\u003e` guides.\n  - **For platform mechanics** (env-var rules, alias contracts,\n    L7 balancer behavior): use `zerops_knowledge query=\u003ctopic\u003e`\n    for the relevant guide. Always preferred over agent\n    extrapolation.\n\n## Payload shape for update-plan\n\n```json\n{\n  \"framework\": \"\u003cslug root, e.g. \\\"nestjs\\\"\u003e\",\n  \"tier\": \"hello-world | minimal | showcase\",\n  \"research\": {\n    \"codebaseShape\": \"1 | 2 | 3\",\n    \"needsAppSecret\": true,\n    \"appSecretKey\": \"\u003cenv-var name, e.g. APP_SECRET / APP_KEY\u003e\",\n    \"description\": \"\u003cone sentence\u003e\"\n  },\n  \"codebases\": [\n    {\"hostname\": \"api\",    \"role\": \"api\",      \"baseRuntime\": \"nodejs@22\"},\n    {\"hostname\": \"app\",    \"role\": \"frontend\", \"baseRuntime\": \"nodejs@22\"},\n    {\"hostname\": \"worker\", \"role\": \"worker\",   \"baseRuntime\": \"nodejs@22\",\n     \"isWorker\": true, \"sharesCodebaseWith\": \"\"}\n  ],\n  \"services\": [\n    {\"hostname\": \"db\",      \"type\": \"postgresql@18\",    \"kind\": \"managed\", \"priority\": 10},\n    {\"hostname\": \"cache\",   \"type\": \"valkey@7.2\",       \"kind\": \"managed\", \"priority\": 10},\n    {\"hostname\": \"broker\",  \"type\": \"nats@2.12\",        \"kind\": \"managed\", \"priority\": 10},\n    {\"hostname\": \"storage\", \"type\": \"object-storage\",   \"kind\": \"storage\"},\n    {\"hostname\": \"search\",  \"type\": \"meilisearch@1.20\", \"kind\": \"managed\", \"priority\": 10}\n  ]\n}\n```\n\nAbove example is a NestJS showcase (shape 3). Swap framework/tier/\ncodebases/roles for other combinations.\n\n## Then\n\n1. `zerops_recipe action=update-plan slug=\u003cslug\u003e plan=\u003cpayload\u003e` (merges\n   into session — you can send partials)\n2. `zerops_recipe action=complete-phase slug=\u003cslug\u003e` → runs the research\n   gate. Read `violations` on failure, patch via another update-plan,\n   retry complete-phase. Do NOT call `zerops_knowledge` to understand\n   violations — the violation message itself names the field + fix.\n3. `zerops_recipe action=enter-phase slug=\u003cslug\u003e phase=provision` →\n   advance into the next phase. **`complete-phase` does NOT auto-\n   advance** — it marks the current phase done; the explicit\n   `enter-phase` call is what moves the session forward. Skipping it\n   leaves the session at `phase=research` and the next `complete-phase`\n   call re-runs research gates.\n"}
```

---

## `recipe:update-plan`
scenario=kanban-laravel-minimal-dev-only | bytes=187 | input={"action": "update-plan"}

```json
{"ok":true,"action":"update-plan","slug":"zerops-laravel-minimal","status":{"slug":"zerops-laravel-minimal","current":"research","completed":[],"codebases":1,"services":1,"factsCount":0}}
```

---

## `recipe:complete-phase`
scenario=kanban-laravel-minimal-dev-only | bytes=6876 | input={"action": "complete-phase"}

```json
{"ok":true,"action":"complete-phase","slug":"zerops-laravel-minimal","status":{"slug":"zerops-laravel-minimal","current":"scaffold","completed":["provision","scaffold","research"],"codebases":1,"services":1,"factsCount":12},"guidance":"Next phase: feature\n\n# Feature phase — implement the showcase feature suite\n\nFor hello-world + minimal tiers, this phase is trivial (one endpoint\nproving the scaffold). For showcase, this phase implements every\nfeature-kind from the feature brief.\n\n## Dispatch\n\n1. **Compose dispatch prompt**: `zerops_recipe\n   action=build-subagent-prompt slug=\u003cslug\u003e briefKind=feature`. Returns\n   the engine-owned recipe-level context block + the feature brief\n   body verbatim (feature-kind catalog, `decision_recording.md`\n   porter_change/field_rationale recording rubric,\n   `mount-vs-container.md` + `yaml-comment-style.md` principles,\n   scaffold-phase symbol table, the showcase scenario spec when\n   `Plan.Tier == \"showcase\"`, and — when `Plan.FeatureKinds` declares\n   `seed`, `scout-import`, or `bootstrap` — the `init-commands-model.md`\n   execOnce key-shape concept atom) + closing notes naming the\n   self-validate path.\n\n2. **One dispatch** for the whole feature suite — feature sub-agent\n   works across every codebase that needs edits. Pass `response.prompt`\n   verbatim. Description: `features-\u003cslug\u003e`.\n\n3. **Behavioral verification** per feature: each feature-kind has an\n   observable signal (cache-demo emits `X-Cache: HIT`, queue-demo has\n   a round-trip status endpoint, etc.). Curl the signal, don't grep\n   the source.\n\n4. **Seed data** so the UI shows something on first click-deploy, not\n   an empty dashboard. A porter deploying tier 4/5 should see real\n   rows, search results, and uploaded objects before creating anything\n   manually. The sub-agent picks the seed command shape for its\n   framework; gate it on a static execOnce key (seeds are\n   non-idempotent by design — see `init-commands-model.md`).\n\n5. **Redeploy affected codebases**: `zerops_deploy` on each codebase\n   the feature agent touched. Re-run `zerops_verify`.\n\n6. **Verify initCommands ran** on each redeployed codebase — same\n   attestation as scaffold (success line in runtime logs + post-deploy\n   data query). If seed data is missing after a green deploy, the\n   execOnce key was burned — recover by touching a source file and\n   redeploying.\n\n7. **Browser-walk verification** on the rendered UI: use the\n   `zerops_browser` tool to navigate to the frontend dev URL and\n   exercise each feature tab (list → create → update → delete →\n   search → upload). After EVERY `zerops_browser` call, record one\n   FactRecord via **`zerops_recipe action=record-fact`** (the v3\n   tool — NOT the legacy `zerops_record_fact`) with\n   `surfaceHint: browser-verification`. Fill:\n   - `topic: \u003ccodebase\u003e-\u003ctab\u003e-browser`\n   - `symptom: \u003cwhat you checked and whether the signal was visible\u003e`\n   - `mechanism: zerops_browser`\n   - `citation: none`\n   - `scope: \u003cservice\u003e/\u003ctab\u003e`\n   - `extra.screenshot: \u003cpath\u003e` and `extra.console: \u003cdigest\u003e`\n   Any console error or blank view is a regression the sub-agent must\n   fix before phase close.\n\n8a. **Record `porter_change` + `field_rationale` facts** for every\n   non-obvious decision you make this phase. Class D framework ×\n   scenario items typically surface here (custom response headers\n   exposed across origins, streamed proxy bodies needing\n   `duplex: 'half'`, per-feature env-var additions). The\n   codebase-content sub-agent at phase 5 reads these facts + on-disk\n   source + spec and synthesizes IG/KB. See\n   `briefs/feature/decision_recording.md` for the full recording\n   contract.\n\n8. **Cross-deploy dev → stage** for every codebase the feature\n   touched: `zerops_deploy sourceService=\u003ch\u003edev targetService=\u003ch\u003estage`\n   + `zerops_verify targetService=\u003ch\u003estage`. Both slots must end\n   green.\n\n## Feature kinds (showcase tier only)\n\n- **crud** — one resource with list+create+show+update+delete\n- **cache-demo** — timing + header surfaces a cache hit/miss\n- **queue-demo** — endpoint enqueues; worker consumes; result readable\n- **storage-upload** — upload file, receive retrievable URL\n- **search-items** — full-text search against the crud resource\n\n## What you author vs what you record (run-16)\n\n**You author**: feature code + `zerops.yaml` field extensions for\nthis phase's needs.\n\n**You record**: `porter_change` + `field_rationale` facts naming the\nWHY behind each non-obvious decision (step 8a above + the\n`decision_recording.md` atom in your brief).\n\n**You do NOT author** documentation surfaces. No `record-fragment` on\n`codebase/\u003ch\u003e/integration-guide`, `knowledge-base`, `claude-md/*`,\nor `zerops-yaml` — phase 5 content sub-agents own those surfaces.\nThe codebase-content sub-agent reads your facts + on-disk source/yaml\n+ spec and synthesizes IG/KB + the whole commented zerops.yaml with\nfull cross-surface awareness.\n\n## After complete-phase phase=feature\n\nWhen `complete-phase phase=feature` (no codebase, after every feature\nsub-agent has terminated cleanly) returns `ok:true`, the engine has\nrecorded the phase as completed AND set the next phase. The next main\naction is `enter-phase phase=finalize` — do NOT re-dispatch the\nfeature sub-agent. The work is done; re-dispatch only re-walks state\nin a fresh sub-agent session and risks compounding session-loss\nartifacts (run-13's features-2 burned ~50s on phase-realignment\nre-walks after exactly this defensive re-dispatch).\n\nIf a compaction event leaves you uncertain whether the feature phase\nclosed, call `zerops_recipe action=status` first — the snapshot's\n`current` and `completed` fields tell you whether to proceed to\nfinalize or re-do feature work.\n\n## Wrapper discipline — what main decides vs sub-agent discovers\n\nThe main agent decides: which codebases the feature set spans, the\nendpoint path shape, the feature-tab UX surface (list-first? search\nbar?). The sub-agent discovers: library choice for the seed/queue/\nsearch client, the exact file layout for its framework, the\nframework-idiomatic command shape. Do NOT pre-chew the library\ndecision in the dispatch wrapper — the sub-agent consults\n`zerops_knowledge` and picks.\n\n## What NOT to do here\n\n- Don't add new managed services. The service set was decided at\n  research and provisioned at provision. Features extend the\n  plan-declared services; they don't extend the plan.\n- Don't add codebases. Codebase count is locked at research.\n- Don't implement mailer unless the plan declared `mail` as a service\n  (it won't for showcase — mail is out-of-scope).\n"}
```

---

## `recipe:enter-phase`
scenario=kanban-laravel-minimal-dev-only | bytes=13123 | input={"action": "enter-phase"}

```json
{"ok":true,"action":"enter-phase","slug":"zerops-laravel-minimal","status":{"slug":"zerops-laravel-minimal","current":"scaffold","completed":["research","provision"],"codebases":1,"services":1,"factsCount":0},"guidance":"# Scaffold phase — one sub-agent dispatch per codebase\n\nEvery codebase in `plan.codebases` gets ONE scaffold sub-agent dispatch.\nThe sub-agent writes source code + `zerops.yaml` for its codebase; the\nmain agent coordinates.\n\n## Mount state at scaffold start\n\nWhen your scaffold sub-agent receives control, the SSHFS mount at\n`/var/www/\u003chostname\u003edev/` already has:\n\n- `.git/` initialized — created by zcp's mount machinery\n  (`ops.InitServiceGit`). Identity: `agent@zerops.io`,\n  branch: `main`.\n- One or more `deploy` commits — created by `zerops_deploy` if any\n  prior deploy ran. Visible in `git log --oneline`.\n\nRecovery for the scaffold commit:\n\n```bash\ncd /var/www/\u003chostname\u003edev\ngit reset --soft $(git rev-list --max-parents=0 HEAD) 2\u003e/dev/null || \\\n  (rm -rf .git \u0026\u0026 git init -q -b main)\ngit config user.email recipe@zerops.io\ngit config user.name 'Recipe Author'\ngit add -A\n[ -n \"$(git status --porcelain)\" ] \u0026\u0026 \\\n  git commit -q -m 'scaffold: initial structure + zerops.yaml' || \\\n  echo 'no changes to commit'\n```\n\nThe `git status --porcelain` pre-check guards against an empty diff:\n`git commit` exits 1 on a clean working tree and cancels every\nparallel tool call in the same Claude message as collateral.\n\nPick the recovery once and apply consistently across all three scaffold\nsub-agents — wipe-and-reinit is acceptable for a dogfood run; in\nproduction, the publish path may want to preserve any meaningful deploy\nhistory. For run 12, wipe-and-reinit.\n\n## Git identity on the dev container\n\nThe dev container has no git identity by default; the SSH-deploy\nsequence runs git operations (commit, push) and fails with\n`SSH_DEPLOY_FAILED: ... default identity` until identity is set.\nBefore the first deploy in any codebase:\n\n```\nssh \u003chostname\u003edev \"git config --global user.name 'zerops-recipe-agent' \\\n  \u0026\u0026 git config --global user.email 'recipe-agent@zerops.io'\"\n```\n\nThis is one-time per dev container; subsequent deploys reuse the\nconfigured identity. Run-13's features-1 burned ~3 min recovering\nfrom two SSH_DEPLOY_FAILED hits before setting identity.\n\n## Dispatch every codebase scaffold IN PARALLEL\n\nWith 2 or 3 codebases, dispatch all sub-agents in a single message (one\n`Agent` tool call per codebase, emitted in parallel). Each sub-agent's\n`zerops_deploy` + `zerops_verify` calls queue naturally at the recipe\nsession mutex — you do NOT need to serialize the dispatch to serialize\nthe deploys. File authoring, `Bash` and `ssh` commands, `npm install`,\nlocal builds, and `zerops_knowledge` consults run concurrently across\nsidechains.\n\nNet savings for a 3-codebase scaffold: 15-30 minutes. Serializing\ndispatch is the wrong optimization — the sub-agents block on their own\nframework work, not on each other.\n\n## For each codebase\n\n1. **Compose dispatch prompt**:\n   `zerops_recipe action=build-subagent-prompt slug=\u003cslug\u003e\n   briefKind=scaffold codebase=\u003chostname\u003e`\n\n   The response carries the FULL dispatch prompt — engine-owned\n   recipe-level context (slug, framework, tier, codebase identity,\n   sister codebases, managed services, your codebase block) +\n   the engine brief body verbatim + closing notes naming the\n   self-validate path. No hand-typed wrapper needed; the engine has\n   every Plan-derivable fact already.\n\n   (`build-brief` still works and returns the brief body alone — use\n   it when you intend to compose your own wrapper, e.g. for a one-off\n   debugging dispatch. Default path is `build-subagent-prompt`.)\n\n2. **Dispatch the sub-agent** via the `Agent` tool. Pass\n   `response.prompt` verbatim as the `prompt`. Description:\n   `scaffold-\u003chostname\u003e`.\n\n3. **Sub-agent produces**: source tree under the Zerops service's\n   SSHFS mount (`/var/www/\u003chostname\u003e/` in-container, or equivalent\n   local path), including `zerops.yaml`. It deploys to its service\n   (`zerops_deploy targetService=\u003chostname\u003e`) and runs the preship\n   contract from its brief (HTTP reachable, X-Forwarded-For echoes,\n   SIGTERM drain, migrations ran). Records facts for any deviation.\n\n4. **Verify the dev deploy**: `zerops_verify targetService=\u003chostname\u003e`.\n\n5. **Start the dev server**: `zerops_dev_server action=start` (dynamic\n   runtimes + any codebase with a frontend bundler). Dev slots run\n   `start: zsc noop --silent` and do NOT auto-start — the long-running\n   process is owned by the agent so code edits don't force a redeploy.\n   Implicit-webserver backends skip this for their own process, but\n   run the tool for a compiled frontend (Vite, esbuild) when applicable.\n   See `principles/dev-loop.md` in the brief.\n\n6. **Verify initCommands ran** (when the scaffold authored any):\n   - `zerops_logs serviceHostname=\u003chostname\u003e severity=INFO since=10m` —\n     confirm the framework's success lines (applied-migration rows,\n     \"N rows seeded\", \"indexed N documents\"). The sub-agent knows what\n     its framework's success output looks like.\n   - Query application state directly: rows in the DB, documents in\n     the search index, objects in storage. Do NOT infer \"initCommands\n     ran\" from \"deploy ACTIVE\" alone — a prior failed deploy can burn\n     the execOnce key silently and the next deploy will skip it.\n   - **Burned-key recovery**: if data is missing after a successful\n     deploy, touch any source file and redeploy — the new deploy\n     version makes per-deploy execOnce keys re-fire. Hand-run the\n     command only when recovery-by-redeploy is not available.\n\n7. **Cross-deploy dev → stage**:\n   `zerops_deploy sourceService=\u003chostname\u003edev targetService=\u003chostname\u003estage`,\n   then `zerops_verify targetService=\u003chostname\u003estage`. This proves the\n   prod setup path (optimized build, `npm ci --omit=dev`, `./dist/~`\n   deployFiles) works, not just the dev self-deploy. Both slots must\n   be green before the phase completes.\n\n## Dispatch integrity\n\nThe engine composes the full dispatch prompt deterministically from\nPlan + Research.Description via `build-subagent-prompt`. Pass\n`response.prompt` to the `Agent` tool byte-identical — the prompt IS\nthe engine output, so paraphrase + truncation risk is mathematically\nzero. There is no separate verify step in the prescribed flow; the\nrecipe action list still carries a recovery primitive in the engine\nfor hand-composed dispatches, but the byte-identical pass-through\npath here doesn't need it.\n\n## What you author vs what you record (run-16)\n\n**You author**: source code + the committed `zerops.yaml` for your\ncodebase. That's the deploy artifact — it has to exist for\n`zerops_deploy` to work.\n\n**You record (`zerops_recipe action=record-fact`)**: structured facts\nnaming every non-obvious decision at densest context — the moment you\nmake the change. Two subtypes cover scaffold scope:\n\n- `porter_change` — code or library decisions a porter would have to\n  make (bind 0.0.0.0, install a library, configure CORS, write a\n  proxy). See `briefs/scaffold/decision_recording.md`.\n- `field_rationale` — non-obvious yaml field decisions\n  (`S3_REGION=us-east-1` because MinIO requires it; two `execOnce`\n  keys to decouple migrate + seed).\n\n**You do NOT author** documentation surfaces during scaffold. No IG /\nKB / CLAUDE.md fragment recording. No `zerops.yaml` block comments\n(those are written above the yaml at codebase-content stitch). Two\ncontent sub-agents at phase 5 (`codebase-content` + `claudemd-author`)\nread your recorded facts + on-disk source / yaml / spec and synthesize\nall documentation surfaces.\n\nThis is the run-16 architecture pivot: deploy phases capture the WHY\nat densest context; content phases author the prose with full context\n+ cross-surface awareness. Closes R-15-4 (CLAUDE.md bleed-through),\nR-15-6 (cross-surface dup), R-15-7 (classification reach).\n\nThe gate set running at scaffold complete-phase (`CodebaseScaffoldGates`,\nintroduced in run-17 §8) checks fact-recording quality only — it does\nNOT check IG / KB / CLAUDE.md / zerops.yaml comment fragments.\nRecording a fragment to \"clear the gate\" is wrong: the gate is already\nsatisfied by your fact-recording work. Fragment authoring runs at\ncodebase-content phase 5 with a different sub-agent.\n\n## Subdomain auto-enable — happens inside `zerops_deploy`\n\nEvery `zerops_deploy` of a non-worker codebase auto-enables the L7\nsubdomain on first deploy when `zerops.yaml` has `httpSupport: true` on\na port. The deploy result carries `SubdomainAccessEnabled: true` plus the\nURL in the response payload; ZCP probes HTTP-readiness before returning\nso the next `zerops_verify` doesn't race port propagation.\n\nDo NOT preemptively call `zerops_subdomain action=enable` inside the\nscaffold sub-agent or the main agent. The deploy handler owns the L7\nactivation step on first deploy. Manual enable is a recovery path only,\nto be used when a deploy result returns a warning indicating auto-enable\nfailed (`auto-enable subdomain failed: ...`).\n\nEligibility derives from REST-authoritative state via two ORed signals:\n`detail.SubdomainAccess` (end-user click-deploy path; set after the\ndeliverable yaml has provisioned a subdomain) OR `detail.Ports[].HTTPSupport`\n(recipe-authoring path; workspace yaml carries `enableSubdomainAccess: true`\nbut the platform doesn't flip `detail.SubdomainAccess` from import alone,\nso the deploy-time port signal is the only intent visible during scaffold).\nRun-15 R-15-1 surfaced the gap: every recipe-authoring scaffold-app\ndispatch had to manually call `zerops_subdomain action=enable` on\nappdev/appstage; run-16 closes it by ORing both signals.\n\n## Wrapper discipline — what main decides vs sub-agent discovers\n\nThe main agent decides: resource name, endpoint path, which codebase\nowns which concern (api/worker/frontend split), which tier the plan\ntargets. The sub-agent discovers: library choice, client config shape,\npackage name, framework-specific import path. Do NOT pre-chew library\ndecisions in the dispatch wrapper — the sub-agent consults\n`zerops_knowledge` and picks based on its framework expertise.\n\n## Scaffold close — main-agent action sequence\n\nAfter all scaffold sub-agents have terminated:\n\n1. `zerops_deploy` for each codebase (cross-deploy dev → stage if not\n   already done by the sub-agent).\n2. `zerops_verify` for each cross-deployed service.\n3. `zerops_recipe action=complete-phase phase=scaffold` (no codebase\n   parameter). The gate requires every codebase deployed + verified on\n   dev + stage before it returns `ok:true`. Calling complete-phase\n   before deploy + verify wastes a turn — the gate fails on missing\n   verifications and you re-run the same sequence anyway.\n\nThe per-codebase pre-termination self-validate (sub-agent's call\nduring scaffold) is a different action — the sub-agent already\nself-validates before terminating. Main's no-codebase call is the\nfinal phase-advance gate.\n\n## Complete-phase gate\n\nEvery plan.codebase hostname must be deployed + verified on BOTH the\ndev and stage slots, every scaffold-owned fragment id recorded, and\nevery codebase with initCommands must have attested that they ran\n(success line + post-deploy data check). Facts recorded during the\nphase flow into the classification gate at finalize.\n\n## Self-validate before terminating (sub-agent)\n\nBefore you terminate, call:\n\n    zerops_recipe action=complete-phase phase=scaffold codebase=\u003cyour-host\u003e\n\nThis runs the codebase-scoped validators (IG / KB / CLAUDE / yaml-\ncomment / source-comment-voice) against your codebase's surfaces only\n— peer codebases are NOT validated, so you only see your own work.\n\nIf `ok:true`: all your work passes the gate; safe to terminate.\n\nIf `ok:false` with violations:\n- Violations on `codebase/\u003chost\u003e/{integration-guide,knowledge-base,\n  claude-md/*}` ids → fix via `record-fragment mode=replace\n  fragmentId=codebase/\u003chost\u003e/\u003cname\u003e fragment=\u003ccorrected body\u003e`.\n- Violations on `\u003cSourceRoot\u003e/zerops.yaml` (yaml-comment-missing-\n  causal-word, etc.) → ssh-edit the yaml file directly; it's not a\n  fragment, it's the committed source. After ssh-edit, the engine's\n  IG item-1 generator will re-read the yaml body on next stitch.\n- Re-call `complete-phase phase=scaffold codebase=\u003cyour-host\u003e` to\n  verify the fix.\n- Repeat until `ok:true`, then terminate.\n\nThe phase-level `complete-phase` (no codebase parameter) is the main\nagent's responsibility after all sub-agents return — it advances the\nphase state. Your job is just to ensure your own codebase's gate\npasses before you exit.\n"}
```

---

## `recipe:emit-yaml`
scenario=kanban-laravel-minimal-dev-only | bytes=518 | input={"action": "emit-yaml"}

```json
{"ok":true,"action":"emit-yaml","slug":"zerops-laravel-minimal","yaml":"services:\n  - hostname: appdev\n    type: php-nginx@8.4\n    startWithoutCode: true\n    maxContainers: 1\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  - hostname: appstage\n    type: php-nginx@8.4\n    enableSubdomainAccess: true\n    verticalAutoscaling:\n      minRam: 0.5\n\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n    mode: NON_HA\n    verticalAutoscaling:\n      minRam: 0.25\n\n"}
```

---

## `recipe:build-subagent-prompt`
scenario=kanban-laravel-minimal-dev-only | bytes=265 | input={"action": "build-subagent-prompt"}

```json
{"ok":true,"action":"build-subagent-prompt","slug":"zerops-laravel-minimal","notice":"brief written to disk; dispatch sub-agent with this path","briefPath":"/var/www/zcprecipator/zerops-laravel-minimal/.briefs/scaffold-app-1778928115447221491.md","briefSize":41295}
```

---

## `recipe:record-fact`
scenario=kanban-laravel-minimal-dev-only | bytes=66 | input={"action": "record-fact"}

```json
{"ok":true,"action":"record-fact","slug":"zerops-laravel-minimal"}
```

---

## `workflow:bootstrap/recipe:start::error`
scenario=kanban-laravel-minimal-dev-only | bytes=326 | input={"action": "start", "workflow": "bootstrap", "route": "recipe", "recipeSlug": "zerops-laravel-minimal"}

```json
{"code":"WORKFLOW_ACTIVE","error":"Bootstrap start failed: bootstrap start: recipe route: unknown slug \"zerops-laravel-minimal\"","suggestion":"Call action=start workflow=bootstrap without route to discover valid options, or action=reset to clear the existing session","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `manage:reload`
scenario=launch-production-existing-with-webhook | bytes=325 | input={"action": "reload"}

```json
{"id":"X03hdLvjT5KIffFmYJWfhg","actionName":"stack.reload","status":"FINISHED","serviceStacks":[{"id":"zmRRZES8S86Xl7gKagpvnQ","name":"appdev"}],"created":"2026-05-21T10:42:49.048Z","started":"2026-05-21T10:42:49.066Z","finished":"2026-05-21T10:42:57.974Z","nextActions":"Verify health: zerops_logs severity=ERROR since=1m."}
```

---

## `workflow:launch-production:start::error`
scenario=launch-to-existing-prod-project | bytes=408 | input={"action": "start", "workflow": "launch-production"}

```json
{"code":"API_ERROR","error":"Project environment variable key 'SESSION_SECRET' is not unique.","suggestion":"The platform flagged specific fields — see apiMeta for each field's failure reason.","apiCode":"projectEnvDuplicateKey","apiMeta":[{"code":"projectEnvDuplicateKey","error":"Project environment variable key 'SESSION_SECRET' is not unique."}],"recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:launch-production:start::status=launching`
scenario=launch-to-existing-prod-project | bytes=178 | input={"action": "start", "workflow": "launch-production"}

```json
{"workflow":"launch-production","status":"launching","phase":"launch-production-active","guidance":"Launch in progress. State file shows targetProjectID vYnTJTgiQBWR5Xk5qIic8g."}
```

---

## `workflow:launch-production:list::nondict`
scenario=launch-production-pipeline-not-configured | bytes=2 | input={"action": "list", "workflow": "launch-production"}

```json
[]
```

---

## `workflow:?:adopt-local::error`
scenario=git-push-setup-then-actions | bytes=182 | input={"action": "adopt-local"}

```json
{"code":"INVALID_PARAMETER","error":"action=\"adopt-local\" is for local env — use workflow=\"bootstrap\" in container env","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:launch-production:reset::error`
scenario=launch-production-from-standard-pair | bytes=607 | input={"action": "reset", "workflow": "launch-production"}

```json
{"code":"DIAGNOSIS_REQUIRED","error":"launch-production-reset requires confirmDestructive after diagnosis","suggestion":"After reading logs, retry with: zerops_workflow action=\"reset\" workflow=\"launch-production\" productionProjectName=\"\u003cyour-target-name\u003e\" confirmDestructive={\"operation\":\"launch-production-reset\",\"acknowledgedTargets\":[\"myapp-prod\"]}","recovery":{"tool":"zerops_workflow","action":"status"},"wouldDestroy":{"operation":"launch-production-reset","targets":["myapp-prod"],"wouldDestroy":{"localFiles":["/var/www/.zcp/state/launch-production/45516e996ff1b824.json"]}}}
```

---

## `workflow:launch-production:reset::obj`
scenario=launch-production-from-standard-pair | bytes=358 | input={"action": "reset", "workflow": "launch-production"}

```json
{"operation":"launch-production-reset","launchId":"45516e996ff1b824","sourceProjectId":"waAzEFn6SBaysG4YE4rv7A","targetProjectName":"myapp-prod","priorStatus":"ready-to-launch","deletedStateFile":"/var/www/.zcp/state/launch-production/45516e996ff1b824.json","note":"State file deleted. Next action=\"start\" workflow=\"launch-production\" will start fresh."}
```

---

## `manage:restart`
scenario=cadence-multiservice-build-run2-replay | bytes=326 | input={"action": "restart"}

```json
{"id":"smV10bb5S7u2b2LjwizoZA","actionName":"stack.restart","status":"FINISHED","serviceStacks":[{"id":"1TyNSvNHSxKRBoNbzrA4JA","name":"appdev"}],"created":"2026-05-21T09:22:34.717Z","started":"2026-05-21T09:22:34.728Z","finished":"2026-05-21T09:22:49.621Z","nextActions":"Verify health: zerops_logs severity=ERROR since=1m."}
```

---

## `workflow:?:build-integration::status=noop`
scenario=git-push-setup-then-actions | bytes=297 | input={"action": "build-integration"}

```json
{"buildIntegration":"actions","service":"appdev","status":"noop","workSessionState":{"status":"none","note":"No active develop session — deploy not tracked. Start one via zerops_workflow action=\"start\" workflow=\"develop\" intent=\"...\" scope=[...] to pick up auto-close + verify tracking."}}
```

---

## `workflow:?:set-default-setup::error`
scenario=stale-setup-rename-recovery | bytes=268 | input={"action": "set-default-setup"}

```json
{"code":"INVALID_PARAMETER","error":"action=\"set-default-setup\" requires targetService","suggestion":"Pass targetService=\u003cruntime-hostname\u003e — the service whose canonical setup-name you want to set","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:?:set-default-setup::status=configured`
scenario=stale-setup-rename-recovery | bytes=91 | input={"action": "set-default-setup"}

```json
{"status":"configured","service":"appdev","primarySetupName":"dev","stageSetupName":"prod"}
```

---

## `workflow:?:skip::error`
scenario=recipe-laravel-showcase-fullstack | bytes=267 | input={"action": "skip", "step": "discover"}

```json
{"code":"BOOTSTRAP_NOT_ACTIVE","error":"Skip step failed: bootstrap skip: skip step: \"discover\" is mandatory and cannot be skipped","suggestion":"Only skippable steps (generate, deploy, close) can be skipped","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:bootstrap/adopt:start::error`
scenario=launch-production-from-standard-pair | bytes=356 | input={"action": "start", "workflow": "bootstrap", "route": "adopt"}

```json
{"code":"INVALID_PARAMETER","error":"plan is not accepted in action=start; submit it via action=\"complete\" step=\"discover\" plan=[...]","suggestion":"Start commits the route only. The discover step is the reasoning space where the plan is produced from route-specific materials; commit it there.","recovery":{"tool":"zerops_workflow","action":"status"}}
```

---

## `workflow:export:start::error`
scenario=export-buildfromgit-self-snapshot | bytes=384 | input={"action": "start", "workflow": "export"}

```json
{"code":"SERVICE_NOT_FOUND","error":"Service \"appdev\" has no bootstrapped meta — export needs the topology.Mode (dev / standard / stage / simple / local-stage / local-only) to resolve variant","suggestion":"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\". Or adopt the service via adopt-local.","recovery":{"tool":"zerops_workflow","action":"status"}}
```
