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
