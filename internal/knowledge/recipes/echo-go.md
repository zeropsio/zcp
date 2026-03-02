# Echo (Go) on Zerops
Go Echo web app with PostgreSQL, S3 Object Storage, Valkey cache, Mailpit, and Adminer.

## Keywords
echo, go, golang, postgresql, valkey, redis, s3, object-storage, api

## TL;DR
Go Echo API with PostgreSQL, Valkey, and S3 — logger must output to `os.Stdout`, static assets deployed alongside the binary.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: go@1
      buildCommands:
        - go build -v -o app main.go
      deployFiles:
        - static/
        - app
      cache:
        - ~/go/pkg/mod
    run:
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        DB_HOST: db
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASSWORD: ${db_password}
        S3_ENDPOINT: ${storage_apiUrl}
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
        S3_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        S3_BUCKET: ${storage_bucketName}
        SMTP_HOST: mailpit
        SMTP_PORT: "1025"
        REDIS_HOST: cache
        REDIS_PORT: ${cache_port}
      initCommands:
        - zsc execOnce seed -- /var/www/app -seed
      start: /var/www/app
```

## import.yml
```yaml
services:
  - hostname: app
    type: go@1
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

## Configuration

**Go code requirement** -- logger must use `os.Stdout`:
```go
log.SetOutput(os.Stdout)
```

## Gotchas
- **Logger must use `os.Stdout`** for Zerops log collection -- `os.Stderr` logs are not captured
- **HTTPS termination disabled** in app code -- Zerops SSL proxy handles TLS termination
- **Sessions stored in Valkey** (Redis-compatible), files in S3 Object Storage
- **S3 endpoint** -- `${storage_apiUrl}` may return an HTTPS URL. The Go MinIO client needs the hostname without protocol prefix and `Secure: true`
- **Mailpit** is for development only -- replace with a production SMTP provider before going live
- **Adminer** should have public access disabled or be removed entirely in production
- **Database seeding** runs via `zsc execOnce` -- executes exactly once across all containers (HA-safe)
