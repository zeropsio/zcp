# Gleam on Zerops

Gleam compiles to Erlang and runs as a self-contained release with PostgreSQL.

## Keywords
gleam, erlang, beam, wisp, mist, functional, postgresql

## TL;DR
Gleam with `gleam export erlang-shipment` and PostgreSQL -- creates a self-contained BEAM release, uses `DATABASE_URL` for connection.

## zerops.yml

```yaml
zerops:
  - setup: api
    build:
      base: gleam@1.5
      buildCommands:
        - gleam export erlang-shipment
      deployFiles: build/erlang-shipment/~
      cache:
        - build
        - _gleam_artefacts
    run:
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        DATABASE_URL: ${db_connectionString}/${db_dbName}
      healthCheck:
        httpGet:
          port: 3000
          path: /status
      start: ./entrypoint.sh run
```

## import.yml

```yaml
services:
  - hostname: api
    type: gleam@1.5
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

The erlang-shipment produces a self-contained release with an `entrypoint.sh` script. The deployed files at `/var/www/` include the entrypoint and all BEAM bytecode.

Health check endpoint (Wisp + Mist example):

```gleam
// In your router
fn handle_request(req: Request) -> Response {
  case wisp.path_segments(req) {
    ["status"] -> wisp.ok() |> wisp.string_body("{\"status\":\"ok\"}")
    _ -> wisp.not_found()
  }
}
```

Database connection -- Gleam libraries (e.g., `gleam_pgo`) typically accept a URL:

```gleam
let assert Ok(db_url) = os.get_env("DATABASE_URL")
let assert Ok(config) = pgo.url_config(db_url)
let db = pgo.connect(config)
```

Bind address -- configure your web framework (Wisp, Mist) to listen on `0.0.0.0:3000`.

## Gotchas

- **Erlang shipment is self-contained** -- `gleam export erlang-shipment` bundles the BEAM runtime; no Erlang/OTP needed on the runtime container
- **Deploy with tilde** -- `build/erlang-shipment/~` extracts the shipment contents directly into `/var/www/` so `entrypoint.sh` is at the root
- **Bind `0.0.0.0`** -- configure Wisp/Mist to bind all interfaces; binding `127.0.0.1` prevents the L7 balancer from reaching your app
- **Cache build artifacts** -- cache both `build` and `_gleam_artefacts` directories for faster incremental builds
- **DATABASE_URL format** -- uses `${db_connectionString}/${db_dbName}` which resolves to a full PostgreSQL connection URL including credentials
- **Use `gleam@1.5` in import.yml** -- do not use `gleam@latest` in the import type field; use `gleam@1.5` for the specific version
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
