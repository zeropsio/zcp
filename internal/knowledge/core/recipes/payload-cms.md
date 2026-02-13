# Payload CMS on Zerops

Payload CMS with Next.js. Requires Ubuntu OS and database wait pattern.

## zerops.yml (database wait + migrations in build)
```yaml
zerops:
  - setup: api
    build:
      base: nodejs@20
      os: ubuntu  # NOT Alpine
      envVariables:
        PAYLOAD_SECRET: ${RUNTIME_PAYLOAD_SECRET}
        DATABASE_URI: ${RUNTIME_DATABASE_URI}
      buildCommands:
        - pnpm i
        - zsc test tcp -6 db:5432 --timeout 30s  # Wait for DB
        - zsc test tcp -6 mailpit:1025 --timeout 30s
        - pnpm payload migrate  # Migrations in BUILD phase
        - pnpm build
    run:
      os: ubuntu
      envVariables:
        DATABASE_URI: ${db_connectionString}/${db_dbName}
```

## import.yml
```yaml
services:
  - hostname: api
    maxContainers: 1
  - hostname: db
    priority: 1  # Start before api
  - hostname: storage
    priority: 1
```

## Gotchas
- **zsc test tcp** ensures DB is ready before migrations (critical for build-time migrations)
- **Ubuntu OS** required (not default Alpine)
- **Migrations run during BUILD** (not runtime)
- Service priorities ensure DB/storage start first
