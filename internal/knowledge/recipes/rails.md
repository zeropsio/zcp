# Rails on Zerops

Ruby on Rails with PostgreSQL, Puma web server. Requires SECRET_KEY_BASE and proper 0.0.0.0 binding.

## Keywords
rails, ruby, puma, postgresql, ruby on rails, ror, activerecord, bundler

## TL;DR
Deploy Rails with `bundle install --deployment`, Puma bound to `0.0.0.0:3000`, migrations via `zsc execOnce`.

## zerops.yml (key sections)
```yaml
zerops:
  - setup: app
    build:
      base: ruby@3.4
      buildCommands:
        - bundle install --deployment
        - bundle exec rake assets:precompile
      deployFiles: ./
      cache: [vendor/bundle]
    run:
      envVariables:
        RAILS_ENV: production
        SECRET_KEY_BASE: <@generateRandomString(<64>)>
        DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:5432/${db_dbName}
      initCommands:
        - zsc execOnce migrate-${ZEROPS_appVersionId} -- bin/rails db:migrate
      start: bundle exec puma -b tcp://0.0.0.0:3000
```

## import.yml
```yaml
services:
  - hostname: app
    type: ruby@3.4
    enableSubdomainAccess: true
    minContainers: 1
  - hostname: db
    type: postgresql@17
    mode: NON_HA
    priority: 10
```

## Gotchas
- **SECRET_KEY_BASE** must be set in production — use `<@generateRandomString(<64>)>` preprocessor
- **Puma binding** must include `0.0.0.0` — default `localhost` causes 502 Bad Gateway
- **`--deployment` flag** required for `bundle install` — without it, gems install to system location
- Migrations use `zsc execOnce` with `appVersionId` for idempotency across multi-container deploys
- Asset precompilation runs in build phase (faster, cached)

## See Also
- zerops://foundation/runtimes — Ruby runtime delta
- zerops://foundation/services — PostgreSQL service card
- zerops://foundation/wiring — cross-service env var wiring
