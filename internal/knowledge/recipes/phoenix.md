# Phoenix Framework on Zerops

Elixir Phoenix with release pattern -- build on Elixir, run on Alpine. PostgreSQL via Ecto with DATABASE_URL.

## Keywords
phoenix, elixir, erlang, postgresql, ecto, mix, beam, release, alpine

## TL;DR
Phoenix with Elixir build and Alpine runtime -- `PHX_HOST`, `PHX_SERVER=true`, and `SECRET_KEY_BASE` as envSecret are mandatory.

## zerops.yml

```yaml
zerops:
  - setup: app
    build:
      base: elixir@1.16
      envVariables:
        MIX_ENV: prod
        DATABASE_URL: ${db_connectionString}/${db_dbName}
        SECRET_KEY_BASE: ${RUNTIME_SECRET_KEY_BASE}
      buildCommands:
        - mix local.hex --force
        - mix local.rebar --force
        - mix deps.get --only prod
        - mix compile
        - mix assets.deploy
        - mix phx.digest
        - mix release --overwrite
      deployFiles: ./
      cache:
        - deps
        - _build
    deploy:
      readinessCheck:
        httpGet:
          port: 4000
          path: /
    run:
      base: alpine@latest
      ports:
        - port: 4000
          httpSupport: true
      envVariables:
        DATABASE_URL: ${db_connectionString}/${db_dbName}
        PORT: "4000"
        PHX_HOST: ${zeropsSubdomain}
        POOL_SIZE: "10"
        PHX_SERVER: "true"
      initCommands:
        - zsc execOnce migrate-${appVersionId} -- _build/prod/rel/myapp/bin/myapp eval "MyApp.Release.migrate()"
      start: _build/prod/rel/myapp/bin/myapp start
      healthCheck:
        httpGet:
          port: 4000
          path: /
```

## import.yml

```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: elixir@1.16
    enableSubdomainAccess: true
    envSecrets:
      SECRET_KEY_BASE: <@generateRandomString(<64>)>

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

Phoenix runtime configuration in `config/runtime.exs` must read env vars at startup:

```elixir
# config/runtime.exs
import Config

if config_env() == :prod do
  database_url =
    System.get_env("DATABASE_URL") ||
      raise "DATABASE_URL is not set"

  config :myapp, MyApp.Repo,
    url: database_url,
    pool_size: String.to_integer(System.get_env("POOL_SIZE") || "10")

  secret_key_base =
    System.get_env("SECRET_KEY_BASE") ||
      raise "SECRET_KEY_BASE is not set"

  # PHX_HOST is a full HTTPS URL from zeropsSubdomain — extract bare hostname
  phx_host = System.get_env("PHX_HOST") || "example.com"
  host = case URI.parse(phx_host) do
    %URI{host: h} when is_binary(h) and h != "" -> h
    _ -> phx_host
  end
  port = String.to_integer(System.get_env("PORT") || "4000")

  config :myapp, MyAppWeb.Endpoint,
    url: [host: host, port: 443, scheme: "https"],
    http: [ip: {0, 0, 0, 0}, port: port],
    secret_key_base: secret_key_base,
    server: true
end
```

A release migration module to run Ecto migrations at runtime:

```elixir
# lib/myapp/release.ex
defmodule MyApp.Release do
  @app :myapp

  def migrate do
    load_app()
    for repo <- repos() do
      {:ok, _, _} = Ecto.Migrator.with_repo(repo, &Ecto.Migrator.run(&1, :up, all: true))
    end
  end

  defp repos, do: Application.fetch_env!(@app, :ecto_repos)
  defp load_app, do: Application.load(@app)
end
```

## Common Failures

- **502 Bad Gateway** -- `PHX_SERVER=true` not set. Without it, Phoenix does not start the HTTP endpoint in release mode.
- **Cannot connect to database during build** -- Build-phase `DATABASE_URL` uses `${db_connectionString}` which resolves at build time. If the database service is not ready, the build fails. The `priority: 10` on db ensures it is created first.
- **Runtime crash: SECRET_KEY_BASE not set** -- The envSecret is available via `${RUNTIME_SECRET_KEY_BASE}` in build env vars. At runtime it is injected directly by Zerops as `SECRET_KEY_BASE`.
- **Wrong release name in start command** -- The release name in `_build/prod/rel/myapp/bin/myapp` must match the app name in `mix.exs`. Replace `myapp` with the actual app name.
- **Assets not found** -- Both `mix assets.deploy` and `mix phx.digest` must run during build. Missing either causes missing CSS/JS in production.

## Gotchas

- **Build on elixir, run on alpine** -- Phoenix releases are self-contained BEAM binaries. The runtime uses `alpine@latest` (no Elixir/Erlang needed).
- **PHX_HOST=${zeropsSubdomain}** is a full HTTPS URL (e.g. `https://app-1df2.prg1.zerops.app`). Phoenix's `url: [host: ...]` expects a bare hostname, so `runtime.exs` must extract it via `URI.parse`.
- **PHX_SERVER=true** is required to start the HTTP server in release mode. Without it, the endpoint is not started.
- **SECRET_KEY_BASE** is generated as envSecret in import.yml. In `build.envVariables`, reference it as `${RUNTIME_SECRET_KEY_BASE}` to access the runtime secret during build.
- **Migrations run at runtime** via `zsc execOnce` in initCommands using the release eval command, not during build. This ensures the database is available and migrations run exactly once per deploy.
- **DATABASE_URL** uses the `${db_connectionString}/${db_dbName}` pattern which includes the full `ecto://user:pass@host:port` prefix.
- **POOL_SIZE** should match or be less than the PostgreSQL max_connections. Default `10` works for NON_HA single-container setups.
- Replace `myapp` and `MyApp` with the actual application name from `mix.exs` throughout the zerops.yml and configuration files.
