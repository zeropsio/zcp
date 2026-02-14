# Zerops Grammar

## Keywords
zerops, grammar, import.yml, zerops.yml, schema, build, deploy, run, networking, ports, binding, 0.0.0.0, environment variables, scaling, autoscaling, yaml, pipeline, hostname, service, container, project, ssl, tls, https, http, cache, prepareCommands, buildCommands, deployFiles, tilde, mode, HA, NON_HA, cron, health check, readiness check

## TL;DR
Schema-derived universal grammar for Zerops. All runtimes share 95% identical structure — import.yml defines topology, zerops.yml defines lifecycle. This file covers the universal schema plus behavioral rules the schema cannot express.

---

## Architecture

**Hierarchy**: Project → Service → Container

- Isolated **VXLAN private network** per project — services communicate by hostname, no cross-project traffic
- **Full Linux containers** via Incus — each has SSH access, working dir `/var/www`
- Two YAML files: `import.yml` (topology) + `zerops.yml` (lifecycle)

### Traffic Flow

```
Internet → L7 Load Balancer (SSL termination) → container VXLAN IP:port → app
```

The **L7 load balancer** sits in front of all runtime services. It terminates SSL/TLS and forwards plain HTTP to the container's private VXLAN network IP. The balancer does NOT connect via localhost — it routes to the container's network interface. Therefore:

- Apps **must bind to `0.0.0.0`** (all interfaces) so the balancer can reach them
- Binding to `localhost`/`127.0.0.1` makes the app unreachable → **502 Bad Gateway**
- Internal service-to-service traffic is always plain `http://` over VXLAN — never `https://`
- Service discovery: `http://<hostname>:<port>` (e.g., `http://db:5432`)

### Core Plans

| Feature | Lightweight | Serious |
|---------|------------|---------|
| Build time | 15 hours | 150 hours |
| Backup storage | 5 GB | 25 GB |
| Egress | 100 GB | 3 TB |
| Upgrade | $10 one-time, ~35s downtime, CANNOT downgrade |

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
  mode: HA | NON_HA                    # MANDATORY for managed services, IMMUTABLE
  priority: int                        # higher = starts first (DB=10, app=5)
  enableSubdomainAccess: bool          # zerops.app subdomain
  startWithoutCode: bool               # start without deploy (runtimes only)
  minContainers: 1-10                  # RUNTIME ONLY, default 1 (managed services have fixed containers)
  maxContainers: 1-10                  # RUNTIME ONLY (managed: NON_HA=1, HA=3, fixed)
  envSecrets: map<string,string>       # masked in GUI, cannot view after creation
  envVariables: map<string,string>     # visible in GUI
  dotEnvSecrets: string                # .env format, auto-creates secrets
  buildFromGit: url                    # one-time build from repo
  objectStorageSize: 1-100             # GB, object-storage only
  objectStoragePolicy: private | public-read | public-objects-read | public-write | public-read-write
  verticalAutoscaling:                 # RUNTIME + DB/CACHE ONLY (not shared-storage, not object-storage)
    cpuMode: SHARED | DEDICATED        # default SHARED
    minCpu/maxCpu: int                 # CPU threads
    startCpuCoreCount: int             # CPU at container start
    minRam/maxRam: float               # GB
    minFreeRamGB: float                # absolute free threshold
    minFreeRamPercent: float            # percentage free threshold
    minDisk/maxDisk: float              # GB, disk never shrinks
```

### Preprocessor Functions
Enable with `#yamlPreprocessor=on` as first line:
- `${random(N)}`, `${randomInt(min, max)}` — random values
- `${sha256(value)}`, `${bcrypt(value, rounds)}`, `${argon2id(value)}` — hashing
- `${jwt(algorithm, secret, payload)}` — JWT generation
- `${generateRSAKeyPair(bits)}`, `${generateEd25519KeyPair()}` — key pairs
- Values generated once at import — fixed after, not regenerated

---

## zerops.yml Schema

```
zerops[]:
  setup: string                        # REQUIRED, matches service hostname
  build:
    base: string | string[]            # runtime(s) — multi-base: [php@8.4, nodejs@18]
    os: alpine | ubuntu                # default alpine
    prepareCommands: string[]          # cached in base layer
    buildCommands: string[]            # runs every build
    deployFiles: string | string[]     # MANDATORY — nothing auto-deploys
    cache: bool | string | string[]    # paths to cache
    addToRunPrepare: string | string[] # copy files from build to run container
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

## Platform Rules

### 1. Binding & Networking
See **Traffic Flow** above — bind `0.0.0.0`, internal traffic always `http://`.

### 2. Port Rules
Valid range **10-65435** — ports 80/443 reserved by Zerops for SSL termination. Exception: PHP uses port 80. `httpSupport: true` for HTTP, `protocol: tcp|udp` for non-HTTP. NEVER `protocol: HTTP`.

### 3. Deploy Semantics
- Build and run are **SEPARATE containers** — build output doesn't appear in run
- **Tilde syntax**: `dist/~` extracts contents to `/var/www/` (not `/var/www/dist/`)
- Without tilde: `dist` → `/var/www/dist/` (nested)
- All files land in `/var/www`
- **Git required**: `zerops_deploy` uses `zcli push` which requires a git repository. Before deploying, run `git init && git add -A && git commit -m "deploy"` in the working directory. Configure git identity if needed: `git config user.email "deploy@zerops.io" && git config user.name "Deploy"`

### 4. Cache Architecture (Two-Layer)
- **Base layer**: OS + prepareCommands (invalidated only when prepareCommands change)
- **Build layer**: buildCommands output (invalidated every build)
- `cache: false` only affects `/build/source` — modules elsewhere remain cached

### 5. Environment Variables
- **envSecrets**: passwords, tokens (masked) | **envVariables**: config (visible)
- Cross-service ref: `${hostname_varname}` — dashes→underscores
- Project vars auto-inherited — do NOT re-reference (creates shadow)
- Cross-phase: build→run `${BUILD_MYVAR}`, run→build `${RUNTIME_MYVAR}`
- Keys: alphanumeric + `_`, case-sensitive. Values: ASCII only

### 6. Mode Immutability
**HA/NON_HA set at creation, CANNOT change** — determines node topology (1 vs 3). Must delete and recreate. Omitting mode causes import failure.

### 7. Service Lifecycle
- Hostname **IMMUTABLE** after creation
- Runtime services start `READY_TO_DEPLOY` (unless `startWithoutCode: true`)
- Managed services start `ACTIVE` immediately
- 10 most recent versions kept, older auto-deleted
- CI skip: `ci skip` or `skip ci` in commit message
- **Build completion detection**: In `zerops_events`, a build is done when the `process` event with action `stack.build` shows status `FINISHED`. The `build` event (appVersion) shows status `ACTIVE` which means deployed and running — NOT still building. Do NOT keep polling if `stack.build` process is `FINISHED`. If build FAILED, the process status will be `FAILED`

### 8. Public Access
- **Shared IPv4**: free, HTTP/HTTPS only, requires BOTH A and AAAA DNS records
- **Dedicated IPv4**: $3/30 days, all protocols
- **IPv6**: free, dedicated per project
- **zerops.app subdomain**: 50MB limit, not production. Use `enableSubdomainAccess: true` in import.yml — do NOT call zerops_subdomain separately after import (fails on READY_TO_DEPLOY services)

### 9. Scaling
- **Vertical**: CPU (shared 1/10-10/10 or dedicated), RAM (dual-threshold: minFreeRamGB OR minFreeRamPercent), Disk (grows only, never shrinks). Applies to runtimes and DB/cache services. Do NOT set verticalAutoscaling for shared-storage or object-storage in import.yml (causes import failure)
- **Horizontal**: 1-10 containers for runtimes only. Managed services (DB, cache, storage) have fixed containers (NON_HA=1, HA=3) — do NOT set minContainers/maxContainers for them in import.yml
- Docker: fixed resources, no min-max, restart on change

### 10. Build Limits
CPU: 1-5 cores, RAM: 8 GB fixed, Disk: 1-100 GB, Time: 60 minutes

### 11. zsc Commands
- `zsc execOnce <key> -- <cmd>`: run once across all containers (HA-safe migrations)
- `zsc add <runtime>@<version>`: install additional runtime in prepareCommands

---

## See Also
- zerops://foundation/runtimes — runtime-specific deltas
- zerops://foundation/services — managed service reference
- zerops://foundation/wiring — cross-service wiring templates
