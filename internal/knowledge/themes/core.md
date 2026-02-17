# Zerops Core Reference

## TL;DR
Complete reference for Zerops YAML generation: platform model, schemas, and rules. Everything needed to produce correct import.yml and zerops.yml for any stack.

## Keywords
zerops, platform, architecture, lifecycle, build, deploy, run, container, networking, vxlan, scaling, storage, state, immutable, hostname, mode, base image, alpine, ubuntu, rules, pitfalls, gotchas, grammar, import.yml, zerops.yml, schema, ports, binding, 0.0.0.0, environment variables, autoscaling, yaml, pipeline, tilde, HA, NON_HA, cron, health check, readiness check, prepareCommands, buildCommands, deployFiles, envSecrets, envVariables

## Container Universe

Everything on Zerops runs in **full Linux containers** (Incus, not Docker). Each container has:
- Full SSH access, working directory `/var/www`
- Connected via VXLAN private network (per project)
- Addressable by service hostname (internal DNS)
- Own disk (persistent, grow-only)

Hierarchy: **Project > Service > Container(s)**. One project = one isolated network. Services communicate by hostname over this network.

**Two core plans** govern project-level resource allowances:

| | Lightweight | Serious |
|---|---|---|
| Build time | 15 hours | 150 hours |
| Backup storage | 5 GB | 25 GB |
| Egress | 100 GB | 3 TB |
| Infrastructure | Single container | Multi-container (HA) |

Upgrading from Lightweight to Serious costs $10 one-time, is irreversible, and causes approximately 35 seconds of network unavailability.

## The Two YAML Files

| File | Purpose | Scope |
|------|---------|-------|
| `import.yml` | **Topology** -- WHAT exists | Services, types, versions, scaling, env vars |
| `zerops.yml` | **Lifecycle** -- HOW it runs | Build, deploy, run commands per service |

These are separate concerns. `import.yml` creates infrastructure. `zerops.yml` defines what happens when code is pushed. A service can exist (imported) without any code deployed yet.

## Build/Deploy Lifecycle

Build and Run are **SEPARATE containers** with **separate base images**.

```
Source Code
    |
+--------------------------------+
|  BUILD CONTAINER               |
|  - Starts with base image ONLY |
|  - prepareCommands: cached     |
|  - buildCommands: compile      |
|  - Output: deployFiles         |
+-----------+--------------------+
            | deployFiles = THE ONLY BRIDGE
            v
+--------------------------------+
|  RUN CONTAINER                 |
|  - Different base image possible|
|  - prepareCommands: run BEFORE |
|    deploy files arrive         |
|  - Deploy files land at /var/www|
|  - start: launches the app     |
+--------------------------------+
```

**Phase ordering:**
1. `build.prepareCommands` -- install tools, cached in base layer
2. `build.buildCommands` -- compile, bundle, test
3. `build.deployFiles` -- select artifacts to transfer
4. `run.prepareCommands` -- customize runtime image (runs BEFORE deploy files arrive!)
5. Deploy files arrive at `/var/www`
6. `run.initCommands` -- per-container-start tasks (migrations)
7. `run.start` -- launch the application

**Critical**: `run.prepareCommands` executes BEFORE deploy files are at `/var/www`. Do NOT reference `/var/www/` paths in `run.prepareCommands`. Use `build.addToRunPrepare` to copy files to `/home/zerops/`, then reference `/home/zerops/` in `run.prepareCommands`.

## Networking

```
Internet -> L7 Load Balancer (SSL termination) -> container VXLAN IP:port -> app
```

- **L7 LB terminates SSL/TLS** -- all internal traffic is plain HTTP
- Apps **MUST bind `0.0.0.0`** -- binding localhost/127.0.0.1 -> 502 Bad Gateway (LB routes to VXLAN IP)
- Internal service-to-service: always `http://hostname:port` -- NEVER `https://`
- Valid port range: **10-65435** (80/443 reserved by Zerops for SSL termination; exception: PHP uses port 80)
- Cloudflare SSL must be **Full (strict)** -- "Flexible" causes infinite redirect loops

## Storage

- **Container disk**: per-container, persistent, **grow-only** (auto-scaling only increases, never shrinks; to reduce: recreate service)
- **Shared storage**: NFS mount at `/mnt/{hostname}`, POSIX-only, max 60 GB, SeaweedFS backend
- **Object storage**: S3-compatible (MinIO backend), `forcePathStyle: true` REQUIRED, region `us-east-1`, one auto-named bucket per service (immutable name)

## Scaling

- **Vertical**: CPU (shared or dedicated), RAM (dual-threshold triggers), Disk (grow-only). Applies to runtimes AND managed services. Does NOT apply to shared-storage or object-storage
- **Horizontal**: 1-10 containers for **runtimes only**. Managed services have fixed container counts: NON_HA=1, HA=3 -- do NOT set minContainers/maxContainers for managed services
- **HA mode**: fixed 3 containers with master-replica topology, auto-failover. Container count is IMMUTABLE for managed services
- **Docker**: fixed resources only (no min-max autoscaling), resource change triggers VM restart

## Immutable Decisions

These CANNOT be changed after creation -- choose correctly or delete+recreate:
- **Hostname** -- becomes internal DNS name, max 25 chars, a-z and 0-9 only
- **Mode** (HA/NON_HA) -- determines node topology (1 vs 3 containers)
- **Object storage bucket name** -- auto-generated from hostname + random prefix
- **Service type category** -- cannot change a runtime to a managed service or vice versa

## Base Image Contract

| Base | OS | Package Manager | Size | libc |
|------|----|----------------|------|------|
| Alpine (default) | Alpine Linux | `apk add --no-cache` | ~5 MB | musl |
| Ubuntu | Ubuntu | `sudo apt-get update && sudo apt-get install -y` | ~100 MB | glibc |

**NEVER cross them**: `apt-get` on Alpine -> "command not found". `apk` on Ubuntu -> "command not found".

Build containers run as user `zerops` with **sudo** access.

---

## import.yml Schema

```
project:                               # OPTIONAL (omit in ZCP context)
  name: string                         # REQUIRED if project: exists
  corePackage: LIGHT | SERIOUS         # default LIGHT
  envVariables: map<string,string>     # project-level vars
  tags: string[]

services[]:                            # REQUIRED
  hostname: string                     # REQUIRED, max 25, a-z and 0-9 ONLY (no hyphens/underscores), IMMUTABLE
  type: <runtime>@<version>            # REQUIRED (100+ valid values)
  mode: HA | NON_HA                    # MANDATORY for managed services (dry-run won't catch missing mode), IMMUTABLE
  priority: int                        # higher = starts first (DB=10, app=5)
  enableSubdomainAccess: bool          # zerops.app subdomain
  startWithoutCode: bool               # start without deploy (runtimes only)
  minContainers: 1-10                  # RUNTIME ONLY, default 1 (managed services have fixed containers)
  maxContainers: 1-10                  # RUNTIME ONLY (managed: NON_HA=1, HA=3, fixed)
  envSecrets: map<string,string>       # blurred in GUI by default, editable/deletable
  dotEnvSecrets: string                # .env format, auto-creates secrets
  # NOTE: envVariables does NOT exist at service level — only at project level
  # For non-secret env vars on a service, use zerops_env after import or zerops.yml run.envVariables
  buildFromGit: url                    # one-time build from repo
  objectStorageSize: 1-100             # GB, object-storage only (changeable in GUI later)
  objectStoragePolicy: private | public-read | public-objects-read | public-write | public-read-write
  objectStorageRawPolicy: string       # custom IAM Policy JSON (alternative to objectStoragePolicy)
  override: bool                       # triggers redeploy of existing runtime service with same hostname
  mount: string[]                      # mount shared storage services at import time
  nginxConfig: string                  # custom nginx config for PHP/static/nginx services
  zeropsSetup: string                  # inline zerops.yml setup name
  zeropsYaml: object                   # inline zerops.yml configuration in import
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
```

### Preprocessor Functions
Enable with `#yamlPreprocessor=on` as first line. Syntax: `<@function(<args>)>`, chain modifiers with `|`: `<@generateRandomString(<32>)|sha256>`.

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

## zerops.yml Schema

```
zerops[]:
  setup: string                        # REQUIRED, matches service hostname
  build:
    base: string | string[]            # runtime(s) -- multi-base: [php@8.4, nodejs@18]
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
    start: string                      # REQUIRED for runtime services
    ports[]: { port: 10-65435, httpSupport: bool, protocol: tcp|udp }
    initCommands: string[]             # every container start (migrations, seeding)
    prepareCommands: string[]          # runtime image customization
    documentRoot: string               # webserver runtimes only (PHP/Nginx/Static)
    healthCheck: { httpGet | exec }    # 2xx or exit 0, 5-min retry window
    envVariables: map<string, string|number|bool>
    crontab[]: { timing: cron, command: string, allContainers: bool }
    routing: { cors, redirects[], headers[] }
    startCommands[]: { command, name, workingDir, initCommands[] }
```

---

## Rules & Pitfalls

### Networking
- **ALWAYS** bind `0.0.0.0` (all interfaces). REASON: L7 LB routes to VXLAN IP, not localhost. Binding 127.0.0.1 -> 502 Bad Gateway
- **ALWAYS** use `http://` for internal service-to-service communication. REASON: SSL terminates at the LB; internal traffic is plain HTTP over VXLAN
- **NEVER** listen on port 443 or 80 (exception: PHP uses 80). REASON: Zerops reserves 80/443 for SSL termination. Use 3000, 8080, etc.
- **ALWAYS** use port range 10-65435. REASON: ports outside this range are reserved by the platform
- **NEVER** use `https://` for internal service URLs. REASON: no TLS certificates exist on internal network; connection will fail
- **ALWAYS** set Cloudflare SSL to "Full (strict)" when using Cloudflare proxy. REASON: "Flexible" causes infinite redirect loops

### Build & Deploy
- **ALWAYS** specify `deployFiles` in zerops.yml. REASON: nothing auto-deploys; build artifacts don't transfer to run container without explicit specification
- **ALWAYS** include `node_modules` in `deployFiles` for Node.js apps (unless bundled). REASON: runtime container doesn't run `npm install`
- **ALWAYS** deploy fat/uber JARs for Java. REASON: build and run are separate containers; thin JARs lose their dependencies
- **ALWAYS** use Maven/Gradle wrapper (`./mvnw`, `./gradlew`) or install build tools via `prepareCommands`. REASON: build container has JDK only -- Maven, Gradle are NOT pre-installed
- **NEVER** reference `/var/www/` in `run.prepareCommands`. REASON: deploy files arrive AFTER prepareCommands execute; `/var/www` is empty during prepare
- **ALWAYS** use `addToRunPrepare` + `/home/zerops/` path for files needed in `run.prepareCommands`. REASON: this is the only way to get files from build into the prepare phase
- **ALWAYS** match `deployFiles` layout to `run.start` path. Two valid patterns for build output directories:
  - **Directory preserved** (NO tilde): `deployFiles: [dist, package.json]` -> files at `/var/www/dist/` -> `start: bun dist/index.js`
  - **Contents extracted** (WITH tilde): `deployFiles: dist/~` -> files at `/var/www/` -> `start: bun index.js`
  REASON: tilde strips the directory prefix. If start command references the subdirectory (e.g., `dist/index.js`), tilde BREAKS it because the file is at `/var/www/index.js`, not `/var/www/dist/index.js`. Use tilde for static sites (no start command) or when start command matches the flattened layout
- **NEVER** use `initCommands` for package installation. REASON: initCommands run on every container restart; use `prepareCommands` for one-time setup
- **ALWAYS** use `--no-cache-dir` for pip in containers. REASON: prevents wasted disk space on ephemeral containers
- **ALWAYS** use `--ignore-platform-reqs` for Composer on Alpine. REASON: musl libc may not satisfy platform requirements checks
- **ALWAYS** require a git repository before `zerops_deploy`. REASON: `zcli push` requires git. Run `git init && git add -A && git commit -m "deploy"` first

### Base Image & OS
- **NEVER** use `apt-get` on Alpine. REASON: Alpine uses `apk`; apt-get doesn't exist. "command not found"
- **NEVER** use `apk` on Ubuntu. REASON: Ubuntu uses `apt-get`; apk doesn't exist
- **ALWAYS** use `sudo apk add --no-cache` on Alpine. REASON: prevents stale package index caching; sudo required as containers run as `zerops` user
- **ALWAYS** use `sudo apt-get update && sudo apt-get install -y` on Ubuntu. REASON: package index not pre-populated; sudo required as containers run as `zerops` user
- **NEVER** set `run.base: alpine@*` for Go. REASON: causes glibc/musl mismatch for CGO-linked binaries -> 502. Omit `run.base` or use `run.base: go@latest`
- **ALWAYS** use `os: ubuntu` for Deno and Gleam. REASON: these runtimes are not available on Alpine

### Environment Variables
- **NEVER** re-reference project-level env vars in service vars. REASON: project vars are auto-inherited; creating a service var with the same name shadows the project var
- **ALWAYS** use `envSecrets` for passwords, tokens, API keys. REASON: blurred in GUI by default, proper security practice
- **ALWAYS** use cross-service reference syntax `${hostname_varname}` (dashes->underscores). REASON: this is the only way to wire services; direct values break on service recreation
- **NEVER** rely on GUI password changes updating env vars. REASON: changing DB password in GUI does NOT update connection string env vars (manual sync required)
- import.yml service level: ONLY `envSecrets` and `dotEnvSecrets`. No `envVariables` (project-level only)
- zerops.yml: `build.envVariables` and `run.envVariables` exist (visible in GUI)
- Managed services auto-generate credentials (hostname, port, user, password, dbName, connectionString) -- do NOT set these in import.yml
- Cross-phase: build->run `${BUILD_MYVAR}`, run->build `${RUNTIME_MYVAR}`
- Keys: alphanumeric + `_`, case-sensitive. Values: ASCII only

### Import & Service Creation
- **ALWAYS** use `valkey@7.2` (not `valkey@8`). REASON: v8 passes dry-run validation but fails actual import
- **ALWAYS** set explicit `mode: NON_HA` or `mode: HA` for managed services (DB, cache, shared-storage). REASON: omitting mode passes dry-run but fails real import with "Mandatory parameter is missing"
- **NEVER** set `mode` for runtime services. REASON: `mode` is only for managed services. Runtime HA is achieved via `minContainers: 2+` (horizontal scaling). Setting `mode: HA` on a runtime is meaningless
- **NEVER** set `minContainers`/`maxContainers` for managed services. REASON: managed services have fixed container counts (NON_HA=1, HA=3); setting these causes import failure
- **NEVER** set `verticalAutoscaling` for shared-storage or object-storage. REASON: these service types don't support vertical scaling; setting it causes import failure
- **ALWAYS** set `priority: 10` for databases/storage services. REASON: ensures they start before application services that depend on them
- **ALWAYS** use `enableSubdomainAccess: true` in import.yml instead of calling `zerops_subdomain` after. REASON: calling subdomain API on READY_TO_DEPLOY service returns an error
- **NEVER** use Docker `:latest` tag. REASON: cached and won't re-pull; always use specific version tags
- **ALWAYS** use `--network=host` for Docker services. REASON: without it, container cannot receive traffic from Zerops routing
- **ALWAYS** use `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` for Object Storage. REASON: MinIO backend doesn't support virtual-hosted style

### Import Generation
- **ALWAYS** create dev/stage service pairs for runtime services. Naming: `{prefix}dev` and `{prefix}stage` (e.g., `appdev`/`appstage`, `apidev`/`apistage`). REASON: workflow engine detects conformant projects by this pattern; single services have no isolation
- **ALWAYS** set `startWithoutCode: true` on dev services when using SSHFS-based development. REASON: without it OR `buildFromGit`, service stays stuck in READY_TO_DEPLOY (empty container, no code)
- **ALWAYS** set `buildFromGit: <url>` OR `startWithoutCode: true` on every runtime service. REASON: runtime services without either have no code source -- they cannot start. This is the #1 import failure
- **ONLY** set `zeropsSetup` when using `buildFromGit` and the zerops.yml setup name differs from the hostname. REASON: zeropsSetup defaults to hostname. With `startWithoutCode`, omit it -- when code is later pushed via `zcli push`, the zerops.yml `setup:` must match the hostname (or use `zcli push --setup <name>`)
- **ALWAYS** set `verticalAutoscaling.minRam: 1.0` (GB) for runtime services. REASON: 0.5 GB causes OOM during compilation (especially Go, Java). 1.0 is the safe minimum
- **ALWAYS** use managed service hostname conventions: `db` (postgresql/mariadb), `cache` (valkey), `queue` (rabbitmq/nats), `search` (elasticsearch), `storage` (object-storage/minio). REASON: standardizes cross-service references and discovery
- **ALWAYS** create managed services with `priority: 10` and runtime services with lower priority (default or `priority: 5`). REASON: databases must be ready before apps that depend on them
- **ALWAYS** match zerops.yml `setup:` to the service hostname (e.g., `setup: evalappjava` for hostname `evalappjava`). REASON: `zcli push` defaults to hostname as setup name. If setup doesn't match hostname, deploy fails with "setup not found". For dev/stage pairs, use `setup: <hostname>` per service, not generic names like `dev`/`prod`
- **ALWAYS** prefer `enableSubdomainAccess: true` in import.yml over calling `zerops_subdomain` after import. REASON: calling subdomain API on a READY_TO_DEPLOY service errors; the import flag activates after first deploy

### Runtime-Specific
- **ALWAYS** set `server.address=0.0.0.0` for Java Spring Boot. REASON: Spring Boot defaults to localhost binding -> unreachable from LB
- **ALWAYS** set `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` for PHP Laravel. REASON: reverse proxy breaks CSRF validation without trusted proxy config
- **ALWAYS** set `CSRF_TRUSTED_ORIGINS` for Django behind proxy. REASON: reverse proxy changes the origin header; Django blocks requests
- **ALWAYS** set `PHX_SERVER=true` for Phoenix/Elixir releases. REASON: without it, the HTTP server doesn't start in release mode
- **ALWAYS** use `cargo b --release` for Rust. REASON: debug builds are 10-100x slower
- **ALWAYS** use `CGO_ENABLED=0 go build` when unsure about CGO dependencies. REASON: produces static binary compatible with any base
- **ALWAYS** use `sudo apk add --no-cache php84-<ext>` for Alpine PHP extensions. REASON: version prefix must match PHP major+minor (e.g., php84-redis for PHP 8.4); sudo required

### Scaling & Platform
- **NEVER** attempt to change HA/NON_HA mode after creation. REASON: mode is immutable; must delete and recreate service
- **NEVER** attempt to change hostname after creation. REASON: hostname is immutable; it becomes the internal DNS name
- **NEVER** expect disk to shrink. REASON: auto-scaling only increases disk; to reduce, recreate the service
- **ALWAYS** use `zsc execOnce <key> -- <cmd>` for migrations in HA. REASON: prevents duplicate execution across multiple containers
- **NEVER** modify `zps`/`zerops`/`super` system users in managed services. REASON: these are system maintenance accounts

### Event Monitoring
- **ALWAYS** filter `zerops_events` by `serviceHostname`. REASON: project-level events include stale builds from other services
- **NEVER** keep polling after `stack.build` shows `FINISHED`. REASON: FINISHED means build is complete; `appVersion` ACTIVE means deployed and running
- **ALWAYS** check `stack.build` process for build status, NOT `appVersion`. REASON: these are different events; `appVersion` ACTIVE != still building

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

## Causal Chains

| Action | Effect | Root Cause |
|--------|--------|------------|
| Bind localhost | 502 Bad Gateway | LB routes to VXLAN IP, not localhost |
| Deploy thin JAR | ClassNotFoundException | Build!=Run containers; dependencies not in artifact |
| `apt-get` on Alpine | "command not found" | Alpine uses `apk`, not `apt-get` |
| Reference `/var/www` in `run.prepareCommands` | File not found | Deploy files arrive AFTER prepareCommands |
| `npm install` only in build | Missing modules at runtime | Build container discarded; `node_modules` must be in `deployFiles` |
| Bare `mvn` in buildCommands | "command not found" | Build image has JDK only; Maven not pre-installed |
| `valkey@8` in import | Import fails | Only `valkey@7.2` is valid |
| No `mode` for managed service | Import fails | Managed services require explicit `mode: NON_HA` or `mode: HA` |
| Set `minContainers` for PostgreSQL | Import fails | Managed services have fixed container counts |
| `build.base: php-nginx@8.3` | "unknown base php-nginx@8.3" | Webserver variants (`php-nginx`, `php-apache`) are run bases only; use `build.base: php@8.3` + `run.base: php-nginx@8.3` |
| `deployFiles: dist/~` + `start: bun dist/index.js` | App crashes / file not found | Tilde extracts `dist/` contents to `/var/www/`, so `index.js` is at `/var/www/index.js`, not `/var/www/dist/index.js`. Either remove tilde OR change start to `bun index.js` |

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
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1.0
  - hostname: appstage
    type: nodejs@22
    startWithoutCode: true
    enableSubdomainAccess: true
```
zerops.yml must have `setup: appdev` and `setup: appstage` blocks matching hostnames.

**Full-Stack Dev/Stage (App + DB + Cache + Storage):**
```yaml
#yamlPreprocessor=on
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
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1.0
  - hostname: appstage
    type: bun@1.2
    startWithoutCode: true
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
zeropsSetup omitted — defaults to hostname `app`, so zerops.yml needs `setup: app`.
