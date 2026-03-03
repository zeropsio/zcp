# Medusa on Zerops

Medusa v2 e-commerce backend with PostgreSQL, Valkey (Redis), Meilisearch, and S3 object storage. Uses yarn for package management with `zsc execOnce` for migrations and initial data seeding.

## Keywords
medusa, nodejs, ecommerce, headless commerce, postgresql, valkey, redis, meilisearch, s3, object storage, typescript

## TL;DR
Medusa v2 on Node.js 22 with PostgreSQL, Valkey, Meilisearch, and S3 storage — migrations and seed scripts via `zsc execOnce` in initCommands, health check on `/health` port 9000.

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
        DATABASE_TYPE: postgres
        NODE_ENV: production
        BACKEND_URL: ${MEDUSA_INSTANCE_URL}
        STORE_CORS: ${NEXT_STORE_URL},http://localhost:5173,http://localhost:3000
        DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:5432/${db_hostname}?ssl_mode=disable
        MINIO_BUCKET: ${storage_bucketName}
        MINIO_ENDPOINT: ${storage_apiUrl}
        MINIO_SECRET_KEY: ${storage_secretAccessKey}
        MINIO_ACCESS_KEY: ${storage_accessKeyId}
        REDIS_URL: redis://${redis_hostname}:6379
        CACHE_REDIS_URL: redis://${redis_hostname}:6379
        EVENTS_REDIS_URL: redis://${redis_hostname}:6379
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
      ADMIN_CORS: ${zeropsSubdomain}
      COOKIE_SECRET: <@generateRandomString(<32>)>
      JWT_SECRET: <@generateRandomString(<32>)>
      SUPERADMIN_EMAIL: admin@example.com
      SUPERADMIN_PASSWORD: s4lt_<@generateRandomString(<16>)>
    verticalAutoscaling:
      minRam: 0.5
      cpu: 1
    priority: 90
```

## Configuration

Medusa v2 uses `medusa-config.ts` for all service connections. Key environment variables:
- `DATABASE_URL` — full PostgreSQL connection string using cross-service references `${db_user}`, `${db_password}`, `${db_hostname}`
- `REDIS_URL`, `CACHE_REDIS_URL`, `EVENTS_REDIS_URL` — all point to the Valkey service via `${redis_hostname}`
- `MINIO_*` — S3-compatible object storage via Zerops object-storage service
- `MEILISEARCH_HOST` and `MEILISEARCH_API_KEY` — full-text search via `${search_hostname}` and `${search_masterKey}`
- `ADMIN_CORS` — set as envSecret resolving to `${zeropsSubdomain}` for admin panel access

## Common Failures
- **Build OOM**: Medusa v2 build is memory-intensive; set `minRam: 0.5` or higher in verticalAutoscaling
- **Migration fails on first deploy**: PostgreSQL must be fully ready; use `priority: 100` for DB and `priority: 90` for medusa to ensure ordering
- **Meilisearch connection refused**: Verify `${search_hostname}` and `${search_port}` cross-service references resolve correctly
- **S3 uploads fail**: Object storage must use `objectStoragePolicy: public-read` and `forcePathStyle: true` in Medusa S3 config

## Gotchas

- **deployFiles is for stage/production** — this recipe shows the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy. With `[.]`, build output stays in its original directory under `/var/www/` — adjust `start` path accordingly (see Deploy Semantics in platform reference).
- **initCommands use `zsc execOnce`** — per-deploy migrations use `${appVersionId}` suffix so they run once per version; initial setup commands (superadmin, seed data) use fixed keys so they run once in the service lifetime
- **Port 9000** — Medusa listens on 9000, not the typical 3000; both readinessCheck and healthCheck must target `/health` on port 9000
- **Four managed services required** — PostgreSQL, Valkey, Meilisearch, and object-storage must all be created before Medusa can start
- **`#yamlPreprocessor=on` required in import.yml** — envSecrets use `<@generateRandomString(...)>` which needs the YAML preprocessor
- **STORE_CORS** must include all storefront URLs (comma-separated) for cross-origin API access from frontend applications
- **Deploy files include `./src/scripts/seed-files`** — seed data files are needed at runtime for initial data population via initCommands
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
