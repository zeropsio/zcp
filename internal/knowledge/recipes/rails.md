# Rails on Zerops

Ruby on Rails with PostgreSQL, Puma web server, and asset pipeline. Requires SECRET_KEY_BASE and proper 0.0.0.0 binding.

## Keywords
rails, ruby, puma, ruby on rails, ror, activerecord, bundler, rake

## TL;DR
Rails with Puma bound to `0.0.0.0:3000`, PostgreSQL via DATABASE_URL, migrations via `zsc execOnce`, and `SECRET_KEY_BASE` as envSecret.

## zerops.yml

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
        RAILS_SERVE_STATIC_FILES: "1"
        RAILS_LOG_TO_STDOUT: "1"
        DATABASE_URL: postgresql://${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}
      initCommands:
        - zsc execOnce migrate-${appVersionId} -- bin/rails db:migrate
      start: bundle exec puma -b tcp://0.0.0.0:3000
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

## import.yml

```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: ruby@3.4
    enableSubdomainAccess: true
    envSecrets:
      SECRET_KEY_BASE: <@generateRandomString(<64>)>

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

Rails must be configured to accept requests through the Zerops reverse proxy. In `config/environments/production.rb`:

```ruby
# config/environments/production.rb

# Allow all hosts behind Zerops L7 balancer
# Alternatively, restrict to your domain:
#   config.hosts << "yourdomain.com"
config.hosts.clear

# Serve static files since there is no separate web server
config.public_file_server.enabled = ENV["RAILS_SERVE_STATIC_FILES"].present?

# Log to stdout for Zerops log collection
config.logger = ActiveSupport::Logger.new(STDOUT) if ENV["RAILS_LOG_TO_STDOUT"].present?
config.log_level = :info

# Trust the Zerops reverse proxy for correct client IP
config.action_dispatch.trusted_proxies = ActionDispatch::RemoteIp::TRUSTED_PROXIES
```

Database configuration via DATABASE_URL is automatic in Rails. The `DATABASE_URL` env var takes precedence over `config/database.yml` in production.

## Common Failures

- **502 Bad Gateway** -- Puma not binding to `0.0.0.0`. The default `localhost` binding is unreachable from the Zerops load balancer. Use `-b tcp://0.0.0.0:3000`.
- **Blocked host** -- Rails 6+ has host authorization enabled by default. Either clear `config.hosts` or add the Zerops subdomain explicitly.
- **Missing SECRET_KEY_BASE** -- Rails refuses to start in production without it. It is set as envSecret in import.yml and injected automatically.
- **Asset 404 errors** -- `RAILS_SERVE_STATIC_FILES` must be `"1"` since there is no separate Nginx/Apache in front of Puma.
- **Migration conflict on multi-container** -- Without `zsc execOnce`, migrations run on every container simultaneously. Use the `appVersionId` guard.

## Gotchas

- **SECRET_KEY_BASE** must be set in production -- use `<@generateRandomString(<64>)>` in import.yml envSecrets. Rails will not start without it.
- **Puma binding** must include `0.0.0.0` -- default `localhost` causes 502 Bad Gateway from the Zerops load balancer.
- **`--deployment` flag** is required for `bundle install` -- without it, gems install to system location and are not included in deployFiles.
- **config.hosts** in Rails 6+ blocks requests by default. Clear it or add the Zerops subdomain to allow traffic through the reverse proxy.
- **RAILS_SERVE_STATIC_FILES=1** is required because Puma is the only server -- there is no Nginx in front to serve static assets.
- **RAILS_LOG_TO_STDOUT=1** ensures logs are captured by the Zerops logging system instead of being written to files that are lost on deploy.
- **Migrations** use `zsc execOnce` with `appVersionId` for idempotency across multi-container deploys.
- **Asset precompilation** runs in the build phase for faster deploys and to benefit from build cache.
