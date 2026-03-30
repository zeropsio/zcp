---
description: "Basic example of running Rust applications on Zerops. Simple Actix Web API with single endpoint that reads from and writes to a PostgreSQL database."
---

# Rust Hello World on Zerops


# Rust Hello World on Zerops


# Rust Hello World on Zerops


# Rust Hello World on Zerops


# Rust Hello World on Zerops


# Rust Hello World on Zerops





## Keywords
rust, cargo, actix-web, axum, warp, binary, zerops.yml, release

## TL;DR
Rust via cargo. ALWAYS build with `--release`. Deploy the single binary via tilde extraction. Binary name comes from Cargo.toml `[package]`.

### Base Image

Includes `cargo` (via Rust base), `git`.

### Build Procedure

1. Set `build.base: rust@latest` (or `rust@1`, `rust@nightly`)
2. `buildCommands: [cargo b --release]` -- ALWAYS `--release` (debug 10-100x slower)
3. `deployFiles: target/release/~app` (tilde extracts binary to `/var/www/`)
4. `run.start: ./app`

### Binding

Most frameworks (actix-web, axum, warp) default to `0.0.0.0` -- verify custom bindings.

### Binary Naming

Name in `Cargo.toml [package]` -> binary at `target/release/{name}` (dashes preserved, e.g., `name = "my-app"` -> `target/release/my-app`).

### Key Settings

Native deps: `apk add --no-cache openssl-dev pkgconfig` in prepareCommands.
Cache: `target/`, `~/.cargo/registry`.

### Resource Requirements

**Dev** (compilation on container): `minRam: 2` — `cargo build` peak ~1.5 GB (link phase is memory-intensive).
**Stage/Prod**: `minRam: 0.25` — compiled binary, minimal footprint.

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `cargo run` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [cargo build --release]`, `deployFiles: target/release/~{binary}`, `start: ./{binary}`

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
zerops:
  # Production setup — compile optimized binaries, deploy minimal footprint.
  # Matching build/runtime Rust stable prevents native linking mismatches.
  - setup: prod
    build:
      base: rust@stable

      # Redirect cargo registry into project tree so Zerops can cache it.
      # build.envVariables persists across all build phases — unlike
      # inline prefixes or prepareCommands exports.
      envVariables:
        CARGO_HOME: ./.cargo

      buildCommands:
        # --release for optimized binary, --locked validates Cargo.lock
        # against Cargo.toml — prevents unexpected dep updates in prod.
        # Builds both 'rust-hello-world' and 'migrate' binaries.
        - cargo build --release --locked

      deployFiles:
        - ./target/release/rust-hello-world
        - ./target/release/migrate

      cache:
        # Paths match CARGO_HOME above. Caching both registry (downloaded
        # sources) and target (compiled deps) speeds up subsequent builds.
        - .cargo/registry
        - target

    # Readiness check: container must serve GET / before the project
    # balancer routes traffic to it — prevents serving during startup.
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /

    run:
      base: rust@stable

      # Migrations run once per deploy version across all containers.
      # In initCommands (not buildCommands) so migration and code deploy
      # atomically — a failed deploy won't leave a migrated DB with old code.
      initCommands:
        - zsc execOnce ${appVersionId} -- ./target/release/migrate

      ports:
        - port: 8080
          httpSupport: true

      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      start: ./target/release/rust-hello-world

  # Development setup — deploy full source for interactive work via SSH.
  # Developer runs 'cargo run' themselves; Zerops prepares the workspace.
  - setup: dev
    build:
      base: rust@stable
      os: ubuntu

      envVariables:
        CARGO_HOME: ./.cargo

      buildCommands:
        # Only fetch dependencies — no compilation.
        # Developer compiles on demand via SSH.
        - cargo fetch

      deployFiles:
        - ./

      cache:
        - .cargo/registry

    run:
      # rust@stable on Ubuntu — developer needs cargo/rustc via SSH,
      # and Ubuntu provides a richer toolset for interactive development.
      base: rust@stable
      os: ubuntu

      # Same execOnce migration pattern as prod. Compiles migration binary
      # on first deploy (one-time cost), then runs it against the database.
      initCommands:
        - zsc execOnce ${appVersionId} -- cargo run --bin migrate

      ports:
        - port: 8080
          httpSupport: true

      envVariables:
        CARGO_HOME: /var/www/.cargo
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # No app started — developer connects via SSH and runs 'cargo run'
      start: zsc noop --silent
```
