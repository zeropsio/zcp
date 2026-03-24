# Rails on Zerops

Ruby runtime with Puma web server. PostgreSQL via DATABASE_URL. Build and run are separate containers.

## Keywords
rails, ror, puma, activerecord

## TL;DR
Ruby runtime, Puma bound to `0.0.0.0:3000`. SECRET_KEY_BASE must be project-level. DATABASE_URL wired from `${hostname_varName}` refs. `bundle install --deployment` required so gems are included in deployFiles.

## SECRET_KEY_BASE

Must be **project-level** (shared across dev+stage). Rails uses it for signing encrypted cookies and sessions — dev and stage must share the same value or sessions break across environments.

**Generate**: `ruby -e "require 'securerandom'; puts SecureRandom.hex(64)"` or `rails secret`
**Set**: `zerops_env project=true variables=["SECRET_KEY_BASE=<generated value>"]`

Do NOT use `envSecrets` in import.yml — generates a different value per service (dev and stage get different keys, breaking session decryption).

## Stack Layers

### Layer 0: Just Rails (no managed services)

Stateless app — APIs. No persistent data.

**import.yml:**
```yaml
services:
  - hostname: appdev
    type: ruby@3.4
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true
```

**zerops.yml:**
```yaml
zerops:
  - setup: appdev
    build:
      base: ruby@3.4
      buildCommands:
        - bundle install --deployment
      deployFiles: ./
      cache: [vendor/bundle]
    run:
      base: ruby@3.4
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        RAILS_ENV: development
        RAILS_LOG_TO_STDOUT: "1"
        RAILS_SERVE_STATIC_FILES: "1"
      start: bundle exec puma -b tcp://0.0.0.0:3000
```

### Layer 1: + Database (PostgreSQL)

**Add to import.yml:**
```yaml
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

**Add/change in zerops.yml envVariables:**
```yaml
        DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
```

`DATABASE_URL` takes precedence over `config/database.yml` in Rails — no additional database config needed.

**Add initCommands:**
```yaml
      initCommands:
        - zsc execOnce migrate-${appVersionId} -- bin/rails db:migrate
```

## Dev vs Stage zerops.yml

Managed services are **shared** — both dev and stage use the same `db`.

| | Dev | Stage |
|---|-----|-------|
| `RAILS_ENV` | `development` | `production` |
| `initCommands` | migrate only | migrate + assets |
| `healthCheck` | omit | port 3000 `/` |
| `readinessCheck` | omit | port 3000 `/` |
| Service refs | `${db_hostname}`, ... | **same** |

Stage zerops.yml additions:
```yaml
zerops:
  - setup: appstage
    build:
      base: ruby@3.4
      buildCommands:
        - bundle install --deployment
        - bundle exec rake assets:precompile
      deployFiles: ./
      cache: [vendor/bundle]
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /
    run:
      base: ruby@3.4
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        RAILS_ENV: production
        RAILS_LOG_TO_STDOUT: "1"
        RAILS_SERVE_STATIC_FILES: "1"
        DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
      initCommands:
        - zsc execOnce migrate-${appVersionId} -- bin/rails db:migrate
      start: bundle exec puma -b tcp://0.0.0.0:3000
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

Asset precompilation runs in the build phase (faster, cached). No Nginx in front of Puma — `RAILS_SERVE_STATIC_FILES` required.

## Reverse Proxy Configuration

Required in `config/environments/production.rb` for Zerops:

```ruby
# Allow traffic through Zerops L7 reverse proxy
# config.hosts.clear allows all hosts — use for dev/testing only.
# For production, whitelist your domain:
#   config.hosts << "yourdomain.com"
#   config.hosts << /.*\.zerops\.app/
config.hosts.clear

# Trust Zerops reverse proxy for correct client IP
config.action_dispatch.trusted_proxies = ActionDispatch::RemoteIp::TRUSTED_PROXIES
```

`config.hosts.clear` disables host authorization entirely. For production, restrict to known domains instead. Rails 6+ blocks all hosts by default — without this, all requests return 422.

## Gotchas

- **SECRET_KEY_BASE must be project-level** — per-service `envSecrets` generates different values for dev and stage, breaking session decryption across environments
- **Puma must bind to `0.0.0.0:3000`** — default `localhost` is unreachable from the Zerops load balancer. Always use `-b tcp://0.0.0.0:3000`
- **`--deployment` flag required for `bundle install`** — without it, gems install to system location and are NOT included in `deployFiles`. The run container gets no gems
- **`config.hosts` blocks all requests by default** (Rails 6+) — clear it or whitelist the Zerops subdomain. Production should whitelist specific domains, not use `config.hosts.clear`
- **RAILS_SERVE_STATIC_FILES=1** — Puma is the only server, no Nginx in front. Required for static asset serving
- **RAILS_LOG_TO_STDOUT=1** — logs written to files are lost on deploy (volatile filesystem). Stdout routes to Zerops log collector
- **No SQLite in production** — container filesystem is replaced on deploy. Always use a database service
- **Migrations use `zsc execOnce`** — handles multi-container deploys. Do not add ActiveRecord advisory locks or other concurrency flags on top
