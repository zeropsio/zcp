# Echo (Go) on Zerops

Go Echo web app with 6 services: PostgreSQL, S3, KeyDB (Redis), Mailpit, Adminer.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: go@latest
      buildCommands:
        - go build -v -o app main.go
      deployFiles:
        - static/
        - app
    run:
      envVariables:
        DB_HOST: db
        S3_ENDPOINT: $storage_apiUrl
        S3_ACCESS_KEY_ID: $storage_accessKeyId
        S3_BUCKET: $storage_bucketName
        REDIS_HOST: redis
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
- Sessions stored in Redis (KeyDB), files in S3
- 6 services: app + pg + s3 + keydb + mailpit + adminer
- Database migration and seeding via initCommands
