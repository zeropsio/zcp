# Elixir/Plug on Zerops

Plain Elixir with Plug/Cowboy HTTP server and PostgreSQL via Ecto -- build on Elixir, run on Alpine release.

## Keywords
elixir, plug, cowboy, postgresql, ecto, mix, beam, release, alpine, api

## TL;DR
Elixir Plug/Cowboy API with PostgreSQL -- mix release on Alpine, `DATABASE_URL` via Ecto, port 4000.

## zerops.yml

```yaml
zerops:
  - setup: app
    build:
      base: elixir@1.16
      envVariables:
        MIX_ENV: prod
        DATABASE_URL: ${db_connectionString}/${db_dbName}
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
        DATABASE_URL: ${db_connectionString}/${db_dbName}
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

## Configuration

Elixir application module starts the Plug/Cowboy HTTP server and Ecto repo:

```elixir
# lib/app/application.ex
defmodule App.Application do
  use Application

  def start(_type, _args) do
    children = [
      App.Repo,
      {Plug.Cowboy, scheme: :http, plug: App.Router, options: [port: port()]}
    ]

    opts = [strategy: :one_for_one, name: App.Supervisor]
    Supervisor.start_link(children, opts)
  end

  defp port, do: String.to_integer(System.get_env("PORT") || "4000")
end
```

Ecto repo configuration reads DATABASE_URL at runtime:

```elixir
# lib/app/repo.ex
defmodule App.Repo do
  use Ecto.Repo,
    otp_app: :app,
    adapter: Ecto.Adapters.Postgres
end

# config/runtime.exs
import Config

if config_env() == :prod do
  config :app, App.Repo,
    url: System.get_env("DATABASE_URL"),
    pool_size: String.to_integer(System.get_env("POOL_SIZE") || "10")
end
```

## Common Failures

- **502 Bad Gateway** -- the release binary name in `start: bin/app start` must match the app name in `mix.exs`. If `mix.exs` defines `:myapp`, use `bin/myapp start`.
- **Cannot connect to database during build** -- `mix ecto.create` and `mix ecto.migrate` run during build using `${db_connectionString}`. The `priority: 10` on db ensures it exists first.
- **Release crash: DATABASE_URL not set** -- ensure `DATABASE_URL` is in both `build.envVariables` and `run.envVariables`.

## Gotchas

- **Build on Elixir, run on Alpine** -- Elixir releases are self-contained BEAM binaries. The runtime uses `alpine@latest` (no Elixir/Erlang needed at runtime).
- **Migrations during build** -- unlike Phoenix, this recipe runs `mix ecto.create` and `mix ecto.migrate` during build since there is no release eval command. For runtime migrations, add a release module and use `initCommands` with `zsc execOnce`.
- **deployFiles trailing tilde** -- `_build/prod/rel/app/~` deploys the contents of the release directory to the root of the runtime container.
- **DATABASE_URL** uses the `${db_connectionString}/${db_dbName}` pattern which resolves to the full `ecto://user:pass@host:port/dbname` URL.
- **POOL_SIZE** should match or be less than PostgreSQL max_connections. Default `10` works for NON_HA single-container setups.
- Replace `app` in the release path and start command with the actual application name from `mix.exs`.
