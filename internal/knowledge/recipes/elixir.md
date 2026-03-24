# Elixir/Plug on Zerops

Plain Elixir with Plug/Cowboy HTTP server and PostgreSQL via Ecto -- build on Elixir, run on Alpine release.

## Keywords
plug, cowboy, ecto, beam, release, mix

## TL;DR
Elixir Plug/Cowboy API with PostgreSQL -- mix release built on `elixir@1.16`, runs on `alpine@latest`. Deploy release contents with tilde syntax. Port 4000.

## zerops.yml

```yaml
zerops:
  - setup: app
    build:
      base: elixir@1.16
      envVariables:
        MIX_ENV: prod
        DATABASE_URL: ${db_connectionString}
      buildCommands:
        - mix deps.get --only prod
        - mix ecto.create
        - mix ecto.migrate
        - mix compile
        - mix release --overwrite
      deployFiles: _build/prod/rel/app/~
      cache:
        - deps
        - _build
    run:
      base: alpine@latest
      ports:
        - port: 4000
          httpSupport: true
      envVariables:
        DATABASE_URL: ${db_connectionString}
        PORT: "4000"
        POOL_SIZE: "10"
      start: bin/app start
      healthCheck:
        httpGet:
          port: 4000
          path: /
```

## import.yml

```yaml
services:
  - hostname: app
    type: elixir@1.16
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Ecto runtime configuration

`config/runtime.exs` must read `DATABASE_URL` at startup (not compile time):

```elixir
import Config

if config_env() == :prod do
  config :app, App.Repo,
    url: System.get_env("DATABASE_URL"),
    pool_size: String.to_integer(System.get_env("POOL_SIZE") || "10")
end
```

## Gotchas

- **Build on Elixir, run on Alpine** -- Elixir releases are self-contained BEAM binaries. The runtime uses `alpine@latest` (no Elixir/Erlang needed at runtime).
- **`deployFiles: _build/prod/rel/app/~`** -- the trailing `~` deploys the contents of the release directory to the container root. Replace `app` with the actual app name from `mix.exs`. The `start` command must match: `bin/app start`.
- **`DATABASE_URL: ${db_connectionString}`** -- the PostgreSQL `connectionString` var already includes the full `postgresql://user:pass@host:port/dbname` URL. Do NOT append `/${db_dbName}` -- that doubles the database name in the path.
- **Migrations during build** -- `mix ecto.create` and `mix ecto.migrate` run during build because plain Plug apps typically don't define a release migration module. The `priority: 10` on db ensures the database exists before the build runs. For runtime migrations, use `initCommands` with `zsc execOnce` and a release eval command instead.
- **`DATABASE_URL` in both build and run** -- Ecto needs the URL during build for `ecto.create`/`ecto.migrate` and at runtime for the connection pool. Set it in both `build.envVariables` and `run.envVariables`.
- **`start: bin/app start`** -- the binary name must match the `:app` name in `mix.exs`. If `mix.exs` defines `:myapp`, use `bin/myapp start`. Mismatch causes 502.
- **POOL_SIZE** should not exceed PostgreSQL `max_connections`. Default `10` is safe for NON_HA single-container.
