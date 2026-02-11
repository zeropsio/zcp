# import.yaml Patterns on Zerops

## Keywords
import.yaml, import yaml, services, priority, preprocessor, generateRandomString, envSecrets, envVariables, NON_HA, HA, production, mailpit, adminer, multi-service, service definition

## TL;DR
import.yaml defines services for a project. Key patterns: priority ordering (DB=10, App=5), `#yamlPreprocessor=on` for secret generation, `envSecrets` for sensitive values, and separate NON_HA→HA for production.

## Service Priority Ordering

Priority controls startup order. Higher priority = starts first.

```yaml
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10          # starts first

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10          # starts with db

  - hostname: api
    type: nodejs@22
    priority: 5           # starts after db/cache
```

**Convention:** Databases/caches at `priority: 10`, application services at `priority: 5` or default.

## YAML Preprocessor

Enable with first-line comment for dynamic value generation:

```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: nodejs@22
    envSecrets:
      SECRET_KEY: <@generateRandomString(<64>)>
      APP_KEY: base64:<@generateRandomString(<32>)>
```

**Functions:**
- `<@generateRandomString(<N>)>` — generates N-character random string
- Values are generated once at import time and stored as env vars

## envSecrets vs envVariables

```yaml
services:
  - hostname: api
    type: nodejs@22

    # Visible in GUI, not masked
    envVariables:
      NODE_ENV: production
      PORT: "3000"

    # Masked in GUI, for sensitive data
    envSecrets:
      DB_PASSWORD: ${db_password}
      SECRET_KEY: <@generateRandomString(<64>)>
      S3_SECRET: ${storage_secretAccessKey}
```

**Rule:** Use `envSecrets` for passwords, API keys, secrets. Use `envVariables` for configuration.

## Service Reference Pattern

Reference other services' env vars using `${hostname_varName}`:

```yaml
services:
  - hostname: db
    type: postgresql@16

  - hostname: api
    type: nodejs@22
    envSecrets:
      DATABASE_URL: postgresql://${db_user}:${db_password}@db:5432/${db_dbName}
      REDIS_URL: redis://${cache_password}@cache:6379
      S3_ENDPOINT: ${storage_apiUrl}
      S3_KEY: ${storage_accessKeyId}
      S3_SECRET: ${storage_secretAccessKey}
```

## Subdomain Access

To make a service publicly accessible via `*.zerops.app` subdomain, add `enableSubdomainAccess: true` in the import YAML. This works for **all runtime and web service types** (nodejs, static, nginx, go, python, etc.):

```yaml
services:
  - hostname: app
    type: nodejs@22
    enableSubdomainAccess: true    # Pre-configures subdomain
```

**Important — subdomain lifecycle:**
- For **new services** (created via import): Use `enableSubdomainAccess: true` in import YAML. This is sufficient — do NOT also call `zerops_subdomain enable` afterward. The subdomain is pre-configured and activates when the service gets deployed.
- For **existing ACTIVE services**: Use `zerops_subdomain enable` tool. This only works on services with status ACTIVE — calling it on READY_TO_DEPLOY services will fail with "Service stack is not http or https".
- **Rule of thumb**: If you're importing a new service, always use `enableSubdomainAccess: true` in the import YAML and skip the `zerops_subdomain` tool call.

## Common Multi-Service Combos

### Simple (App + DB)
```yaml
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: app
    type: nodejs@22
    buildFromGit: https://github.com/user/repo
    enableSubdomainAccess: true
```

### Full-Stack (App + DB + Cache + Storage + Mail)
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

  - hostname: mailpit
    type: go@1
    buildFromGit: https://github.com/zeropsio/recipe-mailpit
    enableSubdomainAccess: true
    minContainers: 1
    maxContainers: 1

  - hostname: app
    type: php-nginx@8.3
    buildFromGit: https://github.com/user/repo
    enableSubdomainAccess: true
    envSecrets:
      APP_KEY: base64:<@generateRandomString(<32>)>
      DB_PASSWORD: ${db_password}
```

### Static-Only (SPA/SSG)
```yaml
services:
  - hostname: app
    type: static
    buildFromGit: https://github.com/user/repo
    enableSubdomainAccess: true
```

## Dev Services (Remove for Production)

### Mailpit (Dev SMTP)
```yaml
  - hostname: mailpit
    type: go@1
    buildFromGit: https://github.com/zeropsio/recipe-mailpit
    enableSubdomainAccess: true
    minContainers: 1
    maxContainers: 1
```

### Adminer (DB GUI)
```yaml
  - hostname: adminer
    type: php-apache@8.3
    buildFromGit: https://github.com/zeropsio/recipe-adminer
    enableSubdomainAccess: true
    minContainers: 1
    maxContainers: 1
```

**Production:** Remove both or restrict public access.

## NON_HA → HA Transition

HA mode is **immutable** after creation. For production:

```yaml
# Development
- hostname: db
  type: postgresql@16
  mode: NON_HA

# Production (must recreate service)
- hostname: db
  type: postgresql@16
  mode: HA
```

## Gotchas
1. **No `project:` section**: ZAIA import adds services to existing project — YAML must not contain `project:` key
2. **`mode` is mandatory**: PostgreSQL, MariaDB, Valkey, KeyDB, shared-storage, elasticsearch MUST have `mode: NON_HA` or `mode: HA` — omitting passes dry-run but **fails real import** with "Mandatory parameter is missing"
3. **Priority is startup order**: Not importance — higher number starts first
4. **Preprocessor first line only**: `#yamlPreprocessor=on` must be the very first line
5. **HA is immutable**: Cannot switch NON_HA↔HA after creation — must delete and recreate
6. **envSecrets persist**: Generated secrets survive service restarts and rebuilds
7. **Service hostname = internal DNS**: The `hostname` field becomes the DNS name for internal routing

## See Also
- zerops://config/import-yml
- zerops://examples/connection-strings
- zerops://operations/production-checklist
- zerops://platform/env-variables
