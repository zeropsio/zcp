# PostgreSQL on Zerops

## Keywords
postgresql, postgres, sql, relational, database, pgbouncer, read replica, ha, primary, connection string, 5432

## TL;DR
PostgreSQL is the recommended database on Zerops with full HA (3 nodes), read replicas on port 5433, and external TLS access via pgBouncer on port 6432.

## Zerops-Specific Behavior
- Versions: 17, 16, 14
- Ports:
  - **5432** — Primary (read/write)
  - **5433** — Read replicas (HA only, read-only)
  - **6432** — External TLS via pgBouncer
- Default database: Named after service hostname
- Default user: Auto-generated with env vars
- Env vars: `hostname`, `port`, `user`, `password`, `connectionString`

## HA Mode (3 nodes)
- 1 primary + 2 read replicas
- Automatic failover on primary failure
- Read scaling: Use port 5433 for read-heavy workloads
- Streaming replication (async)

## Connection Patterns
```
# Internal (from same project)
postgresql://${user}:${password}@${hostname}:5432/${dbname}

# Read replicas (HA only)
postgresql://${user}:${password}@${hostname}:5433/${dbname}

# External (TLS via pgBouncer)
postgresql://${user}:${password}@${hostname}:6432/${dbname}?sslmode=require
```

## Configuration
```yaml
# import.yaml
services:
  - hostname: db
    type: postgresql@16
    mode: HA
```

## Auto-Injected Env Vars in import.yaml

When referencing a PostgreSQL service in `envSecrets`, use the hostname prefix:

```yaml
#yamlPreprocessor=on
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA

  - hostname: app
    type: nodejs@22
    envSecrets:
      DB_HOST: db                              # hostname = service name
      DB_PORT: "5432"
      DB_USER: ${db_user}                      # auto-generated
      DB_PASSWORD: ${db_password}              # auto-generated
      DATABASE_URL: ${db_connectionString}/${db_dbName}
```

**Pattern:** `${<hostname>_<varName>}` — the service hostname becomes the prefix for all its auto-generated env vars.

## Gotchas
1. **`postgresql://` vs `postgres://`**: Some libraries (e.g., older Django) need `postgres://` — create a custom env var
2. **No internal TLS**: Never use `sslmode=require` for internal connections — only for port 6432
3. **HA mode is immutable**: Cannot switch after creation — delete and recreate
4. **Don't modify `zps` user**: System maintenance account — never change or delete
5. **Read replicas are async**: Brief replication lag possible — don't use for write-then-read patterns
6. **Service hostname = DB host**: The `hostname` field in import.yaml becomes the internal DNS name for DB connections

## See Also
- zerops://decisions/choose-database
- zerops://services/_common-database
- zerops://examples/connection-strings
- zerops://config/import-yml-patterns
- zerops://platform/backup
