# Ruby Hello World on Zerops



## Keywords
ruby, rails, bundler, gem, puma, zerops.yml, bundle, Gemfile

## TL;DR
Ruby with bundler/gem/git pre-installed. Use `bundle install --deployment` with lockfile. Bind `tcp://0.0.0.0:3000`. Rails needs `RAILS_ENV=production`.

### Base Image

Includes Ruby, `bundler`, `gem`, `git`.

### Build Procedure

1. Set `build.base: ruby@3.4`
2. `buildCommands`:
   - With Gemfile.lock: `bundle install --deployment` (deterministic, production-ready)
   - Without Gemfile.lock: `bundle install --path vendor/bundle` (--deployment FAILS without lockfile)
3. `deployFiles: ./` (entire source + vendor/bundle)
4. `run.start: bundle exec puma -b tcp://0.0.0.0:3000`

### Key Settings

Cache: `vendor/bundle`.

### Rails Specifics

- `RAILS_ENV: production`, `SECRET_KEY_BASE` via preprocessor
- Migrations: `zsc execOnce migrate-${appVersionId} -- bin/rails db:migrate`
- Assets: `bundle exec rake assets:precompile` in buildCommands

### Resource Requirements

**Dev** (install on container): `minRam: 1` — `bundle install` + asset compilation peak ~0.8 GB.
**Stage/Prod**: `minRam: 0.5` — Puma workers need baseline allocation.

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `run.prepareCommands: [bundle install --path vendor/bundle]`, `start: zsc noop --silent` (idle container -- agent starts `bundle exec ruby app.rb` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [bundle install --deployment]`, `deployFiles: [.]`, `start: bundle exec puma -b tcp://0.0.0.0:3000`

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
zerops:

  # Production setup — bundle only runtime gems, deploy minimal
  # artifacts. Used for stage and all production environments.
  - setup: prod

    build:
      base: ruby@3.4

      # BUNDLE_DEPLOYMENT=1 requires Gemfile.lock (reproducible),
      # installs gems to vendor/bundle, and skips development gems
      # — so only production dependencies are bundled and deployed.
      envVariables:
        BUNDLE_PATH: vendor/bundle
        BUNDLE_DEPLOYMENT: "1"
        BUNDLE_WITHOUT: development

      buildCommands:
        - bundle install

      # Deploy the bundled gems alongside source and the migration
      # script. Gemfile + lock must be present so 'bundle exec'
      # resolves gems at runtime without re-installing them.
      deployFiles:
        - ./vendor
        - ./Gemfile
        - ./Gemfile.lock
        - ./config.ru
        - ./src
        - ./migrate.rb

      # Restore vendor/bundle from the previous build — bundle
      # install then only fetches gems that changed in Gemfile.lock.
      cache:
        - vendor

    # Readiness check: Zerops polls GET / before routing traffic to
    # a new runtime container, ensuring zero-downtime deployments.
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /

    run:
      base: ruby@3.4

      # Run the migration once per deploy across all containers.
      # initCommands execute before 'start' — the database schema
      # is ready when the app boots. zsc execOnce prevents parallel
      # containers from racing to run the same migration.
      # --retryUntilSuccessful handles transient DB startup delays.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- bundle exec ruby migrate.rb

      ports:
        - port: 8080
          httpSupport: true

      envVariables:
        RACK_ENV: production
        # Tell bundle exec where the vendored gems are and that
        # the development group was excluded at build time —
        # prevents Bundler from failing on missing dev gems.
        BUNDLE_PATH: vendor/bundle
        BUNDLE_WITHOUT: development
        DB_NAME: db
        # Referencing variables: ${db_hostname} resolves to the
        # 'db' service's internal hostname — Zerops injects these
        # at runtime from the database service's generated vars.
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # Puma is the production Rack server. -b tcp://0.0.0.0
      # ensures it binds to all interfaces, not just localhost.
      start: bundle exec puma -p 8080 -b tcp://0.0.0.0

  # Development setup — deploy full source for live SSH
  # development. The container stays idle ('zsc noop'); the
  # developer SSHs in and starts the app manually after Zerops
  # runs the migration and prepares the workspace.
  - setup: dev

    build:
      base: ruby@3.4

      # Install all gems (including dev group) into vendor/bundle
      # so they are available when the developer SSHs in — no
      # 'bundle install' needed after connecting.
      envVariables:
        BUNDLE_PATH: vendor/bundle

      buildCommands:
        - bundle install

      # Deploy the entire working directory — source, vendor, and
      # zerops.yaml — so the developer can edit and push to other
      # services directly from the SSH session.
      deployFiles: ./

      cache:
        - vendor

    run:
      base: ruby@3.4

      # Migration runs once per deploy — database schema and seed
      # data are ready when the developer SSHs in.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- bundle exec ruby migrate.rb

      ports:
        - port: 8080
          httpSupport: true

      envVariables:
        RACK_ENV: development
        BUNDLE_PATH: vendor/bundle
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # Container stays idle — start the app from SSH:
      #   bundle exec puma -p 8080 -b tcp://0.0.0.0
      # For auto-reload during development:
      #   bundle exec rerun -- puma -p 8080 -b tcp://0.0.0.0
      start: zsc noop --silent
```
