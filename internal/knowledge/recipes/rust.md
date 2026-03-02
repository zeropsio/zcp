# Rust on Zerops

Compiled Rust binary with PostgreSQL. Build with cargo, deploy only the release binary.

## Keywords
rust, cargo, actix, axum, rocket, compiled, binary, postgresql

## TL;DR
Rust with `cargo build --release` and PostgreSQL -- deploy only the binary, bind `0.0.0.0`, cache `target/` for faster rebuilds.

## zerops.yml

```yaml
zerops:
  - setup: api
    build:
      base: rust@stable
      buildCommands:
        - cargo build --release
      deployFiles:
        - ./target/release/~/rust
      cache: target
    run:
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      healthCheck:
        httpGet:
          port: 8080
          path: /status
      start: ./rust
```

## import.yml

```yaml
services:
  - hostname: api
    type: rust@stable
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

Database connection in Rust code:

```rust
// Read env vars set by zerops.yml
let db_host = std::env::var("DB_HOST").unwrap();
let db_port = std::env::var("DB_PORT").unwrap();
let db_user = std::env::var("DB_USER").unwrap();
let db_pass = std::env::var("DB_PASS").unwrap();
let db_name = std::env::var("DB_NAME").unwrap();

let url = format!("postgres://{}:{}@{}:{}/{}", db_user, db_pass, db_host, db_port, db_name);
```

Health check endpoint (Actix example):

```rust
#[get("/status")]
async fn status() -> impl Responder {
    HttpResponse::Ok().json(serde_json::json!({"status": "ok"}))
}
```

Bind address -- must listen on `0.0.0.0`, not `127.0.0.1`:

```rust
HttpServer::new(|| App::new().service(status))
    .bind("0.0.0.0:8080")?
    .run()
    .await
```

## Gotchas

- **Bind `0.0.0.0`** -- Rust HTTP servers (Actix, Axum, Rocket) must bind `0.0.0.0`, not `127.0.0.1`, or Zerops L7 balancer cannot reach them
- **Deploy only the binary** -- `./target/release/~/rust` uses the tilde wildcard to deploy just the compiled binary to `/var/www/rust`; do not deploy the entire `target/` directory
- **Cache `target/`** -- Rust builds are slow; caching `target` between builds avoids recompiling dependencies from scratch
- **Logger must output to stdout** -- Zerops collects logs from stdout/stderr only; configure your logger (env_logger, tracing) accordingly
- **Binary name must match** -- the binary name in `deployFiles` and `start` must match the `[[bin]]` name in `Cargo.toml` (defaults to the package name)
