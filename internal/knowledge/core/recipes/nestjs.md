# NestJS on Zerops

NestJS API with TypeORM migrations, S3 file uploads via multer + aws-sdk.

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

## Gotchas
- **TypeORM idempotent migrations** via zsc execOnce with appVersionId
- **File uploads** use multer + aws-sdk for S3 integration
- 5 services: api + pg + s3 + mailpit + adminer
- Health checks configured out of box
