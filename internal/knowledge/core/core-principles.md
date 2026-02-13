# Zerops Core Principles

## Keywords
zerops, core, universal, yaml, import, build, deploy, run, networking, environment variables, scaling, ports

## TL;DR
Universal Zerops rules that apply to ALL runtimes and services. Organized by decision point for YAML generation.

---

## YAML Structure

### zerops.yml
```yaml
zerops:
  - setup: <service-hostname>      # REQUIRED: matches service in project
    build:                          # OPTIONAL: omit for services without build
      base: <runtime>@<version>     # REQUIRED if build: exists
      os: alpine                    # alpine (default) or ubuntu
      prepareCommands: [...]        # Cached in base layer
      buildCommands: [...]          # Runs every build
      deployFiles: [...]            # MANDATORY: what to deploy
      cache: [...]                  # Paths to cache (or true/false)
      addToRunPrepare: [...]        # Copy from build to run container
    deploy:                         # OPTIONAL
      temporaryShutdown: false      # false = zero-downtime (default)
      readinessCheck: ...           # httpGet or exec
    run:                            # REQUIRED
      base: <runtime>@<version>     # If different from build base
      start: <command>              # REQUIRED for runtime services
      ports: [...]                  # Range: 10-65435
      initCommands: [...]           # Runs on every container start
      prepareCommands: [...]        # Runtime image customization
      healthCheck: ...              # httpGet or exec
      envVariables: {...}
```

### import.yml
```yaml
project:                            # OPTIONAL when adding to existing project
  name: <name>                      # REQUIRED if project: exists
  corePackage: LIGHT                # LIGHT (default) or SERIOUS

services:                           # REQUIRED: list of services
  - hostname: <name>                # REQUIRED: max 25 chars, lowercase a-z/0-9
    type: <type>@<version>          # REQUIRED: see version table
    mode: HA                        # MANDATORY for databases/caches: NON_HA or HA
    priority: 10                    # Higher = starts first (DB=10, app=5)
    enableSubdomainAccess: true     # Pre-configure *.zerops.app subdomain
    startWithoutCode: true          # Start immediately without deploy (runtimes only)
    envSecrets: {...}               # Masked in GUI
    dotEnvSecrets: |                # .env format, auto-creates secrets
      KEY=value
    buildFromGit: <url>             # One-time build from repo
    verticalAutoscaling: {...}
    minContainers: 1
    maxContainers: 10
```

---

## Build Pipeline

### Cache Behavior
- **Two-layer cache**: Base layer (OS + prepareCommands) + Build layer (buildCommands output)
- **prepareCommands change = full rebuild**: Invalidates BOTH cache layers
- **`cache: false` only affects `/build/source`**: Files elsewhere (Go modules in `~/go`, pip in `/usr/lib/python`) remain cached
- **buildFromGit caches, local push doesn't**: Git builds benefit from layer caching

### Deploy Files
- **`deployFiles` is MANDATORY**: Build output not auto-deployed
- **Tilde syntax extracts contents**: `dist/~` → `/var/www/`, not `/var/www/dist/`
- **Without tilde: nested directory**: `dist` → `/var/www/dist/`
- **Files land in `/var/www`**: `./src/assets/fonts` → `/var/www/src/assets/fonts`

### Build Container Limits
- **CPU**: 1-5 cores (scales as needed)
- **RAM**: 8 GB (fixed)
- **Disk**: 1-100 GB
- **Time limit**: 1 hour (build terminated after 60 minutes)

### Application Versions
- **Keeps 10 most recent versions**: Older auto-deleted
- **Can restore any previous version**

---

## Ports & Networking

### Port Rules
- **Valid range: 10-65435**: Ports 80 and 443 reserved by Zerops for SSL termination
- **`httpSupport: true` for HTTP**: NOT `protocol: HTTP` (invalid)
- **`protocol: TCP` or `protocol: UDP` for non-HTTP**: Only these two values accepted

### Internal Communication
- **VXLAN private network per project**: Isolated, no cross-project communication
- **Service discovery by hostname**: `http://hostname:port` (hostname = service name from import.yml)
- **NEVER use HTTPS internally**: SSL terminates at L7 balancer
- **NEVER listen on port 443**: App listens on standard port (3000, 8080), Zerops handles SSL

### Public Access
- **Shared IPv4: free, HTTP/HTTPS only**: Requires BOTH A and AAAA DNS records for SNI routing
- **Dedicated IPv4: $3/30 days**: All protocols, non-refundable, auto-renews
- **IPv6: free, dedicated per project**: All protocols
- **zerops.app subdomain: 50MB upload limit**: Not for production, use `enableSubdomainAccess: true` in import.yml
- **Custom domain: 512MB upload limit**: Production-ready

---

## Environment Variables

### Scopes
- **Service variables**: Only available to specific service
- **Project variables**: Auto-inherited by ALL services, DO NOT re-reference

### Cross-Service Reference
- **Syntax: `${servicename_variablename}`**: Dashes become underscores (hostname `my-db` → `${my_db_password}`)

### Cross-Phase Access
- **Build → Runtime**: `${BUILD_MYVAR}` in run section
- **Runtime → Build**: `${RUNTIME_MYVAR}` in build section

### Auto-Generated Variables
- **`${zeropsSubdomain}`**: Resolves to service subdomain (available in zerops.yml)
- **`${appVersionId}`**: Unique ID for current deployment version

### Restrictions
- **Keys**: Alphanumeric + `_` only, case-sensitive
- **Values**: ASCII only, no EOL characters, no UTF-8

---

## Scaling

### Vertical (Resources)
**CPU:**
- **Shared**: 1/10 to 10/10 physical core (shared with ≤10 apps)
- **Dedicated**: Exclusive physical core access
- **Mode change limit**: Once per hour (SHARED↔DEDICATED)

**RAM:**
- **Dual-threshold**: `minFreeRamGB` AND `minFreeRamPercent` — system uses whichever is higher
- **Default min free**: 0.0625 GB (64 MB)

**Disk:**
- **Can only grow**: Never shrinks automatically, to reduce = recreate service

### Horizontal (Containers)
- **Range: 1-10 containers**: Applies to runtimes, Linux containers, Docker
- **Databases/caches: fixed count**: Set at creation, HA = 3 nodes (immutable)

### HA Mode
- **IMMUTABLE after creation**: Cannot switch NON_HA↔HA, must delete and recreate
- **HA recovery**: Failed container disconnected → new created → data synced → old removed

---

## Preprocessor & Secrets

### Enable Preprocessor
```yaml
#yamlPreprocessor=on                # MUST be first line
services:
  - hostname: app
    envSecrets:
      SECRET_KEY: <@generateRandomString(<32>)>      # Old syntax
      APP_KEY: ${random(32)}                         # New syntax
```

### Functions
- **`<@generateRandomString(<N>)>`**: Old syntax, N-character random string
- **`${random(N)}`**: New syntax, N-character random string
- **`${randomInt(min, max)}`**: Random integer in range
- **`${sha256(value)}`**, **`${bcrypt(value, rounds)}`**, **`${argon2id(value)}`**: Hashing
- **`${jwt(algorithm, secret, payload)}`**: JWT generation (HS256, HS384, HS512, RS256, RS384, RS512)
- **`${generateRSAKeyPair(bits)}`**, **`${generateEd25519KeyPair()}`**: Key pairs

### Behavior
- **Values generated once at import time**: Fixed after import, not regenerated
- **envSecrets vs envVariables**: envSecrets masked in GUI, envVariables visible

---

## zsc Commands (Init & Migrations)

### One-Time Execution (HA-Safe)
```yaml
run:
  initCommands:
    - zsc execOnce migrate -- npm run db:migrate
    - zsc execOnce seed -- npm run db:seed
```
- **Executes exactly once across all containers**: Prevents duplicate migrations in HA setups
- **`-r, --retryUntilSuccessful`**: Retry until success
- **`<key>` is unique identifier**: Must be unique per execution

### Install Additional Tech
```yaml
build:
  prepareCommands:
    - zsc add python@3.9               # Install additional runtime
```

---

## Core Package & Lifecycle

### Core Plans
- **Lightweight**: 15h build, 5GB backup, 100GB egress
- **Serious**: 150h build, 25GB backup, 3TB egress
- **Upgrade: $10 one-time, LIGHT→SERIOUS**: ~35s network downtime, CANNOT downgrade

### Service Lifecycle
- **Hostname is immutable**: Cannot change after creation, must delete and recreate
- **Runtime services start in READY_TO_DEPLOY**: Unless `startWithoutCode: true` (immediate ACTIVE)
- **Managed services start ACTIVE**: Databases, caches, storage auto-start

---

## Platform Variables for Frameworks

- **`TRUSTED_PROXIES`**: Configure for Django/Laravel behind Zerops L7 balancer (CSRF validation)
- **`AWS_USE_PATH_STYLE_ENDPOINT: true`**: REQUIRED for Zerops Object Storage (MinIO)
- **`PHX_SERVER: true`**: REQUIRED for Elixir Phoenix releases
- **`server.address: 0.0.0.0`**: REQUIRED for Java Spring (default localhost unreachable)

---

## See Also
- zerops://runtime-exceptions — runtime-specific deviations
- zerops://service-cards — managed service connection details
- zerops://wiring-patterns — multi-service architecture patterns
