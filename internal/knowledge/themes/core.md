# Zerops YAML Reference

YAML generation reference: import.yaml and zerops.yaml schemas, rules, pitfalls, and complete multi-service examples.

---

## import.yaml Schema

```
project:                               # OPTIONAL (omit in ZCP context)
  name: string                         # REQUIRED if project: exists
  corePackage: LIGHT | SERIOUS         # default LIGHT
  envVariables: map<string,string>     # project-level vars
  tags: string[]

services[]:                            # REQUIRED
  hostname: string                     # REQUIRED, max 40, a-z and 0-9 ONLY (no hyphens/underscores), IMMUTABLE
  type: <runtime>@<version>            # REQUIRED (100+ valid values)
  mode: HA | NON_HA                    # Defaults to NON_HA if omitted for managed services. IMMUTABLE
  priority: int                        # higher = starts first (DB=10, app=5)
  enableSubdomainAccess: bool          # zerops.app subdomain
  startWithoutCode: bool               # start without deploy (runtimes only)
  minContainers: 1-10                  # RUNTIME ONLY, default 1 (managed services have fixed containers)
  maxContainers: 1-10                  # RUNTIME ONLY (managed: NON_HA=1, HA=3, fixed)
  envSecrets: map<string,string>       # blurred in GUI by default, editable/deletable
  dotEnvSecrets: string                # .env format, auto-creates secrets
  # NOTE: envVariables does NOT exist at service level — only at project level
  # For non-secret env vars on a service, use zerops_env after import or zerops.yaml run.envVariables
  buildFromGit: url                    # one-time build from repo — use ONLY with verified URLs (utility recipes like mailpit). Do NOT guess URLs.
  objectStorageSize: 1-100             # GB, object-storage only (changeable in GUI later)
  objectStoragePolicy: private | public-read | public-objects-read | public-write | public-read-write
  objectStorageRawPolicy: string       # custom IAM Policy JSON (alternative to objectStoragePolicy)
  override: bool                       # triggers redeploy of existing runtime service with same hostname
  mount: string[]                      # pre-configure shared storage connection (ALSO requires mount in zerops.yaml run section to activate)
  nginxConfig: string                  # custom nginx config for PHP/static/nginx services
  zeropsSetup: string                  # inline zerops.yaml setup name
  zeropsYaml: object                   # inline zerops.yaml configuration in import
  verticalAutoscaling:                 # RUNTIME + DB/CACHE ONLY (not shared-storage, not object-storage)
    cpuMode: SHARED | DEDICATED        # default SHARED
    minCpu/maxCpu: int                 # CPU threads
    startCpuCoreCount: int             # CPU at container start
    minRam/maxRam: float               # GB
    minFreeRamGB: float                # absolute free threshold
    minFreeRamPercent: float            # percentage free threshold
    minFreeCpuCores: float             # absolute free CPU threshold
    minFreeCpuPercent: float            # percentage free CPU threshold
    minDisk/maxDisk: float              # GB, disk never shrinks
    swapEnabled: bool                  # enable swap memory (safety net, default varies by service type)
```

### Preprocessor Functions
Enable with `#zeropsPreprocessor=on` as first line. Syntax: `<@function(<args>)>`, chain modifiers with `|`: `<@generateRandomString(<32>)|sha256>`.

**Functions:**
- `<@generateRandomString(<len>)>` -- random alphanumeric string
- `<@generateRandomBytes(<len>)>` -- random bytes (binary)
- `<@generateRandomInt(<min>,<max>)>` -- random integer in range
- `<@pickRandom(<opt1>,<opt2>,...)>` -- pick random from options
- `<@setVar(<name>,<content>)>` / `<@getVar(<name>)>` -- store and retrieve variables
- `<@generateRandomStringVar(<name>,<len>)>` -- generate + store string variable
- `<@generateJWT(<secret>,<payload>)>` -- JWT token generation
- `<@getDateTime(<format>,[<tz>])>` -- formatted datetime
- `<@generateED25519Key(<name>)>`, `<@generateRSA2048Key(<name>)>`, `<@generateRSA4096Key(<name>)>` -- key pairs (stores pubKey/privKey)

**Modifiers** (applied with `|`): `sha256`, `sha512`, `bcrypt`, `argon2id` (hashing) | `toHex`, `toString` (encoding) | `upper`, `lower`, `title` (case) | `noop` (testing)

**Rules:** Functions return strings. Two-phase processing: preprocessing then YAML parsing. Values generated once at import -- fixed after, not regenerated. Escape special characters: `\<`, `\>`, `\|` (double-escape `\\` for backslash)

**Always-available** `${...}` functions: `${random(length)}`, `${randomInt(min,max)}`, `${sha256(value)}`, `${bcrypt(value,rounds)}`, `${argon2id(value)}`, `${jwt(algo,secret,payload)}`, `${generateRSAKeyPair(bits)}`, `${generateEd25519KeyPair()}`

**WARNING**: API `dryRun` validates YAML schema only -- it does NOT enforce service-type restrictions (e.g., `minContainers` on managed services passes dry-run but fails real import). The rules in this document ARE the validation layer.

---

## zerops.yaml Schema

```
zerops[]:
  setup: string                        # REQUIRED, matches service hostname
  build:
    base: string | string[]            # runtime(s) -- multi-base: [php@8.4, nodejs@22]
    os: alpine | ubuntu                # default alpine
    prepareCommands: string[]          # cached in base layer
    buildCommands: string[]            # runs every build
    deployFiles: string | string[]     # MANDATORY -- nothing auto-deploys
    cache: bool | string | string[]    # paths to cache
    addToRunPrepare: string | string[] # copy files from build to /home/zerops/ in prepare container
    envVariables: map<string, string|number|bool>
  deploy:
    temporaryShutdown: bool            # false = zero-downtime (default)
    readinessCheck:                    # gates traffic switch
      httpGet: { port: int, path: string }
      exec: { command: string }
  run:
    base: string                       # if different from build base
    os: alpine | ubuntu
    start: string                      # REQUIRED (except implicit-webserver: php-nginx, php-apache, nginx, static)
    ports[]: { port: 10-65435, httpSupport: bool, protocol: tcp|udp }  # httpSupport: true = receives HTTP via L7 LB (REQUIRED for web); false = raw TCP/UDP only
    initCommands: string[]             # every container start (migrations, seeding)
    prepareCommands: string[]          # runtime image customization
    documentRoot: string               # webserver runtimes only (PHP/Nginx/Static)
    healthCheck: { httpGet | exec }    # 2xx or exit 0, 5-min retry window
    envVariables: map<string, string|number|bool>
    crontab[]: { timing: cron, command: string, allContainers: bool }
    routing: { cors, redirects[], headers[] }
    mount: string[]                    # shared storage hostnames to mount at /mnt/{hostname} (REQUIRED for storage access at runtime)
    startCommands[]: { command, name, workingDir, initCommands[] }
```

---

## Rules & Pitfalls

### Networking
- **NEVER** listen on port 443 or 80 (exception: PHP uses 80). REASON: Zerops reserves 80/443 for SSL termination. Use 3000, 8080, etc.
- **ALWAYS** use port range 10-65435. REASON: ports outside this range are reserved by the platform
- **ALWAYS** set Cloudflare SSL to "Full (strict)" when using Cloudflare proxy. REASON: "Flexible" causes infinite redirect loops

### Build & Deploy
- **ALWAYS** include `node_modules` in `deployFiles` for Node.js apps (unless bundled). REASON: runtime container doesn't run `npm install`
- **ALWAYS** deploy fat/uber JARs for Java. REASON: build and run are separate containers; thin JARs lose their dependencies
- **ALWAYS** use Maven/Gradle wrapper (`./mvnw`, `./gradlew`) or install build tools via `prepareCommands`. REASON: build container has JDK only -- Maven, Gradle are NOT pre-installed
- **NEVER** reference `/var/www/` in `run.prepareCommands`. REASON: deploy files arrive AFTER prepareCommands execute; `/var/www` is empty during prepare
- **ALWAYS** use `addToRunPrepare` + `/home/zerops/` path for files needed in `run.prepareCommands`. REASON: this is the only way to get files from build into the prepare phase
- **NEVER** use `initCommands` for package installation. REASON: initCommands run on every container restart; use `prepareCommands` for one-time setup
- **ALWAYS** use `--no-cache-dir` for pip in containers. REASON: prevents wasted disk space on ephemeral containers
- **ALWAYS** use `--ignore-platform-reqs` for Composer on Alpine. REASON: musl libc may not satisfy platform requirements checks

### Base Image & OS
- **NEVER** use `apt-get` on Alpine. REASON: Alpine uses `apk`; apt-get doesn't exist
- **NEVER** use `apk` on Ubuntu. REASON: Ubuntu uses `apt-get`; apk doesn't exist
- **ALWAYS** use `sudo apk add --no-cache` on Alpine. REASON: prevents stale package index caching; sudo required as containers run as `zerops` user
- **ALWAYS** use `sudo apt-get update && sudo apt-get install -y` on Ubuntu. REASON: package index not pre-populated; sudo required as containers run as `zerops` user
- **NEVER** set `run.base: alpine@*` for Go. REASON: causes glibc/musl mismatch for CGO-linked binaries -> 502. Omit `run.base` or use `run.base: go@latest`
- **ALWAYS** use `os: ubuntu` for Deno and Gleam. REASON: these runtimes are not available on Alpine

### Environment Variables — Three Levels

**Where to put what:**

| What | Where | Why |
|------|-------|-----|
| Shared config (non-secret, all services) | `project.envVariables` in import.yaml | Auto-inherited by every service. Do NOT re-reference in zerops.yaml (creates shadow). |
| Cross-service wiring (DB creds, cache host) | `run.envVariables` in zerops.yaml | `${hostname_varname}` references resolve at deploy time. This is the ONLY place cross-service refs work. |
| Per-service secrets (APP_KEY, SECRET_KEY_BASE) | `envSecrets` per-service in import.yaml | Each service gets its own key. Blurred in GUI. Auto-injected as OS vars — do NOT re-reference in zerops.yaml. |

**How they work:**
- **project.envVariables** (import.yaml): inherited by all services in the project. Good for shared non-secret config (e.g. `APP_NAME`, `LOG_LEVEL`). Changes via GUI, no redeploy needed.
- **run.envVariables** (zerops.yaml): injected at deploy time. Support `${hostname_varname}` cross-service references. Changes take effect on next deploy.
- **envSecrets** (import.yaml per-service, or GUI): injected directly as OS env vars at container start. Changes require a **service restart** (not just redeploy).

**Critical rules:**
- `${...}` syntax is ONLY for cross-service references in run.envVariables (`${db_hostname}`). Writing `APP_KEY: ${APP_KEY}` does NOT reference the envSecret — it creates a literal string.
- import.yaml service level: ONLY `envSecrets` and `dotEnvSecrets`. No `envVariables` at service level (project-level only).
- Managed services auto-generate credentials (hostname, port, user, password, dbName, connectionString) — do NOT set these in import.yaml.
- `zeropsSubdomain`: platform-injected full HTTPS URL (e.g. `https://app-1df2-3000.prg1.zerops.app`), created when `enableSubdomainAccess: true`.

### Import & Service Creation
- **ALWAYS** set explicit `mode: NON_HA` or `mode: HA` for managed services (DB, cache, shared-storage). Mode defaults to NON_HA if omitted. Set HA explicitly for production. IMMUTABLE
- **NEVER** set `mode` for runtime services. REASON: `mode` is only for managed services. Runtime HA is achieved via `minContainers: 2+` (horizontal scaling)
- **NEVER** set `minContainers`/`maxContainers` for managed services. REASON: managed services have fixed container counts (NON_HA=1, HA=3); setting these causes import failure
- **NEVER** set `verticalAutoscaling` for shared-storage or object-storage. REASON: these service types don't support vertical scaling; setting it causes import failure
- **ALWAYS** set `priority: 10` for databases/storage services. REASON: ensures they start before application services that depend on them
- **ALWAYS** set `enableSubdomainAccess: true` in import.yaml AND call `zerops_subdomain action="enable"` once after the first deploy of each new service. REASON: the import flag marks intent; the subdomain API call activates the L7 route
- **ALWAYS** use `valkey@7.2` (not `valkey@8`). REASON: v8 passes dry-run validation but fails actual import
- **NEVER** use Docker `:latest` tag. REASON: cached and won't re-pull; always use specific version tags
- **ALWAYS** use `--network=host` for Docker services. REASON: without it, container cannot receive traffic from Zerops routing
- **ALWAYS** use `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` for Object Storage. REASON: MinIO backend doesn't support virtual-hosted style

### Import Generation (dev/stage patterns)
- **Standard mode:** create dev/stage pairs for runtimes. Naming: `{prefix}dev` and `{prefix}stage` (e.g., `appdev`/`appstage`, `apidev`/`apistage`). Dev mode: single `{prefix}dev`. Simple mode: single `{name}` with real start command
- **ALWAYS** set `startWithoutCode: true` ONLY on dev services (not stage). Simple mode: set on the single service. REASON: dev starts immediately; stage stays in READY_TO_DEPLOY until code arrives
- **ALWAYS** set `maxContainers: 1` for dev services. REASON: dev uses SSHFS; multiple containers cause file conflicts
- **ONLY** set `zeropsSetup` in import.yaml when using `buildFromGit`. REASON: zeropsSetup requires buildFromGit (API rejects one without the other). For workspace deploys (no buildFromGit), use `zerops_deploy setup="..."` parameter instead
- **ALWAYS** set `minRam` high enough for initial RAM spikes (autoscaling has ~10-20s reaction time). Dev needs higher than stage/prod (compilation on container)
- **ALWAYS** use managed service hostname conventions: `db`, `cache`, `queue`, `search`, `storage`. REASON: standardizes cross-service references
- **ALWAYS** put `envSecrets` per-service (not project level) for framework secrets (APP_KEY, SECRET_KEY_BASE). REASON: each service needs its own key — sharing across dev/stage leaks session cookies. envSecrets are auto-injected as OS vars, no need to wire them in zerops.yaml
- **ALWAYS** use generic `setup:` names in zerops.yaml (`dev`, `prod`, `worker`). When deploying to a hostname that differs from the setup name, pass `setup="..."` to `zerops_deploy`. REASON: generic names work across all environments; `zeropsSetup` in recipe import.yaml + `--setup` in workspace deploy both handle the mapping
- **ALWAYS** add `run.healthCheck` and `deploy.readinessCheck` ONLY to stage/prod entries, NEVER to dev. REASON: dev uses `zsc noop --silent`; healthCheck would restart the container during iteration

### Runtime-Specific
- **ALWAYS** set `server.address=0.0.0.0` for Java Spring Boot. REASON: Spring Boot defaults to localhost binding -> unreachable from LB
- **ALWAYS** set `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` for PHP Laravel. REASON: reverse proxy breaks CSRF validation without trusted proxy config
- **ALWAYS** set `CSRF_TRUSTED_ORIGINS` for Django behind proxy. REASON: reverse proxy changes the origin header; Django blocks requests
- **ALWAYS** set `PHX_SERVER=true` for Phoenix/Elixir releases. REASON: without it, the HTTP server doesn't start in release mode
- **ALWAYS** use `cargo b --release` for Rust. REASON: debug builds are 10-100x slower
- **ALWAYS** use `CGO_ENABLED=0 go build` when unsure about CGO dependencies. REASON: produces static binary compatible with any base
- **ALWAYS** use `sudo apk add --no-cache php84-<ext>` for Alpine PHP extensions. REASON: version prefix must match PHP major+minor; sudo required

### Scaling & Platform
- **NEVER** attempt to change HA/NON_HA mode after creation. REASON: mode is immutable; must delete and recreate service
- **NEVER** attempt to change hostname after creation. REASON: hostname is immutable; it becomes the internal DNS name
- **NEVER** expect disk to shrink. REASON: auto-scaling only increases disk; to reduce, recreate the service

---

## Schema Rules

### Deploy Semantics
- Without tilde: `dist` -> `/var/www/dist/` (directory preserved)
- **Tilde syntax**: `dist/~` -> contents extracted to `/var/www/` (directory stripped)
- All files land under `/var/www`
- **INVARIANT**: `run.start` path MUST match where `deployFiles` places files:
  - `deployFiles: [dist]` + `start: bun dist/index.js` -- CORRECT (file at `/var/www/dist/index.js`)
  - `deployFiles: dist/~` + `start: bun index.js` -- CORRECT (file at `/var/www/index.js`)
  - `deployFiles: dist/~` + `start: bun dist/index.js` -- BROKEN (no `/var/www/dist/` exists)
- **Git required**: `zerops_deploy` uses `zcli push` which requires a git repository
- **Self-deploy with `[.]`**: When switching from a recipe's production `deployFiles` to `[.]`, build output stays in its original directory under `/var/www/` instead of being extracted/flattened. The `start` command must reference the full path:
  - Recipe uses `dist/~` + `start: bun index.js` → with `[.]`: `start: bun dist/index.js` (files at `/var/www/dist/`)
  - Recipe uses `./app` + `start: ./app` → with `[.]`: same `start: ./app` (binary at `/var/www/app`)
  - Recipe uses `target/release/~binary` + `start: ./binary` → with `[.]`: `start: ./target/release/binary`
  - Principle: tilde extraction no longer happens, directory structure is preserved as-is. Match `start` to where build output actually lands.
- **`.deployignore`**: Place at repo root (gitignore syntax) to exclude files/folders from deploy artifact. NOT recursive into subdirectories by default. Recommended to mirror `.gitignore` patterns. Also works with `zcli service deploy`.
- **Deploy mode determines `deployFiles`**:

  | Deploy mode | Who deploys? | deployFiles | start |
  |-------------|-------------|-------------|-------|
  | Dev (in dev+stage) | Self-deploy | `[.]` | `zsc noop --silent` (implicit-webserver: omit) |
  | Stage (in dev+stage) | Cross-deploy from dev | Recipe pattern | Compiled/prod start |
  | Simple (single service) | Self-deploy | `[.]` | Real start command |
  | Production (buildFromGit) | Platform from git | Recipe pattern | Compiled/prod start |

  Self-deploy with specific paths (e.g., `[app]`, `dist/~`) destroys source files + zerops.yaml after deploy, making iteration impossible. Only cross-deploy targets and git-based builds can use specific paths safely.

### Cache Architecture (Two-Layer)
- **Base layer**: OS + prepareCommands (invalidated only when prepareCommands change)
- **Build layer**: buildCommands output (invalidated every build)
- `cache: false` only affects `/build/source` -- modules elsewhere remain cached

### Public Access
- **Shared IPv4**: free, HTTP/HTTPS only, requires BOTH A and AAAA DNS records
- **Dedicated IPv4**: $3/30 days, all protocols
- **IPv6**: free, dedicated per project
- **zerops.app subdomain**: 50MB limit, not production

### zsc Commands
- `zsc execOnce <key> -- <cmd>`: run once across all containers (HA-safe migrations)
- `zsc add <runtime>@<version>`: install additional runtime in prepareCommands

---

## Multi-Service Examples

**Dev/Stage Setup (App + DB):**
```yaml
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
  - hostname: appdev
    type: nodejs@22
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1.0
  - hostname: appstage
    type: nodejs@22
    enableSubdomainAccess: true
```
Dev starts immediately (RUNNING) with `startWithoutCode`. Stage stays in READY_TO_DEPLOY until first deploy from dev.
zerops.yaml must have `setup: appdev` and `setup: appstage` blocks matching hostnames.

**Dev/Stage Setup (API + DB) — alternative prefix:**
```yaml
services:
  - hostname: db
    type: mariadb@10.6
    mode: NON_HA
    priority: 10
  - hostname: apidev
    type: bun@1.2
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1.0
  - hostname: apistage
    type: bun@1.2
    enableSubdomainAccess: true
```
Same pattern as above but using `api` prefix instead of `app`. zerops.yaml needs `setup: apidev` and `setup: apistage`.

**Full-Stack Dev/Stage (App + DB + Cache + Storage):**
```yaml
#zeropsPreprocessor=on
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10
  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
  - hostname: appdev
    type: bun@1.2
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1.0
  - hostname: appstage
    type: bun@1.2
    enableSubdomainAccess: true
    envSecrets:
      APP_KEY: <@generateRandomString(<32>)>
```

**Production (buildFromGit, no SSHFS):**
```yaml
services:
  - hostname: db
    type: postgresql@16
    mode: HA
    priority: 10
  - hostname: app
    type: go@1.24
    buildFromGit: https://github.com/user/repo
    enableSubdomainAccess: true
```
zeropsSetup omitted — with `buildFromGit`, defaults to hostname `app`, so zerops.yaml needs `setup: app`. For recipe repos with generic names (`setup: prod`), set `zeropsSetup: prod` explicitly.
