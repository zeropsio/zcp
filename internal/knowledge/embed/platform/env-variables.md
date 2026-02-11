# Environment Variables on Zerops

## Keywords
environment variables, env vars, secrets, envSecrets, project variables, service variables, cross-service, variable reference, env isolation

## TL;DR
Zerops has two scopes (service and project), two isolation modes, and a `RUNTIME_`/`BUILD_` prefix system for cross-phase access.

## Two Scopes

### Service Variables (per-service)
- Defined in `zerops.yaml`, GUI, or API
- Only available to that specific service

### Project Variables (all services)
- Defined in GUI or API
- Automatically inherited by all services in the project
- **Do not re-reference project vars** — they're auto-available

## Variable Types
- **Build & Runtime**: Defined in `zerops.yaml` `envVariables` section
- **Secrets**: Defined in GUI or `envSecrets` in import.yaml (not in zerops.yaml)
- **System-generated**: Read-only (e.g., `hostname`, `port`, connection strings)

## Isolation Modes

### `service` (default, recommended)
- Services are isolated — must explicitly reference other service vars
- Cross-service reference: `${servicename_variablename}` (uses **underscore**, not dash)
- Example: hostname `my-db` → reference as `${my_db_password}` (dashes become underscores)

### `none` (legacy)
- All variables shared with service prefix (e.g., `db_password`)
- Not recommended for new projects

## Cross-Phase Access
- Access runtime var in build: `${RUNTIME_MYVAR}`
- Access build var in runtime: `${BUILD_MYVAR}`

## Precedence
1. Service variables (highest)
2. Project variables
3. Build/runtime vars override secrets with same name

## Restrictions
- Keys: Alphanumeric + `_` only, case-sensitive, unique per scope
- Values: ASCII only, no EOL characters

## Configuration

### In zerops.yaml (runtime env vars)
```yaml
# zerops.yaml (under run: section)
envVariables:
  NODE_ENV: production
  DATABASE_URL: postgresql://${db_user}:${db_password}@db:5432/${db_dbname}
```

### In import.yaml (at service creation time)
```yaml
services:
  - hostname: db
    type: postgresql@16
  - hostname: app
    type: nodejs@22
    envSecrets:
      DB_HOST: ${db_hostname}
      DB_PORT: ${db_port}
      DB_USER: ${db_user}
      DB_PASSWORD: ${db_password}
      DB_NAME: ${db_dbName}
      DATABASE_URL: postgresql://${db_user}:${db_password}@db:5432/${db_dbName}
```

### Via zerops_env tool (after service exists)
```
zerops_env(action: "set", serviceHostname: "app", variables: [
  "DB_HOST=${db_hostname}",
  "DB_PASSWORD=${db_password}"
])
```

## Gotchas
1. **Don't re-reference project vars**: They're auto-available — referencing creates a service var that shadows the project var
2. **Password sync is manual**: Changing DB password in GUI doesn't update env vars (and vice versa)
3. **Env vars not available via VPN**: Connect to service to read them, or use GUI/API
4. **ASCII only**: No UTF-8 in env var values

## See Also
- zerops://config/zerops-yml
- zerops://config/import-yml
- zerops://gotchas/common
