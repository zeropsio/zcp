# Wiring Patterns

## Variable Reference Syntax

`${hostname_variablename}` — Underscores, not dashes (dashes in hostnames become underscores).

**Examples:**
```yaml
# PostgreSQL
DATABASE_URL: postgresql://${db_user}:${db_password}@db:5432/${db_dbName}

# Valkey/KeyDB
REDIS_URL: redis://${cache_password}@cache:6379

# Object Storage
S3_ENDPOINT: ${storage_apiUrl}
S3_KEY: ${storage_accessKeyId}
S3_SECRET: ${storage_secretAccessKey}
```

## envSecrets vs envVariables

- **envSecrets**: Sensitive data (passwords, tokens, certificates) — masked in GUI, cannot view after creation
- **envVariables**: Configuration (URLs, feature flags, public settings) — visible in GUI

## Concrete Wiring Example

```yaml
project:
  name: my-stack

services:
  - hostname: app
    type: nodejs@22
    envVariables:
      DB_HOST: db
      DB_NAME: ${db_dbName}
      REDIS_HOST: cache
    envSecrets:
      DB_PASSWORD: ${db_password}
      REDIS_PASSWORD: ${cache_password}

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10
```

## Cross-Service Reference Gotchas

- **Hostname = internal DNS** — Service hostname becomes DNS name (`db`, `cache`, `storage`)
- **Project vars auto-inherited** — Do NOT re-reference in service env (shadows with service var)
- **Use hostname directly** — `db:5432`, NOT `${db_hostname}:5432` (both work, direct is simpler)
- **Password sync** — Changing DB password in GUI doesn't update env vars (manual sync needed)
- **NEVER use HTTPS internally** — SSL terminates at L7 balancer (`http://app:3000`, not `https://`)
