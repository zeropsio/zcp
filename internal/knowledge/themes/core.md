# Zerops YAML Reference

YAML generation reference: import.yml and zerops.yml schemas, rules, pitfalls, and complete multi-service examples.

---

## import.yml Schema

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
  # For non-secret env vars on a service, use zerops_env after import or zerops.yml run.envVariables
  buildFromGit: url                    # one-time build from repo — use ONLY with verified URLs (utility recipes like mailpit). Do NOT guess URLs.
  objectStorageSize: 1-100             # GB, object-storage only (changeable in GUI later)
  objectStoragePolicy: private | public-read | public-objects-read | public-write | public-read-write
  objectStorageRawPolicy: string       # custom IAM Policy JSON (alternative to objectStoragePolicy)
  override: bool                       # triggers redeploy of existing runtime service with same hostname
  mount: string[]                      # pre-configure shared storage connection (ALSO requires mount in zerops.yml run section to activate)
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
    swapEnabled: bool                  # enable swap memory (safety net, default varies by service type)
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

  Self-deploy with specific paths (e.g., `[app]`, `dist/~`) destroys source files + zerops.yml after deploy, making iteration impossible. Only cross-deploy targets and git-based builds can use specific paths safely.

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
zerops.yml must have `setup: appdev` and `setup: appstage` blocks matching hostnames.

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
Same pattern as above but using `api` prefix instead of `app`. zerops.yml needs `setup: apidev` and `setup: apistage`.

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
zeropsSetup omitted — defaults to hostname `app`, so zerops.yml needs `setup: app`.
