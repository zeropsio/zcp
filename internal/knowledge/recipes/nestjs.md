# NestJS on Zerops

NestJS API with TypeORM migrations, S3 file uploads via multer + aws-sdk.

## Keywords
nestjs, nodejs, typeorm, postgresql, s3, object-storage, typescript, api

## TL;DR
NestJS API with TypeORM and S3 uploads — migrations via `zsc execOnce` with `appVersionId`.

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
        - ./dist
        - node_modules
        - package.json
    run:
      envVariables:
        DATABASE_HOST: db
        DATABASE_NAME: db
        STORAGE_ACCESS_KEY_ID: ${storage_accessKeyId}
        STORAGE_ENDPOINT: ${storage_apiUrl}
        STORAGE_S3_BUCKET_NAME: ${storage_bucketName}
        STORAGE_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
      initCommands:
        - zsc execOnce $ZEROPS_appVersionId npm run typeorm:migrate
      start: npm run start:prod
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
    priority: 10
```

## Gotchas
- **TypeORM idempotent migrations** via zsc execOnce with appVersionId
- **File uploads** use multer + aws-sdk for S3 integration
- 5 services: api + pg + s3 + mailpit + adminer
- Health checks configured out of box
