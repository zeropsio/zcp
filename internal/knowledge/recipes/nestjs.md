# NestJS on Zerops

NestJS API with TypeORM migrations, PostgreSQL, S3 file uploads via multer + aws-sdk.

## Keywords
nestjs, nodejs, typeorm, postgresql, s3, object-storage, typescript, api, express

## TL;DR
NestJS API with TypeORM and S3 uploads — migrations via `zsc execOnce` with `appVersionId`, trust proxy required.

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
      cache: node_modules
    run:
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        DATABASE_HOST: ${db_hostname}
        DATABASE_PORT: ${db_port}
        DATABASE_NAME: db
        DATABASE_USERNAME: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        SMTP_HOST: mailpit
        SMTP_PORT: "1025"
        SMTP_USER: ""
        SMTP_PASS: ""
        SMTP_EMAIL_FROM: recipe@zerops.io
        STORAGE_ACCESS_KEY_ID: ${storage_accessKeyId}
        STORAGE_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        STORAGE_ENDPOINT: ${storage_apiUrl}
        STORAGE_S3_BUCKET_NAME: ${storage_bucketName}
        STORAGE_REGION: us-east-1
      initCommands:
        - zsc execOnce ${appVersionId} npm run typeorm:migrate
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
    objectStoragePolicy: public-read
    priority: 10

  - hostname: mailpit
    type: go@1
    priority: 10
```

## Configuration

NestJS with Express requires trust proxy for correct client IP behind the Zerops load balancer:

```typescript
// main.ts
const app = await NestFactory.create(AppModule);
app.set('trust proxy', true);
```

TypeORM datasource config reads env vars directly:

```typescript
// db.config.ts
export const dataSource = new DataSource({
  type: 'postgres',
  host: process.env.DATABASE_HOST,
  port: parseInt(process.env.DATABASE_PORT, 10),
  username: process.env.DATABASE_USERNAME,
  password: process.env.DATABASE_PASSWORD,
  database: process.env.DATABASE_NAME,
});
```

## Gotchas
- **TypeORM migrations** use `zsc execOnce ${appVersionId}` in initCommands so they run exactly once per deploy, not per container
- **trust proxy** required: `app.set('trust proxy', true)` in main.ts for Express-based NestJS behind Zerops L7 balancer
- **S3 file uploads** use multer + aws-sdk; `STORAGE_REGION` must be set (use `us-east-1` for Zerops object storage)
- **DATABASE_HOST** uses `${db_hostname}` cross-service reference, not hardcoded `db` hostname
- **Mailpit** is a dev-only SMTP mock; replace with production SMTP service and update SMTP env vars for production
- **Adminer** (DB GUI) omitted from import.yml by default; add `php-apache@8.1` service if needed for development
