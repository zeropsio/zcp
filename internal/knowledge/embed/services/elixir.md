# Elixir on Zerops

## Keywords
elixir, erlang, otp, phoenix, mix, release, beam, clustering, distributed, ecto

## TL;DR
Elixir on Zerops builds on `elixir@1.16` but runs on `alpine@latest` for minimal footprint. Use Mix releases for deployment with tilde syntax.

## Zerops-Specific Behavior
- Versions: 1.16, 1
- Build base: `elixir@1.16` (Erlang/OTP pre-installed)
- Run base: `alpine@latest` (multi-runtime pattern — compiled BEAM release doesn't need full Elixir)
- Build tool: Mix (pre-installed)
- Working directory: `/var/www`
- No default port — must configure
- Deploy: Mix release (compiled BEAM bytecode) with tilde syntax

## Configuration
```yaml
zerops:
  - setup: myapp
    build:
      base: elixir@1.16
      buildCommands:
        - mix local.hex --force && mix local.rebar --force
        - mix deps.get --only prod
        - MIX_ENV=prod mix compile
        - MIX_ENV=prod mix release
      deployFiles:
        - _build/prod/rel/app/~
      cache:
        - deps
        - _build
    run:
      base: alpine@latest
      start: bin/app start
      ports:
        - port: 4000
          httpSupport: true
      envVariables:
        MIX_ENV: prod
```

### Phoenix Framework
```yaml
zerops:
  - setup: web
    build:
      base: elixir@1.16
      buildCommands:
        - mix local.hex --force && mix local.rebar --force
        - mix deps.get --only prod
        - MIX_ENV=prod mix compile
        - MIX_ENV=prod mix assets.deploy
        - MIX_ENV=prod mix phx.digest
        - MIX_ENV=prod mix release --overwrite
      deployFiles:
        - _build/prod/rel/app/~
      cache:
        - deps
        - _build
    run:
      base: alpine@latest
      start: bin/app start
      ports:
        - port: 4000
          httpSupport: true
      envVariables:
        PHX_SERVER: "true"
        PHX_HOST: myapp.zerops.app
```

## Gotchas
1. **Multi-base pattern**: Build on `elixir@1.16`, run on `alpine@latest` — compiled release doesn't need Elixir runtime
2. **Tilde syntax for deploy**: Use `_build/prod/rel/app/~` to deploy release contents to root
3. **Start with `bin/app start`**: After tilde extraction, the release binary is at `bin/app`
4. **Clustering needs DNS setup**: BEAM clustering across containers requires `libcluster` with DNS strategy
5. **Cache `deps` and `_build`**: Elixir/Erlang compilation is slow — always cache
6. **Set `MIX_ENV=prod`**: Both in build and runtime — affects compilation and behavior
7. **`PHX_SERVER=true` required**: Phoenix releases need this to start the HTTP server

## See Also
- zerops://services/_common-runtime
- zerops://services/gleam
- zerops://examples/zerops-yml-runtimes
