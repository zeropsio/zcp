# PHP on Zerops

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

- `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` -- REQUIRED or CSRF breaks
- Alpine extensions: `sudo apk add --no-cache php84-<ext>` (version prefix = PHP major+minor, `sudo` required)
- Cache: `vendor`
- Document root: `documentRoot: public` (Laravel, Symfony) or `documentRoot: www/` (Nette). Default nginx handles PHP routing. Custom config via `siteConfigPath: site.conf.tmpl` only if non-standard rules needed -- use `fastcgi_pass unix:{{.PhpSocket}};` (MUST include `unix:` prefix)

### Common Mistakes

- Missing `documentRoot` -> Nginx doesn't know where to serve from
- Missing `TRUSTED_PROXIES` -> CSRF validation fails behind L7 LB
- Using `php-nginx` as build base -> build needs `php@X`, not the webserver variant
- Apache: `.htaccess` MUST be included in `deployFiles` (Nginx doesn't need it)
- `apk add` without `sudo` -> "Permission denied" in prepareCommands

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, no build step needed (PHP is interpreted)
**Prod deploy**: `buildCommands: [composer install --ignore-platform-reqs]`, `deployFiles: [., vendor/]`
