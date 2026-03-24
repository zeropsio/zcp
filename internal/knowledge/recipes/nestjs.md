# NestJS on Zerops

NestJS API on Node.js with PostgreSQL and S3 file storage.

## Keywords
nestjs, typeorm, decorator, guard, interceptor, fastify

## TL;DR
NestJS on Node.js 20 with PostgreSQL and object storage — `trust proxy` required behind Zerops L7 balancer, migrations via `zsc execOnce ${appVersionId}`.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: nodejs@20
      buildCommands:
        - npm i
        - npm run build
      deployFiles:
        - dist
        - node_modules
        - package.json
      cache: node_modules
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        DATABASE_HOST: ${db_hostname}
        DATABASE_PORT: ${db_port}
        DATABASE_NAME: ${db_dbName}
        DATABASE_USERNAME: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        STORAGE_ACCESS_KEY_ID: ${storage_accessKeyId}
        STORAGE_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        STORAGE_ENDPOINT: ${storage_apiUrl}
        STORAGE_S3_BUCKET_NAME: ${storage_bucketName}
        STORAGE_REGION: us-east-1
      initCommands:
        - zsc execOnce ${appVersionId} -- npm run typeorm:migrate
      start: npm run start:prod
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

## import.yml
```yaml
services:
  - hostname: api
    type: nodejs@20
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

## Configuration

NestJS with Express requires trust proxy for correct IP/protocol behind the Zerops L7 balancer:

```typescript
// main.ts
const app = await NestFactory.create(AppModule);
app.set('trust proxy', true);
```

## Gotchas

- **Trust proxy required** — `app.set('trust proxy', true)` in main.ts; without it, request IP and HTTPS detection are wrong behind the Zerops L7 balancer
- **TypeORM migrations via `zsc execOnce`** — runs once per deploy per container across all replicas; `${appVersionId}` key ensures re-run on each new deploy
- **`DATABASE_HOST: ${db_hostname}`** — use the dynamic ref, not the hardcoded service hostname; if the service is named `mydb`, the ref is `${mydb_hostname}`
- **Object Storage uses MinIO** — `forcePathStyle: true` required in S3 client config; `STORAGE_REGION` must be set (use `us-east-1`)
- **`AWS_USE_PATH_STYLE_ENDPOINT`** — if using AWS SDK env var auto-detection, set this to `"true"` for Zerops S3 compatibility
