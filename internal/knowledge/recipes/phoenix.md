# Phoenix Framework on Zerops

Elixir Phoenix with release pattern -- build on Elixir, run on Alpine. PostgreSQL via Ecto, runtime migrations via `zsc execOnce`.

## Keywords
phoenix, liveview, ecto, beam, release, mix

## TL;DR
Phoenix on `elixir@1.16` build + `alpine@latest` runtime. `PHX_SERVER=true`, `PHX_HOST=${zeropsSubdomain}`, `SECRET_KEY_BASE` as envSecret. Deploy release with tilde syntax. Migrations at runtime via `zsc execOnce`.

## zerops.yml

```yaml
zerops:
  - setup: app
    build:
      base: elixir@1.16
      envVariables:
        MIX_ENV: prod
        DATABASE_URL: ${db_connectionString}
        SECRET_KEY_BASE: ${RUNTIME_SECRET_KEY_BASE}
      buildCommands:
        - mix local.hex --force
        - mix local.rebar --force
        - mix deps.get --only prod
        - mix compile
        - mix assets.deploy
        - mix phx.digest
        - mix release --overwrite
      deployFiles: _build/prod/rel/myapp/~
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
        DATABASE_URL: ${db_connectionString}
        PORT: "4000"
        PHX_HOST: ${zeropsSubdomain}
        PHX_SERVER: "true"
        POOL_SIZE: "10"
      initCommands:
        - zsc execOnce migrate-${appVersionId} -- bin/myapp eval "MyApp.Release.migrate()"
      start: bin/myapp start
      healthCheck:
        httpGet:
          port: 4000
          path: /
```

Replace `myapp` and `MyApp` with the actual app name from `mix.exs` throughout.

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

## Runtime configuration

`config/runtime.exs` reads env vars at container start -- not at compile time:

```elixir
import Config

if config_env() == :prod do
  config :myapp, MyApp.Repo,
    url: System.get_env("DATABASE_URL") || raise("DATABASE_URL not set"),
    pool_size: String.to_integer(System.get_env("POOL_SIZE") || "10")

  # PHX_HOST from zeropsSubdomain is a full HTTPS URL -- extract bare hostname
  phx_host = System.get_env("PHX_HOST") || "example.com"
  host = case URI.parse(phx_host) do
    %URI{host: h} when is_binary(h) and h != "" -> h
    _ -> phx_host
  end

  config :myapp, MyAppWeb.Endpoint,
    url: [host: host, port: 443, scheme: "https"],
    http: [ip: {0, 0, 0, 0}, port: String.to_integer(System.get_env("PORT") || "4000")],
    secret_key_base: System.get_env("SECRET_KEY_BASE") || raise("SECRET_KEY_BASE not set"),
    server: true
end
```

Release migration module (needed for `initCommands` eval):

```elixir
# lib/myapp/release.ex
defmodule MyApp.Release do
  @app :myapp

  def migrate do
    Application.load(@app)
    for repo <- Application.fetch_env!(@app, :ecto_repos) do
      {:ok, _, _} = Ecto.Migrator.with_repo(repo, &Ecto.Migrator.run(&1, :up, all: true))
    end
  end
end
```

## Gotchas

- **`deployFiles: _build/prod/rel/myapp/~`** -- tilde deploys release contents to container root. Replace `myapp` with the actual app name. The `start` and `initCommands` eval paths must match. Using `./` deploys the entire source tree including `_build` -- do not use `./`.
- **`PHX_SERVER=true`** -- without this, Phoenix does not start the HTTP endpoint in release mode. The app starts but serves no requests.
- **`PHX_HOST=${zeropsSubdomain}`** -- `zeropsSubdomain` is a full HTTPS URL (e.g. `https://app-1df2.prg1.zerops.app`). Phoenix's `url: [host: ...]` expects a bare hostname. Extract it in `runtime.exs` via `URI.parse` as shown above.
- **`SECRET_KEY_BASE` in build env** -- the envSecret is injected at runtime as `SECRET_KEY_BASE`. During build, reference it as `${RUNTIME_SECRET_KEY_BASE}` (Zerops-specific naming for runtime secrets accessed at build time).
- **`DATABASE_URL: ${db_connectionString}`** -- the PostgreSQL `connectionString` var already includes `postgresql://user:pass@host:port/dbname`. Do NOT append `/${db_dbName}` -- that doubles the database name in the path.
- **Migrations at runtime, not build** -- Phoenix uses a release eval command in `initCommands` with `zsc execOnce`. This ensures the database is available and migrations run exactly once per deploy version across all containers.
- **`SECRET_KEY_BASE` scope** -- if dev and stage are separate Phoenix services each needing their own key (they can differ), per-service `envSecrets` is correct. If they must share a key (unusual for Phoenix), use a project-level env var instead.
- **Build on Elixir, run on Alpine** -- Phoenix releases are self-contained BEAM binaries. `alpine@latest` at runtime needs no Elixir or Erlang.
- **`mix assets.deploy` + `mix phx.digest`** -- both required for production asset compilation. Missing either causes missing CSS/JS. `assets.deploy` bundles; `phx.digest` fingerprints.
