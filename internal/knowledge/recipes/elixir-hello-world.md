---
description: "A minimal Elixir web application built with Plug and Cowboy that connects to a PostgreSQL database, runs Ecto migrations on deploy, and exposes a single health check endpoint at `/`. Used within Elixir Hello World recipe for Zerops platform."
---

# Elixir Hello World on Zerops


# Elixir Hello World on Zerops


# Elixir Hello World on Zerops


# Elixir Hello World on Zerops





## Keywords
elixir, mix, hex, phoenix, erlang, release, zerops.yml, mix.exs

## TL;DR
Elixir with mix/hex/rebar pre-installed. Build = Run base. Deploy a Mix release. Set `PHX_SERVER=true` + `MIX_ENV=prod` for Phoenix.

### Base Image

Includes `mix`, `hex`, `rebar`, `npm`, `yarn`, `git`, `npx`.

**Build = Run**: both use `elixir@latest`.

### Build Procedure

1. Set `build.base: elixir@latest`
2. `buildCommands: [mix deps.get --only prod, mix compile, mix release]`
3. `deployFiles: _build/prod/rel/{app_name}/~` -- release name = mix.exs `app:` property (e.g. `:my_app` -> `_build/prod/rel/my_app/~`)
4. `run.start: bin/{app_name} start` -- same name as mix.exs app

### Required Environment

`PHX_SERVER=true` + `MIX_ENV=prod`

### Phoenix-Specific

Also set `PHX_HOST=${zeropsSubdomain}` (full HTTPS URL -- extract hostname in runtime.exs via `URI.parse`).

### Key Settings

Cache: `deps`, `_build`.

### Resource Requirements

**Dev** (compilation on container): `minRam: 1` — `mix compile` + release build peak ~0.8 GB.
**Stage/Prod**: `minRam: 0.25` — BEAM VM lightweight for most apps.

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `run.prepareCommands: [mix deps.get]`, `start: zsc noop --silent` (idle container -- agent starts `mix run --no-halt` manually via SSH for iteration)
**Prod deploy**: build release, deploy extracted release, `start: bin/{app_name} start`

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
zerops:
  # Production setup — compile a self-contained OTP release,
  # deploy the release artifact to a minimal Alpine runtime.
  # The release bundles the Erlang runtime, so no Elixir
  # image is needed on the runtime container.
  - setup: prod
    build:
      base: elixir@1.16
      envVariables:
        # MIX_ENV must be set during build so the release
        # is compiled for production (no dev/test deps).
        MIX_ENV: prod
      buildCommands:
        # Fetch only production deps, compile, then create
        # a self-contained OTP release that includes the
        # Erlang runtime for portability.
        - mix deps.get --only prod
        - mix compile
        - mix release --overwrite
      deployFiles:
        # The ~ strips the path prefix: contents of
        # _build/prod/rel/app/ land directly at /var/www/.
        # bin/app, lib/, erts-*/, releases/ all deploy
        # to the runtime container root.
        - _build/prod/rel/app/~
      cache:
        # Cache extracted source deps and compiled artifacts.
        # Subsequent builds skip re-downloading and
        # re-compiling unchanged dependencies.
        - deps
        - _build

    # Readiness check: verifies new containers are healthy
    # before the project balancer routes traffic to them.
    deploy:
      readinessCheck:
        httpGet:
          port: 4000
          path: /

    run:
      # Alpine runtime — the OTP release is self-contained,
      # no Elixir or Erlang installation required here.
      base: alpine@latest

      # Run migration once per deploy (zsc execOnce ensures
      # exactly one container executes across all replicas,
      # preventing race conditions). In initCommands — not
      # buildCommands — so migration and new code deploy
      # atomically. --retryUntilSuccessful handles the brief
      # window where the database isn't reachable yet.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- ./bin/app eval "App.Release.migrate()"

      ports:
        - port: 4000
          httpSupport: true

      envVariables:
        MIX_ENV: prod
        PORT: "4000"
        POOL_SIZE: "10"
        # Zerops injects these from the 'db' service.
        # Variable names follow the {hostname}_{key} pattern.
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
        DB_NAME: db

      start: ./bin/app start

  # Dev setup — deploy full source with all deps so the
  # developer can SSH in and start working immediately.
  # Zerops prepares the workspace; the developer drives
  # application startup via SSH.
  - setup: dev
    build:
      base: elixir@1.16
      envVariables:
        MIX_ENV: dev
        # HOME is needed in build so Mix can locate its
        # archives when compiling deps.
        HOME: /home/zerops
      buildCommands:
        # Fetch all deps (including dev/test) and compile
        # the full project. Deploying pre-compiled _build/
        # means initCommands can run migrations without
        # recompiling from source on the runtime container.
        - mix local.hex --force
        - mix local.rebar --force
        - mix deps.get
        - mix compile
      deployFiles:
        # Deploy the entire working directory — source,
        # pre-fetched deps, and compiled beam files all
        # land at /var/www/ for immediate SSH use.
        - ./
      cache:
        # Cache dep sources for faster subsequent deploys.
        - deps

    run:
      # Use the Elixir image for dev — provides mix, iex,
      # and the full Erlang toolchain for SSH development.
      base: elixir@1.16

      # Install Hex in the runtime container so 'mix'
      # commands work without interactive prompts. Runs
      # once per container creation (cached in image).
      prepareCommands:
        - mix local.hex --force
        - mix local.rebar --force

      # Run migration once — database is ready when the
      # developer SSHs in (no manual setup required).
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- mix ecto.migrate

      ports:
        - port: 4000
          httpSupport: true

      envVariables:
        MIX_ENV: dev
        PORT: "4000"
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
        DB_NAME: db
        # Mix and Erlang require HOME to locate ~/.mix and
        # ~/.erlang.cookie. Zerops runtime containers don't
        # inherit HOME — set it explicitly.
        HOME: /home/zerops

      # Container stays idle — developer starts the app
      # manually: mix run --no-halt  or  iex -S mix
      start: zsc noop --silent
```
