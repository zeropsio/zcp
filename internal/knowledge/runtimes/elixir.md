# Elixir on Zerops

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

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `run.prepareCommands: [mix deps.get]`, `start: zsc noop --silent` (idle container -- agent starts `mix run --no-halt` manually via SSH for iteration)
**Prod deploy**: build release, deploy extracted release, `start: bin/{app_name} start`
