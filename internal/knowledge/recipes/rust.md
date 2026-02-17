# Rust on Zerops

Compiled Rust binary. Build with cargo, deploy the release binary.

## Keywords
rust, cargo, actix, axum, rocket, compiled, binary

## TL;DR
Rust with `cargo build --release` — deploy only the binary, bind `0.0.0.0`.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: rust@latest
      buildCommands:
        - cargo build --release
      deployFiles: target/release/app
    run:
      ports:
        - port: 8080
          httpSupport: true
      start: ./app
```

## import.yml
```yaml
services:
  - hostname: app
    type: rust@stable
    enableSubdomainAccess: true
```

## Gotchas
- **Bind 0.0.0.0** — Rust HTTP servers (Actix, Axum, Rocket) must bind `0.0.0.0`, not `127.0.0.1`
- **Compiled binary needs no runtime base** — the release binary is self-contained
- **Deploy only the binary** — no need to deploy the entire `target/` directory
- **Build times can be long** — use `cache: [target/]` to cache dependencies between builds
- **Logger must output to stdout** for Zerops log collection
