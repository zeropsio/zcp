# Echo (Go) on Zerops

Go Echo web app with 6 services: PostgreSQL, S3, Valkey (Redis-compatible), Mailpit, Adminer.

## Keywords
echo, go, golang, postgresql, valkey, redis, s3, object-storage

## TL;DR
Go Echo API with PostgreSQL, Valkey, and S3 — logger must output to `os.Stdout` for Zerops log collection.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: go@1
      buildCommands:
        - go mod tidy
        - go build -v -o app main.go
      deployFiles:
        - static/
        - app
    run:
      envVariables:
        DB_HOST: db
        S3_ENDPOINT: ${storage_apiUrl}
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
        S3_BUCKET: ${storage_bucketName}
        REDIS_HOST: cache
        SMTP_HOST: mailpit
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
    priority: 10
```

## Go code requirement
```go
// Logger MUST output to os.Stdout for Zerops log collection
log.SetOutput(os.Stdout)
```

## Gotchas
- **Logger must use os.Stdout** for Zerops log collection
- **HTTPS termination disabled** (runs behind SSL proxy)
- Sessions stored in Valkey (Redis-compatible), files in S3
- 6 services: app + pg + s3 + valkey + mailpit + adminer
- Database migration and seeding via initCommands
