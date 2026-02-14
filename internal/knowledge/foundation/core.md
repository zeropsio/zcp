# Zerops Fundamentals

## Keywords
zerops, core, principles, architecture, import.yml, zerops.yml, build, deploy, run, networking, ports, binding, 0.0.0.0, environment variables, scaling, autoscaling, yaml, pipeline, vxlan, L7 balancer, hostname, service, container, project, ssl, tls, https, http, cache, prepareCommands, buildCommands, deployFiles, tilde, mode, HA, NON_HA, cron, health check, readiness check

## TL;DR
Everything you need to understand Zerops and generate correct infrastructure. Zerops is a developer-first PaaS using full Linux containers (Incus), private VXLAN networking, and L7 load balancing with automatic SSL termination.

---

## 1. Architecture

**Hierarchy**: Project → Service → Container

- Each project gets an **isolated VXLAN private network** — services communicate by hostname within the project, no cross-project communication
- **L7 load balancer** handles SSL termination, routing, and health checking — apps never handle SSL directly
- **Full Linux containers** via Incus (not Docker, not serverless) — each container is a complete OS with SSH access
- Working directory: always `/var/www` for both build and run containers
- Two YAML files define everything:
  - `import.yml` — topology (what services exist, how they scale)
  - `zerops.yml` — lifecycle (how to build, deploy, and run each service)

### Core Plans

| Feature | Lightweight | Serious |
|---------|------------|---------|
| Build time | 15 hours | 150 hours |
| Backup storage | 5 GB | 25 GB |
| Egress | 100 GB | 3 TB |
| Upgrade | $10 one-time, ~35s network downtime, CANNOT downgrade |

---

## 2. Service Topology (import.yml)

### Structure
```yaml
project:                            # OPTIONAL when adding to existing project
  name: <name>                      # REQUIRED if project: exists
  corePackage: LIGHT                # LIGHT (default) or SERIOUS

services:                           # REQUIRED: list of services
  - hostname: <name>                # REQUIRED: max 25 chars, lowercase a-z/0-9, IMMUTABLE after creation
    type: <type>@<version>          # REQUIRED: see version table
    mode: HA                        # MANDATORY for databases/caches: NON_HA or HA
    priority: 10                    # Higher = starts first (DB=10, app=5)
    enableSubdomainAccess: true     # Pre-configure *.zerops.app subdomain
    startWithoutCode: true          # Start immediately without deploy (runtimes only)
    envSecrets: {...}               # Masked in GUI, generated at import time
    envVariables: {...}             # Visible in GUI
    dotEnvSecrets: |                # .env format, auto-creates secrets
      KEY=value
    buildFromGit: <url>             # One-time build from repo
    minContainers: 1
    maxContainers: 10
    verticalAutoscaling:
      cpuMode: SHARED               # SHARED or DEDICATED
      minCpu: 1
      maxCpu: 5
      startCpuCoreCount: 2          # CPU cores at container start
      minRam: 0.5
      maxRam: 4
      minFreeRamGB: 0.5             # Absolute free RAM threshold (GB)
      minFreeRamPercent: 50         # % of granted RAM that must stay free
      minDisk: 1
      maxDisk: 20
```

### Hostname Rules
- Immutable after creation — becomes internal DNS name
- Max 25 characters, lowercase letters, digits, hyphens
- Becomes the DNS name for internal service discovery

### Mode Rule (CRITICAL)
- **MANDATORY** for all managed services (databases, caches, shared-storage)
- **IMMUTABLE** after creation — cannot switch NON_HA↔HA, must delete and recreate
- WHY: mode determines node topology at creation time (1 node for NON_HA, 3 nodes for HA)
- Omitting mode passes dry-run but fails real import with "Mandatory parameter is missing"
- Services requiring mode: postgresql, mariadb, valkey, keydb, elasticsearch, clickhouse, qdrant, typesense, kafka, nats, meilisearch, shared-storage

### Preprocessor Functions
Enable with `#yamlPreprocessor=on` as first line:
- `${random(N)}` — N-character random string
- `${randomInt(min, max)}` — random integer in range
- `${sha256(value)}`, `${bcrypt(value, rounds)}`, `${argon2id(value)}` — hashing
- `${jwt(algorithm, secret, payload)}` — JWT (HS256, HS384, HS512, RS256, RS384, RS512)
- `${generateRSAKeyPair(bits)}`, `${generateEd25519KeyPair()}` — key pairs
- Values generated once at import time — fixed after import, not regenerated

### envSecrets vs envVariables
- **envSecrets**: Sensitive data (passwords, tokens) — masked in GUI, cannot view after creation
- **envVariables**: Configuration (URLs, flags) — visible in GUI

### No project: Section in ZCP Context
When using ZCP (ZAIA), import adds services to the existing project — omit the `project:` section entirely.

---

## 3. Build Pipeline (zerops.yml)

### Structure
```yaml
zerops:
  - setup: <service-hostname>      # REQUIRED: matches service hostname
    build:
      base: <runtime>@<version>     # REQUIRED if build: exists (or array for multi-base)
      os: alpine                    # alpine (default) or ubuntu
      prepareCommands: [...]        # Cached in base layer
      buildCommands: [...]          # Runs every build
      deployFiles: [...]            # MANDATORY: what to deploy
      cache: [...]                  # Paths to cache (or true/false)
      addToRunPrepare: [...]        # Copy files from build to run container
    deploy:
      temporaryShutdown: false      # false = zero-downtime (default)
      readinessCheck: ...           # httpGet or exec, gates traffic switch
    run:
      base: <runtime>@<version>     # If different from build base
      start: <command>              # REQUIRED for runtime services
      ports: [...]                  # Range: 10-65435
      initCommands: [...]           # Runs on every container start
      prepareCommands: [...]        # Runtime image customization
      healthCheck: ...              # httpGet or exec
      envVariables: {...}
      crontab: [...]                # Standard cron syntax
      documentRoot: <path>          # PHP/Nginx/Static only
```

### Three Build Phases
1. **prepareCommands** → install system deps, cached in base layer
2. **buildCommands** → compile/bundle, runs every build
3. **deployFiles** → what gets deployed (MANDATORY)

WHY separate: different cache layers, different purposes.

### Cache Architecture (Two-Layer)
- **Base layer**: OS + prepareCommands output (invalidated only when prepareCommands change)
- **Build layer**: buildCommands output (invalidated on every build)
- WHY: prepareCommands are expensive (OS packages, system deps), build commands are fast
- `cache: false` only affects `/build/source` — files elsewhere (Go modules in `~/go`, pip in `/usr/lib/python`) remain cached
- buildFromGit caches, local push doesn't

### deployFiles (CRITICAL)
- **MANDATORY** — nothing auto-deploys, must list explicitly
- WHY: build and run are SEPARATE containers — build output doesn't automatically appear in run container
- **Tilde syntax**: `dist/~` extracts contents to `/var/www/` (not `/var/www/dist/`)
- Without tilde: `dist` → `/var/www/dist/` (nested directory)
- All files land in `/var/www`

### addToRunPrepare
- Listed under `build:` section (NOT `run:`)
- Copies files from build container into run container's base image
- WHY: some runtimes need build artifacts in the run image (e.g., Python pip packages)

### Build Container Limits
- CPU: 1-5 cores (scales as needed)
- RAM: 8 GB (fixed)
- Disk: 1-100 GB
- Time limit: 1 hour (build terminated after 60 minutes)

### Multi-Base Build
`base: [php@8.4, nodejs@18]` — install multiple runtimes in single build container.

### Application Versions
- Keeps 10 most recent versions, older auto-deleted
- Can restore any previous version

### Build ≠ Run Base
Build base and run base can differ:
- PHP: build `php@8.4`, run `php-nginx@8.4`
- SSG: build `nodejs@22`, run `static`
- Elixir: build `elixir@1.16`, run `alpine@latest`
- SSR: build and run same base (e.g., `nodejs@22`)

---

## 4. Runtime Configuration (zerops.yml run section)

### start Command
- Required for all runtime services
- Single command that starts the application
- Runs as PID 1 in the container

### initCommands
- Runs on every container start (including restarts and scaling events)
- NOT for package installation — use prepareCommands for that
- Good for: migrations (`zsc execOnce`), seeding, cache warming

### prepareCommands (run section)
- Runtime image customization — install system packages for the run container
- Different from build prepareCommands — these customize the run environment

### Health Check
- `httpGet`: URL must return 2xx (5s timeout, follows 3xx redirects)
- `exec.command`: must return exit code 0 (5s timeout)
- Retry window: 5 minutes, then container marked as failed

### Readiness Check (deploy section)
- Gates traffic switch during deployment
- Same format as health check (httpGet or exec)

### documentRoot
- PHP, Nginx, and Static services only
- Subdirectory of `/var/www` to serve as web root

### zsc Commands (Init & Migrations)
```yaml
run:
  initCommands:
    - zsc execOnce migrate -- npm run db:migrate
    - zsc execOnce seed -- npm run db:seed
```
- `execOnce`: runs exactly once across all containers (HA-safe, prevents duplicate migrations)
- `-r, --retryUntilSuccessful`: retry until success
- Key must be unique per execution
- `zsc add <runtime>@<version>`: install additional runtime in prepareCommands

---

## 5. Networking

### THE Binding Rule
**Bind to `0.0.0.0`, NEVER `localhost` or `127.0.0.1`**

WHY: The L7 load balancer routes traffic to the container's IP address. Binding to `localhost` makes the app unreachable from the balancer, resulting in **502 Bad Gateway**.

Framework-specific binding:
- Node.js/Express: `app.listen(port, "0.0.0.0")`
- Bun: `Bun.serve({ hostname: "0.0.0.0", port })`
- Deno: `Deno.serve({ hostname: "0.0.0.0", port }, handler)`
- Python/uvicorn: `--host 0.0.0.0`
- Python/gunicorn: `--bind 0.0.0.0:8000`
- Java Spring: `server.address=0.0.0.0`
- .NET Kestrel: `ASPNETCORE_URLS=http://0.0.0.0:5000`
- Go: default `:port` binds all interfaces (correct)
- PHP: Nginx handles binding (no config needed)

### Port Rules
- **Valid range: 10-65435** — ports 80 and 443 reserved by Zerops for SSL termination
- Exception: PHP services use port 80 (the only exception)
- Port syntax in zerops.yml:
  - `httpSupport: true` for HTTP ports
  - `protocol: TCP` or `protocol: UDP` for non-HTTP ports
  - NEVER `protocol: HTTP` — this is invalid

### Internal Communication
- **ALWAYS `http://`, NEVER `https://`** internally
- WHY: SSL terminates at the L7 balancer — internal traffic between services is plain HTTP over the private VXLAN network
- Service discovery: `http://hostname:port` (hostname = service name from import.yml)
- NEVER listen on port 443 — app listens on standard port (3000, 8080, etc.), Zerops handles SSL

### Public Access
- **Shared IPv4**: free, HTTP/HTTPS only, requires BOTH A and AAAA DNS records for SNI routing
- **Dedicated IPv4**: $3/30 days, all protocols (TCP/UDP), auto-renews, non-refundable
- **IPv6**: free, dedicated per project, all protocols
- **zerops.app subdomain**: 50MB upload limit, not for production — use `enableSubdomainAccess: true` in import.yml
- **Custom domain**: 512MB upload limit, production-ready

### Platform Variables
- `TRUSTED_PROXIES`: configure for Django/Laravel behind L7 balancer (CSRF validation) — `"127.0.0.1,10.0.0.0/8"`
- `AWS_USE_PATH_STYLE_ENDPOINT: true`: REQUIRED for Zerops Object Storage (MinIO)
- `PHX_SERVER: true`: REQUIRED for Elixir Phoenix releases

---

## 6. Environment Variables

### Scopes
- **Service variables**: only available to that specific service
- **Project variables**: auto-inherited by ALL services — do NOT re-reference (creates shadow)

### Cross-Service Reference
- Syntax: `${servicename_variablename}`
- Dashes become underscores: hostname `my-db` → `${my_db_password}`
- Use hostname directly for connections: `db:5432`, NOT `${db_hostname}:5432` (both work, direct is simpler)

### Cross-Phase Access
- Build → Runtime: `${BUILD_MYVAR}` in run section
- Runtime → Build: `${RUNTIME_MYVAR}` in build section

### Auto-Generated Variables
- `${zeropsSubdomain}` — service subdomain
- `${appVersionId}` — unique deployment version ID

### Restrictions
- Keys: alphanumeric + `_` only, case-sensitive
- Values: ASCII only, no EOL characters, no UTF-8

### Precedence
1. Service variables (highest)
2. Project variables
3. Build/runtime vars override secrets with same name

### Gotchas
- Project vars auto-inherited — re-referencing creates service var that shadows project var
- Password sync is manual — changing DB password in GUI doesn't update env vars
- Env vars not available via VPN — use GUI or API to read them

---

## 7. Scaling

### Vertical (Resources)

**CPU:**
- Shared: 1/10 to 10/10 physical core (shared with ≤10 apps)
- Dedicated: exclusive physical core access
- Mode change: once per hour (SHARED↔DEDICATED)
- `startCpuCoreCount`: CPU cores at container start (default 2)

**RAM:**
- Dual-threshold mechanism — scales based on the HIGHER of two thresholds:
  - `minFreeRamGB`: absolute minimum free RAM (e.g., 0.5 GB)
  - `minFreeRamPercent`: percentage of granted RAM that must stay free
- Default min free: 0.0625 GB (64 MB)

**Disk:**
- Can only grow — never shrinks automatically
- To reduce disk: recreate the service

### Horizontal (Containers)
- Range: 1-10 containers for runtimes, Linux containers, Docker
- Databases/caches: fixed count, set at creation, HA = 3 nodes (immutable)
- App must be stateless/HA-ready for horizontal scaling

### HA Mode
- IMMUTABLE after creation — cannot switch NON_HA↔HA, must delete and recreate
- WHY: determines node topology at creation time (1 vs 3 nodes)
- HA recovery: failed container disconnected → new created → data synced → old removed

### Docker Scaling Exception
Docker services use fixed resources (no min-max ranges). Changing resources triggers VM restart.

---

## 8. Service Lifecycle

- **Hostname immutable** after creation — cannot change, must delete and recreate
- **Runtime services** start in `READY_TO_DEPLOY` state (unless `startWithoutCode: true`)
- **Managed services** (databases, caches, storage) start `ACTIVE` immediately
- **10 most recent versions** kept, older auto-deleted
- **CI skip**: include `ci skip` or `skip ci` in commit message to skip pipeline trigger

### Deploy Patterns
- **Single-base**: build and run use same runtime (Node.js SSR, PHP, Python, Java)
- **Multi-base (Build→Static)**: build on full runtime, run on static (React SPA, Vue SPA, SSG)
- **Multi-runtime (Compiled→Alpine)**: build on language runtime, run on alpine (Elixir, Rust, Go)

---

## See Also
- zerops://foundation/runtimes — runtime-specific exceptions
- zerops://foundation/services — managed service reference + wiring
