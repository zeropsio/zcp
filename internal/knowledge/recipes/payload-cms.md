# Payload CMS on Zerops

Payload CMS 3.x with Next.js frontend, PostgreSQL, and S3 object storage.

## Keywords
payload, payload-cms, rich-text, collections, blocks, access-control

## TL;DR
Payload CMS on Node.js 20 (Ubuntu, required) — migrations run during BUILD phase using `zsc test tcp` to wait for the database, not in initCommands.

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
        DATABASE_URI: ${db_connectionString}
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

Build-time env vars use `${RUNTIME_*}` prefix to access the runtime service's own secrets and cross-service refs during the build phase. Zerops resolves these from the runtime service's environment:

- `${RUNTIME_PAYLOAD_SECRET}` — the `PAYLOAD_SECRET` envSecret set on the `api` service
- `${RUNTIME_DATABASE_URI}` — resolves to the `DATABASE_URI` from the run envVariables
- `${RUNTIME_S3_*}` — resolves to the S3 cross-service refs from the run envVariables

## Gotchas

- **Ubuntu OS required on both build and run** — Alpine lacks native dependencies Payload needs; `os: ubuntu` must appear in both `build:` and `run:`
- **Migrations run during BUILD** — not in initCommands; `zsc test tcp -6 db:5432 --timeout 30s` must precede migration commands to wait for PostgreSQL
- **`${RUNTIME_*}` prefix** — only way to pass runtime secrets into the build phase; without the prefix, `${PAYLOAD_SECRET}` would not resolve during build
- **`DATABASE_URI: ${db_connectionString}`** — PostgreSQL `connectionString` already includes the full URL with database name; do not append `/${db_dbName}`
- **`#yamlPreprocessor=on` required** — `envSecrets` use `<@generateRandomString(...)>` which requires the YAML preprocessor
- **`maxContainers: 1` recommended** — Payload + Next.js does not reliably handle multiple containers
- **Object storage `private` policy** — Payload manages media access control; do not use `public-read`
