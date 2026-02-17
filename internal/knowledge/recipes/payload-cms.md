# Payload CMS on Zerops

Payload CMS with Next.js. Requires Ubuntu OS and database wait pattern.

## Keywords
payload, cms, nextjs, nodejs, postgresql, ubuntu, headless cms

## TL;DR
Payload CMS on Node.js (Ubuntu) — migrations in BUILD phase with `zsc test tcp` DB wait pattern.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: nodejs@20
      os: ubuntu
      envVariables:
        PAYLOAD_SECRET: ${RUNTIME_PAYLOAD_SECRET}
        DATABASE_URI: ${RUNTIME_DATABASE_URI}
      buildCommands:
        - pnpm i
        - zsc test tcp -6 db:5432 --timeout 30s
        - zsc test tcp -6 mailpit:1025 --timeout 30s
        - pnpm payload migrate
        - pnpm build
    run:
      os: ubuntu
      envVariables:
        DATABASE_URI: ${db_connectionString}/${db_dbName}
      start: pnpm start
```

## import.yml
```yaml
services:
  - hostname: api
    type: nodejs@20
    enableSubdomainAccess: true
    maxContainers: 1

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    priority: 10
```

## Gotchas
- **zsc test tcp** ensures DB is ready before migrations (critical for build-time migrations)
- **Ubuntu OS** required (not default Alpine)
- **Migrations run during BUILD** (not runtime)
- Service priorities ensure DB/storage start first
