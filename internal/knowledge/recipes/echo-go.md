# Echo (Go) on Zerops

Go Echo web app with PostgreSQL, Valkey cache, and S3 Object Storage.

## Keywords
echo, fiber, chi, gin, gorilla

## TL;DR
Go runtime — build binary in build container, deploy single binary. Logger must output to `os.Stdout`. Never set `run.base: alpine` (glibc/musl mismatch). Wire managed services via `${hostname_varName}`.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: go@1
      buildCommands:
        - go mod tidy && go build -v -o app main.go
      deployFiles:
        - app
      cache:
        - ~/go/pkg/mod
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /
    run:
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_NAME: ${db_dbName}
        DB_USER: ${db_user}
        DB_PASSWORD: ${db_password}
        REDIS_HOST: ${cache_hostname}
        REDIS_PORT: ${cache_port}
        S3_ENDPOINT: ${storage_apiUrl}
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
        S3_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        S3_BUCKET: ${storage_bucketName}
      initCommands:
        - zsc execOnce ${appVersionId} -- /var/www/app -migrate
      start: /var/www/app
      healthCheck:
        httpGet:
          port: 8080
          path: /
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

Logger must write to `os.Stdout` — Zerops log collection reads stdout, not stderr:
```go
log.SetOutput(os.Stdout)
```

S3 endpoint from `${storage_apiUrl}` is an HTTPS URL. The Go MinIO client needs the hostname without protocol prefix and `Secure: true`:
```go
endpoint := strings.TrimPrefix(os.Getenv("S3_ENDPOINT"), "https://")
client, _ := minio.New(endpoint, &minio.Options{Secure: true, ...})
```

## Gotchas

- **Never set `run.base: alpine`** — Go binaries compiled in the build container link against glibc; Alpine uses musl. Runtime panics at start. Omit `run.base` entirely or use `go@1` (which uses glibc).
- **Deploy single binary** — `deployFiles: app` (just the binary). Add static assets alongside if needed: `deployFiles: [app, static/]`
- **`${db_dbName}` not `${db_database}`** — PostgreSQL exposes `dbName`, not `database`. Wrong var name silently resolves to the literal string.
- **Valkey var is `hostname`** — use `${cache_hostname}`, not `${cache_host}`. `host` does not exist as a Valkey env var.
- **No TLS in app code** — Zerops L7 balancer terminates TLS. Do not configure TLS in Echo/the Go app.
- **`zsc execOnce` for migrations** — executes exactly once across all containers per deploy (HA-safe). Use it instead of running migrations in `start`.
- **S3 endpoint is HTTPS** — strip the protocol prefix before passing to MinIO client, set `Secure: true`.
