# PHP on Zerops

## Keywords
php, laravel, wordpress, composer, nginx, apache, php-fpm, php-nginx, php-apache, document root

## TL;DR
PHP on Zerops comes in two run variants: `php-nginx` (recommended) and `php-apache`. Build always uses generic `php@<version>`. Serves from `/var/www` with configurable document root.

## Zerops-Specific Behavior
- Versions: 8.1, 8.3, 8.4
- Build base: `php@<version>` (generic, for Composer)
- Run variants: `php-nginx` (recommended), `php-apache`
- Base: Alpine (default)
- Package manager: Composer (pre-installed)
- Document root: configurable (default `/var/www`)
- PHP extensions: Install via `prepareCommands` using `apk add` (not docker-php-ext-install)
- PHP services use port 80 (exception to the 80/443 reservation rule)

## Configuration
```yaml
zerops:
  - setup: myapp
    build:
      base: php@8.4
      buildCommands:
        - composer install --ignore-platform-reqs
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
    run:
      base: php-nginx@8.4
      documentRoot: public
      ports:
        - port: 80
          httpSupport: true
```

## Framework Patterns

### Laravel
```yaml
zerops:
  - setup: app
    build:
      base: php@8.4
      buildCommands:
        - composer install --ignore-platform-reqs
        - php artisan config:cache
        - php artisan route:cache
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
    run:
      base: php-nginx@8.4
      documentRoot: public
      envVariables:
        APP_ENV: production
        TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"
```

### Laravel Jetstream (with Node.js assets)
```yaml
zerops:
  - setup: app
    build:
      base:
        - php@8.4
        - nodejs@18
      buildCommands:
        - composer install --ignore-platform-reqs
        - npm install
        - npm run build
        - php artisan config:cache
        - php artisan route:cache
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
        - node_modules
        - package-lock.json
    run:
      base: php-nginx@8.4
      documentRoot: public
```

### WordPress
```yaml
zerops:
  - setup: app
    build:
      base: php@8.4
      buildCommands:
        - composer install --no-dev
      deployFiles: ./
    run:
      base: php-apache@8.4
      documentRoot: ""
```

## Apache vs Nginx Variants

| Variant | Runtime type | Use when |
|---------|-------------|----------|
| `php-nginx@8.4` | Nginx + PHP-FPM | Modern frameworks (Laravel, Symfony) — **recommended** |
| `php-apache@8.4` | Apache + mod_php | Apps requiring `.htaccess` (WordPress, legacy PHP) |

**Build base is always generic `php@<version>`** — the variant only matters for `run.base`.

## Custom Nginx Config

Use `siteConfigPath` to provide custom Nginx configuration:

```yaml
run:
  siteConfigPath: nginx.conf
  documentRoot: public
```

## Trusted Proxies

Zerops routes traffic through a reverse proxy. Frameworks must trust it:

```yaml
# Laravel
envVariables:
  TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"

# Symfony
envVariables:
  TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"
```

Without this, CSRF validation, HTTPS detection, and client IP resolution break.

## Logging

Use syslog for structured logging (Zerops captures syslog output):

```php
// Laravel: config/logging.php — use 'syslog' channel
// Symfony: monolog.yaml — use SyslogHandler
```

## Sessions

**File-based sessions break with multiple containers.** Use Redis/Valkey:

```yaml
envVariables:
  SESSION_DRIVER: redis
  REDIS_HOST: cache
  REDIS_PORT: "6379"
envSecrets:
  REDIS_PASSWORD: ${cache_password}
```

## Gotchas
1. **Document root matters**: Laravel needs `public`, WordPress uses root — misconfigured doc root = 404
2. **Cache vendor + composer.lock**: Add both to `build.cache` for faster builds
3. **No `docker-php-ext-install`**: Zerops PHP is not Docker-based. Install extensions via `apk add php-<ext>` in prepareCommands
4. **php-nginx vs php-apache**: Use `php-nginx` unless your app specifically requires Apache (.htaccess)
5. **Trusted proxies required**: Without proxy config, CSRF breaks behind Zerops L7 balancer
6. **File sessions don't scale**: Multiple containers don't share filesystem — use Redis for sessions
7. **Build base ≠ run base**: Build uses generic `php@8.4`, run uses `php-nginx@8.4` or `php-apache@8.4`
8. **`--ignore-platform-reqs` for Alpine**: Use this Composer flag to avoid musl/extension compatibility issues

## See Also
- zerops://services/_common-runtime
- zerops://services/nginx
- zerops://operations/production-checklist
- zerops://operations/init-commands
- zerops://examples/zerops-yml-runtimes
