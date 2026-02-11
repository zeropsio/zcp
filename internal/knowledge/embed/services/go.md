# Go on Zerops

## Keywords
go, golang, go build, cgo, binary, static binary, go modules, go mod

## TL;DR
Go on Zerops compiles to a static binary by default (Alpine); use Ubuntu base if you need CGO with glibc, and deploy only the compiled binary.

## Zerops-Specific Behavior
- Versions: 1.22, 1 (alias for latest stable)
- Base: Alpine (default) — musl libc
- Go modules: Cached outside `/build/source` (persist even with `cache: false`)
- Working directory: `/var/www`
- No default port — must configure
- Deploy: Single binary (no runtime dependencies needed)

## Configuration
```yaml
zerops:
  - setup: myapp
    build:
      base: go@1
      buildCommands:
        - go build -o app ./cmd/server
      deployFiles:
        - app
    run:
      start: ./app
      ports:
        - port: 8080
          httpSupport: true
```

### With CGO (Ubuntu required)
```yaml
zerops:
  - setup: myapp
    build:
      base: go@1
      os: ubuntu
      prepareCommands:
        - apt-get update && apt-get install -y gcc libc-dev
      buildCommands:
        - CGO_ENABLED=1 go build -o app ./cmd/server
      deployFiles:
        - app
    run:
      os: ubuntu
      start: ./app
      ports:
        - port: 8080
          httpSupport: true
```

## Gotchas
1. **Go modules cached globally**: `~/go/pkg/mod` persists even with `cache: false` — this is expected behavior
2. **CGO needs Ubuntu**: Alpine uses musl — CGO bindings may fail. Switch to Ubuntu base for CGO
3. **Deploy only the binary**: Don't deploy the entire source tree — just the compiled binary
4. **No default port**: Must bind to `0.0.0.0:PORT` and configure in `zerops.yaml`
5. **Use `go@1` for latest**: Recipes use `go@1` alias instead of pinning specific versions

## See Also
- zerops://services/_common-runtime
- zerops://services/alpine
- zerops://services/ubuntu
- zerops://examples/zerops-yml-runtimes
