# Go on Zerops

Plain Go HTTP server with net/http stdlib and PostgreSQL via lib/pq. Minimal Go backend template.

## Keywords
go, golang, net/http, postgresql, api, stdlib

## TL;DR
Go stdlib HTTP server on port 8080 with PostgreSQL -- single binary build, zero runtime dependencies.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: go@1
      buildCommands:
        - go build -o app .
      deployFiles: ./app
      cache:
        - ~/go/pkg/mod
    run:
      base: go@1
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      start: ./app
      healthCheck:
        httpGet:
          port: 8080
          path: /status
```

## import.yml
```yaml
services:
  - hostname: api
    type: go@1
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

Database connection string built from env vars:

```go
connStr := fmt.Sprintf(
    "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
    os.Getenv("DB_HOST"),
    os.Getenv("DB_PORT"),
    os.Getenv("DB_USER"),
    os.Getenv("DB_PASS"),
    os.Getenv("DB_NAME"),
)
db, err := sql.Open("postgres", connStr)
```

Health check endpoint:

```go
http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
})
```

## Gotchas

- **deployFiles is for stage/production** — this recipe shows the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy. With `[.]`, build output stays in its original directory under `/var/www/` — adjust `start` path accordingly (see Deploy Semantics in platform reference).
- **Bind to 0.0.0.0** -- `http.ListenAndServe(":8080", nil)` binds all interfaces by default; do not use `127.0.0.1:8080`
- **Single binary deploy** -- Go compiles to a static binary, only `./app` needs to be in `deployFiles`
- **Logger must use os.Stdout** for Zerops log collection -- `os.Stderr` logs are not captured by the platform
- **sslmode=disable** in the PostgreSQL connection string -- Zerops internal network is encrypted at the VXLAN layer
- **${db_hostname}** and other `${db_*}` vars are auto-injected by Zerops from the `db` service
- **Module cache** -- `~/go/pkg/mod` in build cache speeds up subsequent builds significantly
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
