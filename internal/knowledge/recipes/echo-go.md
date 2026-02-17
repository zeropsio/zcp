# Echo (Go) on Zerops

Go Echo web app with 6 services: PostgreSQL, S3, Valkey (Redis-compatible), Mailpit, Adminer.

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
