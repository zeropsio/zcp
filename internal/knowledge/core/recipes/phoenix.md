# Phoenix Framework on Zerops

Elixir Phoenix with release pattern. Build on Elixir, run on Alpine.

## zerops.yml (multi-base: elixir → alpine)
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
        - mix deps.get --only prod
        - mix ecto.create
        - mix ecto.migrate  # Migrations in BUILD
        - mix compile
        - mix assets.deploy
        - mix phx.digest
        - mix release --overwrite
      deployFiles: /
    run:
      base: alpine@latest  # Self-contained release
      envVariables:
        PHX_HOST: ${zeropsSubdomain}  # REQUIRED
        PHX_SERVER: true              # REQUIRED
      start: _build/prod/rel/recipe_phoenix/bin/recipe_phoenix start
```

## import.yml (SECRET_KEY_BASE)
```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    envSecrets:
      SECRET_KEY_BASE: <@generateRandomString(<64>)>
```

## Gotchas
- **Build on elixir@1.16, run on alpine** (releases are self-contained)
- **PHX_HOST=${zeropsSubdomain}** REQUIRED for proper routing
- **PHX_SERVER=true** REQUIRED to start Phoenix in release mode
- Migrations run during BUILD phase (ecto.migrate)
- Assets compiled during build (assets.deploy, phx.digest)
