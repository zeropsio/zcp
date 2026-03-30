---
description: "Go recipe for Zerops — a minimal HTTP server backed by PostgreSQL, showcasing idempotent database migrations, environment-variable-driven configuration, and the complete set of Zerops infrastructure patterns across six ready-to-deploy environment configurations."
---

# Go Hello World on Zerops


# Go Hello World on Zerops


# Go Hello World on Zerops


# Go Hello World on Zerops


# Go Hello World on Zerops


# Go Hello World on Zerops





## Keywords
go, golang, binary, cgo, zerops.yml, go.sum, go mod tidy

## TL;DR
Go compiles to a single binary. Deploy only the binary. NEVER set `run.base: alpine`. NEVER write go.sum by hand -- let `go mod tidy` handle it.

### Base Image

Includes Go compiler, `git`, `wget`.

**Build != Run**: compiled binary -- deploy only binary, no `run.base` needed (omit it).

### Build Procedure

1. Set `build.base: go@latest`
2. `buildCommands`: ALWAYS use `go mod tidy` before build:
   `go mod tidy && go build -o app .`
3. `deployFiles: app` (single binary)
4. `run.start: ./app`

### Binding

Default `:port` binds all interfaces (correct, no change needed).

### Critical Rules

**NEVER set `run.base: alpine@*`** -- causes glibc/musl mismatch for CGO-linked binaries -> 502. Omit `run.base` or use `run.base: go@latest`.

**NEVER write go.sum by hand** -- checksums will be wrong, build fails with `checksum mismatch`. Let `go mod tidy` in buildCommands generate it.

**Do NOT include go.sum in source** when creating new apps -- `go mod tidy` in buildCommands handles it.

### CGO

Requires `os: ubuntu` + `CGO_ENABLED=1`. When unsure: `CGO_ENABLED=0 go build` for static binary.

### Key Settings

Logger MUST output to `os.Stdout`.
Cache: `~/go` (auto-cached).

### Resource Requirements

**Dev** (compilation on container): `minRam: 1` — `go build` peak ~0.8 GB; autoscaling can't react fast enough for sub-10s spike.
**Stage/Prod**: `minRam: 0.25` — compiled binary, minimal footprint.

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `go run .` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [go mod tidy && go build -o app .]`, `deployFiles: app`, `start: ./app`

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
# Production setup — compile both binaries, deploy minimal
# static artifacts. Development setup — deploy full source
# for SSH-driven development without a pre-build step.
zerops:
  - setup: prod
    build:
      base: go@1.22
      # CGO_ENABLED=0 produces a fully static binary — no C compiler
      # or system libraries linked at runtime. lib/pq is pure Go
      # so this is safe and results in a portable artifact.
      envVariables:
        CGO_ENABLED: "0"
      buildCommands:
        # Download all module dependencies, then build both the
        # app server and the database migration binary.
        - go mod download
        - go build -o app .
        - go build -o migrate ./cmd/migrate
      deployFiles:
        # Deploy only the compiled binaries — no source or toolchain
        # in the runtime image, keeping the artifact minimal.
        - ./app
        - ./migrate
      # cache: true snapshots the global GOMODCACHE (system-level
      # directory), avoiding a full re-download on each build.
      # Not combined with folder-level cache — only one strategy
      # applies per build.
      cache: true

    # Readiness check — verifies each new container responds before
    # the project balancer routes production traffic to it,
    # enabling zero-downtime deploys.
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /

    run:
      # go@1.22 runtime runs the compiled static binary.
      base: go@1.22
      # Run migration once per deploy via zsc execOnce. Placing it
      # here in initCommands — not buildCommands — ensures the
      # schema and the newly deployed binary are always in sync.
      # execOnce prevents parallel execution across containers.
      initCommands:
        - zsc execOnce ${appVersionId} -- ./migrate
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        # Zerops generates db_hostname, db_port, db_user, and
        # db_password automatically for the 'db' service.
        # Reference them via ${hostname_key} syntax.
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
        # DB_NAME matches the Zerops-generated database name
        # (always the service hostname).
        DB_NAME: db
      start: ./app

  - setup: dev
    # Development workspace — source deployed as-is, developer
    # drives compilation and the server via SSH.
    build:
      base: go@1.22
      buildCommands:
        # Download dependencies into the global GOMODCACHE so the
        # build container image snapshot (cache: true) includes
        # them — avoids a network fetch on every build.
        - go mod download
      deployFiles:
        # Deploy the full source tree so the developer has
        # everything available immediately after SSH.
        - ./
      # cache: true snapshots the entire build container image,
      # preserving the global GOMODCACHE across builds.
      cache: true

    run:
      # go@1.22 provides the full Go toolchain for SSH-driven
      # development — compile, run, and test without additional
      # installation.
      base: go@1.22
      # Migration compiles and runs once per deploy (one-time cost,
      # guarded by execOnce). Database is ready when the developer
      # SSHs in — no manual migration step needed.
      initCommands:
        - zsc execOnce ${appVersionId} -- go run cmd/migrate/main.go
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
        DB_NAME: db
        # HOME is required — Zerops runtime processes don't inherit
        # it by default, and Go uses it to locate GOPATH and GOCACHE
        # (/home/zerops/go and /home/zerops/.cache/go-build).
        HOME: /home/zerops
      # Zerops starts nothing — container idles, developer runs
      # 'go run .' or 'go build && ./app' via SSH.
      start: zsc noop --silent
```
