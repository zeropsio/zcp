# Payload CMS on Zerops

Payload CMS 3.x with Next.js frontend, PostgreSQL, and S3 object storage. Requires Ubuntu OS and build-time database migrations.

## Keywords
payload, cms, nextjs, nodejs, ubuntu, headless cms

## TL;DR
Payload CMS on Node.js 20 (Ubuntu) with PostgreSQL — migrations run during BUILD with `zsc test tcp` to wait for the database.

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
        NEXT_PUBLIC_SERVER_URL: ${RUNTIME_zeropsSubdomain}
        S3_ENDPOINT: ${RUNTIME_S3_ENDPOINT}
        S3_ACCESS_KEY_ID: ${RUNTIME_S3_ACCESS_KEY_ID}
        S3_SECRET_ACCESS_KEY: ${RUNTIME_S3_SECRET_ACCESS_KEY}
        S3_BUCKET: ${RUNTIME_S3_BUCKET}
      buildCommands:
        - pnpm i
        - pnpm exec next telemetry disable
        - zsc test tcp -6 db:5432 --timeout 30s
        - pnpm payload generate:importmap
        - pnpm payload migrate:status
        - pnpm payload migrate
        - pnpm build
      deployFiles:
        - next.config.js
        - redirects.js
        - node_modules
        - package.json
        - public
        - .next
      cache: node_modules
    run:
      base: nodejs@20
      os: ubuntu
      envVariables:
        DATABASE_URI: ${db_connectionString}/${db_dbName}
        NEXT_PUBLIC_SERVER_URL: ${zeropsSubdomain}
        S3_ENDPOINT: ${storage_apiUrl}
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
        S3_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        S3_BUCKET: ${storage_bucketName}
      ports:
        - port: 3000
          httpSupport: true
      start: pnpm start
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

## import.yml
```yaml
#yamlPreprocessor=on
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: private
    priority: 10

  - hostname: api
    type: nodejs@20
    enableSubdomainAccess: true
    maxContainers: 1
    envSecrets:
      PAYLOAD_SECRET: <@generateRandomString(<24>)>
    verticalAutoscaling:
      minRam: 1
```

## Configuration
Build-time env vars use `${RUNTIME_*}` prefix to reference the runtime service's own envSecrets and cross-service vars. This is how Zerops makes runtime secrets available during build:
- `RUNTIME_PAYLOAD_SECRET` resolves to the `PAYLOAD_SECRET` envSecret set on the `api` service
- `RUNTIME_DATABASE_URI` resolves to `${db_connectionString}/${db_dbName}` from the runtime config
- `RUNTIME_S3_*` vars resolve to the corresponding `storage_*` cross-service references

## Common Failures
- **Build fails at migrate**: Database not ready; `zsc test tcp -6 db:5432 --timeout 30s` must precede migration commands
- **500 on first load**: Missing `PAYLOAD_SECRET` envSecret; verify `#yamlPreprocessor=on` is the first line of import.yml
- **Images not uploading**: S3 storage not configured; check `storage_apiUrl`, `storage_accessKeyId`, `storage_secretAccessKey`, `storage_bucketName` references

## Gotchas

- **Ubuntu OS required** — both build and run must set `os: ubuntu` (Alpine lacks dependencies Payload needs)
- **Migrations run during BUILD** (not runtime) — `zsc test tcp` ensures PostgreSQL is ready before `pnpm payload migrate`
- **Build env vars use RUNTIME_ prefix** to access runtime secrets during build phase
- Object storage required for media uploads (Payload S3 plugin)
- `maxContainers: 1` recommended — Payload with Next.js does not reliably handle multiple containers
- Service priorities ensure DB and storage are ready before the app service builds
