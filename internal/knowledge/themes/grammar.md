# Zerops Grammar

## TL;DR
YAML schema reference for Zerops. import.yml defines topology (WHAT exists), zerops.yml defines lifecycle (HOW it runs). For platform concepts see platform theme, for actionable rules see rules theme.

## Keywords
zerops, grammar, import.yml, zerops.yml, schema, build, deploy, run, networking, ports, binding, 0.0.0.0, environment variables, scaling, autoscaling, yaml, pipeline, hostname, service, container, project, ssl, tls, https, http, cache, prepareCommands, buildCommands, deployFiles, tilde, mode, HA, NON_HA, cron, health check, readiness check

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
  mode: HA | NON_HA                    # optional, default NON_HA, IMMUTABLE once set
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

**WARNING**: API `dryRun` validates YAML schema only -- it does NOT enforce service-type restrictions (e.g., `minContainers` on managed services passes dry-run but fails real import). The rules in this grammar ARE the validation layer.

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

## Schema Rules

### Port Rules
Valid range **10-65435** -- ports 80/443 reserved by Zerops for SSL termination. Exception: PHP uses port 80. `httpSupport: true` for HTTP, `protocol: tcp|udp` for non-HTTP. NEVER `protocol: HTTP`.

### Deploy Semantics
- **Tilde syntax**: `dist/~` extracts contents to `/var/www/` (not `/var/www/dist/`)
- Without tilde: `dist` -> `/var/www/dist/` (nested)
- All files land in `/var/www`
- **Git required**: `zerops_deploy` uses `zcli push` which requires a git repository. Before deploying, run `git init && git add -A && git commit -m "deploy"` in the working directory

### Cache Architecture (Two-Layer)
- **Base layer**: OS + prepareCommands (invalidated only when prepareCommands change)
- **Build layer**: buildCommands output (invalidated every build)
- `cache: false` only affects `/build/source` -- modules elsewhere remain cached

### Environment Variables
- **import.yml service level**: ONLY `envSecrets` and `dotEnvSecrets`. No `envVariables` (project-level only)
- **zerops.yml**: `build.envVariables` and `run.envVariables` exist (visible in GUI)
- **Managed services auto-generate credentials** (hostname, port, user, password, dbName, connectionString) -- do NOT set these in import.yml. Only set custom env vars on runtime services. Managed services accept only `mode`, `priority`, `hostname`, `type`, and scaling config in import
- Cross-service ref: `${hostname_varname}` -- dashes->underscores
- Project vars auto-inherited -- do NOT re-reference (creates shadow)
- Cross-phase: build->run `${BUILD_MYVAR}`, run->build `${RUNTIME_MYVAR}`
- Keys: alphanumeric + `_`, case-sensitive. Values: ASCII only

### Public Access
- **Shared IPv4**: free, HTTP/HTTPS only, requires BOTH A and AAAA DNS records
- **Dedicated IPv4**: $3/30 days, all protocols
- **IPv6**: free, dedicated per project
- **zerops.app subdomain**: 50MB limit, not production

### zsc Commands
- `zsc execOnce <key> -- <cmd>`: run once across all containers (HA-safe migrations)
- `zsc add <runtime>@<version>`: install additional runtime in prepareCommands

### Multi-Service Examples

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
    zeropsSetup: dev
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1.0
  - hostname: appstage
    type: nodejs@22
    startWithoutCode: true
    zeropsSetup: prod
    enableSubdomainAccess: true
```

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
    zeropsSetup: dev
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1.0
  - hostname: appstage
    type: bun@1.2
    startWithoutCode: true
    zeropsSetup: prod
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
    zeropsSetup: prod
    enableSubdomainAccess: true
```
