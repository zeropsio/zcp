# PHP Hello World on Zerops



## Keywords
php, php-nginx, php-apache, composer, laravel, symfony, nette, wordpress, zerops.yml, documentRoot

## TL;DR
Build with `php@X`, run with `php-nginx@X` or `php-apache@X`. Port 80 fixed. Composer pre-installed. Set `documentRoot` and `TRUSTED_PROXIES`.

### Base Image

Includes `composer`, `git`, `wget`, PHP runtime.

### Build != Run

Build `php@X`, run `php-nginx@X` or `php-apache@X`.

**Port**: 80 fixed (exception to 80/443 rule).

### Pre-installed PHP Extensions

Both php-nginx and php-apache images include:
pdo, pdo_pgsql, pdo_mysql, pdo_sqlite, redis, imagick, mongodb, curl, dom, fileinfo, gd, gmp, iconv, intl, ldap, mbstring, opcache, openssl, session, simplexml, sockets, tidy, tokenizer, xml, xmlwriter, zip, soap, imap, igbinary, msgpack.

Use `apk add` only for extensions NOT in this list.

### Build Procedure

1. Set `build.base: php@8.4` (or desired version)
2. If assets needed: `base: [php@8.4, nodejs@22]` (multi-base)
3. `buildCommands`: `composer install --ignore-platform-reqs` (Alpine musl compat)
4. `deployFiles`: include `vendor/`, app files
5. Set `run.base: php-nginx@8.4` (or `php-apache@8.4`)
6. Set `documentRoot` -- Laravel: `public`, WordPress: `""`, Nette: `www/`

### Key Settings

- `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` -- REQUIRED or CSRF breaks. Laravel 11+ requires explicit wiring in `bootstrap/app.php`: `$middleware->trustProxies(at: explode(',', env('TRUSTED_PROXIES', '127.0.0.1')))`
- Alpine extensions: `sudo apk add --no-cache php84-<ext>` (version prefix = PHP major+minor, `sudo` required)
- Cache: `vendor`
- Document root: `documentRoot: public` (Laravel, Symfony) or `documentRoot: www/` (Nette). Default nginx handles PHP routing. Custom config via `siteConfigPath: site.conf.tmpl` only if non-standard rules needed -- use `fastcgi_pass unix:{{.PhpSocket}};` (MUST include `unix:` prefix)

### PHP/FPM Tuning

Override php.ini via `PHP_INI_*` env vars, FPM pool config via `PHP_FPM_*`. Both require **restart** (reload writes config files but FPM does not re-read them). Zerops defaults differ from stock PHP: `upload_max_filesize=1024M`, `post_max_size=1024M`, `display_errors=off`, `log_errors=on`.

For full reference: `zerops_knowledge query="PHP tuning"` -- covers all defaults, FPM dynamic/ondemand modes, upload limit 3-layer chain, and gotchas.

### Resource Requirements

**Dev** (install on container): `minRam: 0.5` — `composer install` peak ~0.3 GB.
**Stage/Prod**: `minRam: 0.25` — PHP-FPM workers are lightweight.

### Common Mistakes

- Missing `documentRoot` -> Nginx doesn't know where to serve from
- Missing `TRUSTED_PROXIES` -> CSRF validation fails behind L7 LB
- Using `php-nginx` as build base -> build needs `php@X`, not the webserver variant
- Apache: `.htaccess` MUST be included in `deployFiles` (Nginx doesn't need it)
- `apk add` without `sudo` -> "Permission denied" in prepareCommands

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, no build step needed (PHP is interpreted)
**Prod deploy**: `buildCommands: [composer install --ignore-platform-reqs]`, `deployFiles: [., vendor/]`

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
zerops:
  # Production setup — optimized Composer install, minimal deploy footprint.
  - setup: prod
    build:
      base: php@8.5
      buildCommands:
        # Install production dependencies only; --no-dev excludes test tools,
        # --optimize-autoloader builds a classmap for faster class resolution.
        - composer install --no-dev --optimize-autoloader
      deployFiles:
        - ./index.php
        - ./migrate.php
        # vendor/ holds the Composer autoloader (and any packages you add).
        - ./vendor
      # Cache vendor/ between builds — Composer restores unchanged packages
      # from cache, skipping redundant network fetches on every deploy.
      cache:
        - vendor

    # Readiness check: new containers must answer HTTP 200 on port 80
    # before the project balancer routes traffic to them. This is what
    # enables zero-downtime deploys (temporaryShutdown: false by default).
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /

    run:
      base: php-apache@8.5
      # PHP-FPM starts via the php-apache base image default (foreground mode).
      # Apache runs alongside it as an OS-level service.
      # No 'start' needed here — the base image default handles it.
      # Run migration exactly once per deploy, regardless of container count.
      # initCommands run per container before traffic is accepted; zsc execOnce
      # ensures one container executes the migration and all others wait.
      # --retryUntilSuccessful handles brief DB startup delays on first deploy.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php migrate.php
      envVariables:
        # DB_NAME matches the PostgreSQL service hostname — a static value,
        # not a generated variable (Zerops names the database after hostname).
        DB_NAME: db
        # The remaining vars reference generated credentials from the 'db'
        # service. Pattern: ${hostname_key} → e.g., ${db_hostname}, ${db_port}.
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

  # Dev setup — deploys full source for live development via SSH.
  # PHP is interpreted per-request: edit files in /var/www and changes
  # take effect immediately — no rebuild or container restart required.
  - setup: dev
    build:
      base: php@8.5
      buildCommands:
        # Install all dependencies including dev packages, so the developer
        # has testing and debugging tools available after SSH.
        - composer install
      deployFiles:
        # Deploy the entire working directory — source files, vendor/,
        # and zerops.yaml so 'zcli push' works from the dev container.
        - ./
      cache:
        - vendor

    run:
      base: php-apache@8.5
      initCommands:
        # Migration runs once per deploy — DB is ready when SSH session starts.
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php migrate.php
      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      # PHP-FPM is the Zerops-managed process for php-apache services —
      # omitting 'start' uses the base image default, which runs PHP-FPM
      # in foreground mode. Apache runs alongside it as an OS service.
      # SSH in and edit PHP files in /var/www; changes take effect on the
      # next request without any restart.
```
