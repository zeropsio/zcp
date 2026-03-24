# Ruby on Zerops

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
