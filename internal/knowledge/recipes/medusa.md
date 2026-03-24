# Medusa on Zerops

Medusa v2 e-commerce backend with PostgreSQL, Valkey, Meilisearch, and S3 object storage.

## Keywords
medusa, ecommerce, headless commerce, storefront, cart, checkout

## TL;DR
Medusa v2 on Node.js 22 with PostgreSQL, Valkey, Meilisearch, and S3 — migrations and seed scripts via `zsc execOnce` in initCommands, health check on `/health` port 9000.

## zerops.yml
```yaml
zerops:
  - setup: medusa
    build:
      base: nodejs@22
      buildCommands:
        - yarn
        - yarn build
      deployFiles:
        - .medusa/server/~
        - tsconfig.json
        - node_modules
        - ./src/scripts/seed-files
      cache: node_modules
    run:
      base: nodejs@22
      envVariables:
        ADMIN_CORS: ${zeropsSubdomain}
        SUPERADMIN_EMAIL: admin@example.com
        DATABASE_TYPE: postgres
        NODE_ENV: production
        BACKEND_URL: ${MEDUSA_INSTANCE_URL}
        STORE_CORS: ${NEXT_STORE_URL},http://localhost:5173,http://localhost:3000
        DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}?ssl_mode=disable
        MINIO_BUCKET: ${storage_bucketName}
        MINIO_ENDPOINT: ${storage_apiUrl}
        MINIO_SECRET_KEY: ${storage_secretAccessKey}
        MINIO_ACCESS_KEY: ${storage_accessKeyId}
        REDIS_URL: redis://${redis_hostname}:${redis_port}
        CACHE_REDIS_URL: redis://${redis_hostname}:${redis_port}
        EVENTS_REDIS_URL: redis://${redis_hostname}:${redis_port}
        MEILISEARCH_HOST: http://${search_hostname}:${search_port}
        MEILISEARCH_API_KEY: ${search_masterKey}
      initCommands:
        - zsc execOnce ${appVersionId}_migration -- yarn migrate
        - zsc execOnce ${appVersionId}_links -- yarn syncLinks
        - zsc execOnce createInitialSuperadmin -- yarn createInitialSuperadmin
        - zsc execOnce seedInitialData -- yarn seedInitialData
        - zsc execOnce setInitialPublishableKey -- yarn setInitialPublishableKey
        - zsc execOnce addInitialSearchDocuments -- yarn addInitialSearchDocuments
      ports:
        - port: 9000
          httpSupport: true
      start: yarn start
      healthCheck:
        httpGet:
          port: 9000
          path: /health
```

## import.yml
```yaml
#yamlPreprocessor=on
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 100

  - hostname: search
    type: meilisearch@1.10
    mode: NON_HA
    enableSubdomainAccess: true
    priority: 100

  - hostname: redis
    type: valkey@7.2
    mode: NON_HA
    priority: 100

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 100

  - hostname: medusa
    type: nodejs@22
    enableSubdomainAccess: true
    envSecrets:
      COOKIE_SECRET: <@generateRandomString(<32>)>
      JWT_SECRET: <@generateRandomString(<32>)>
      SUPERADMIN_PASSWORD: s4lt_<@generateRandomString(<16>)>
    verticalAutoscaling:
      minRam: 0.5
      cpu: 1
    priority: 90
```

## Gotchas

- **Port 9000** — Medusa listens on 9000, not 3000; both readinessCheck and healthCheck must target `/health` on port 9000
- **`#yamlPreprocessor=on` required** — `envSecrets` use `<@generateRandomString(...)>` which needs the YAML preprocessor
- **`${redis_port}` not hardcoded 6379** — use the dynamic ref; Valkey env var name is `hostname` not `host` (`${redis_hostname}`, not `${redis_host}`)
- **Two `zsc execOnce` key patterns** — migration/links use `${appVersionId}_*` (re-run each deploy); setup commands use fixed keys (run once in service lifetime)
- **Build OOM** — Medusa v2 build is memory-intensive; `minRam: 0.5` in verticalAutoscaling prevents OOM during build
- **Priority ordering** — DB and managed services at priority 100, medusa at 90 ensures dependencies start first
- **S3 `forcePathStyle: true`** — required in Medusa's S3 plugin config (`objectStoragePolicy: public-read` + path-style endpoint for MinIO backend)
- **`STORE_CORS`** — comma-separated list of all storefront URLs; include localhost ports for local development
- **`seed-files` in deployFiles** — seed data files under `./src/scripts/seed-files` are needed at runtime by the seed initCommands
