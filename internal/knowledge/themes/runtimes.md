# Runtime Deltas

## TL;DR
Runtime-specific deltas from universal grammar. Each section lists ONLY what differs. If not listed, grammar defaults apply: build.base = run.base, os = alpine, bind 0.0.0.0, deployFiles mandatory.

## Keywords
runtime, php, nodejs, node, bun, deno, python, go, java, rust, dotnet, elixir, gleam, ruby, rails, static, docker, nginx, alpine, ubuntu, deploy, binding, 0.0.0.0, zerops.yml

## Runtime Name Normalization

| MCP Input | Section |
|-----------|---------|
| `php-nginx@*`, `php-apache@*`, `php@*` | PHP |
| `nodejs@*` | Node.js |
| `bun@*` | Bun |
| `deno@*` | Deno |
| `python@*` | Python |
| `go@*` | Go |
| `java@*` | Java |
| `rust@*` | Rust |
| `dotnet@*` | .NET |
| `elixir@*` | Elixir |
| `gleam@*` | Gleam |
| `ruby@*` | Ruby |
| `static` | Static |
| `docker@*` | Docker |
| `nginx@*` | Nginx |
| `alpine@*` | Alpine |
| `ubuntu@*` | Ubuntu |

## PHP

**Base image includes**: `composer`, `git`, `wget`, PHP runtime
**Build!=Run**: build `php@X`, run `php-nginx@X` or `php-apache@X`
**Port**: 80 fixed (exception to 80/443 rule)
**Pre-installed PHP extensions** (both php-nginx and php-apache images):
pdo, pdo_pgsql, pdo_mysql, pdo_sqlite, redis, imagick, mongodb, curl, dom, fileinfo, gd, gmp, iconv, intl, ldap, mbstring, opcache, openssl, session, simplexml, sockets, tidy, tokenizer, xml, xmlwriter, zip, soap, imap, igbinary, msgpack.
Use `apk add` only for extensions NOT in this list.

**Build procedure**:
1. Set `build.base: php@8.4` (or desired version)
2. If assets needed: `base: [php@8.4, nodejs@18]` (multi-base)
3. `buildCommands`: `composer install --ignore-platform-reqs` (Alpine musl compat)
4. `deployFiles`: include `vendor/`, app files
5. Set `run.base: php-nginx@8.4` (or `php-apache@8.4`)
6. Set `documentRoot` -- Laravel: `public`, WordPress: `""`, Nette: `www/`

**Key settings**:
- `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` -- REQUIRED or CSRF breaks
- Alpine extensions: `sudo apk add --no-cache php84-<ext>` (version prefix = PHP major+minor, `sudo` required)
- Cache: `vendor`
- Custom nginx: `siteConfigPath: site.conf.tmpl` -- use `{{.PhpSocket}}` for fastcgi_pass (NOT `127.0.0.1:9000`)

**Common mistakes**:
- Missing `documentRoot` -> Nginx doesn't know where to serve from
- Missing `TRUSTED_PROXIES` -> CSRF validation fails behind L7 LB
- Using `php-nginx` as build base -> build needs `php@X`, not the webserver variant
- `apk add` without `sudo` -> "Permission denied" in prepareCommands

**Dev deploy**: `deployFiles: [.]`, no build step needed (PHP is interpreted)
**Prod deploy**: `buildCommands: [composer install --ignore-platform-reqs]`, `deployFiles: [., vendor/]`

## Node.js

**Base image includes**: Node.js, `npm`, `yarn`, `pnpm`, `git`, `npx`

**Build procedure**:
1. Set `build.base: nodejs@22` (or desired version)
2. `buildCommands` — use ONE package manager:
   - `pnpm i` (preferred — fastest, smallest disk, pre-installed)
   - `npm ci` (deterministic — REQUIRES package-lock.json, fails without it)
   - `npm install` (flexible — works without lockfile, creates/updates package-lock.json)
   - `yarn install` (requires yarn.lock)
3. `deployFiles`: MUST include `node_modules` (runtime doesn't run package install)
4. `run.start`: `node server.js` or framework start command

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container — agent starts `node index.js` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [pnpm i, pnpm build]`, `deployFiles` includes `node_modules` + build output

**Binding per framework**:
- Express: `app.listen(port, "0.0.0.0")`
- Next.js: `next start -H 0.0.0.0`
- Fastify: `host: "0.0.0.0"`
- NestJS: `app.listen(port, "0.0.0.0")`
- Hono: `serve({hostname: "0.0.0.0"})`

**Deploy patterns**:
- Next.js SSR: deploy `.next`, `node_modules`, `package.json`, `next.config.js`, `public`
- Nuxt SSR: deploy `.output`, `node_modules`, `package.json`
- Cache: `node_modules`, `.next/cache`, `.pnpm-store`

**Common mistakes**:
- Missing `node_modules` in `deployFiles` -> "Cannot find module" at runtime
- Not binding `0.0.0.0` -> 502 Bad Gateway
- Next.js missing `output: 'export'` for static -> produces SSR output instead
- Using `npm ci` without `package-lock.json` -> EUSAGE error ("can only install with existing lockfile")

## Bun

**Base image includes**: Bun, `npm`, `yarn`, `git`, `npx`

**Build procedure**:
1. Set `build.base: bun@latest`
2. `buildCommands`: `bun i`, then `bun run build` or `bun build --outdir dist --target bun`
3. **Bundled deploy** (recommended): `deployFiles: [dist, package.json]` (NO node_modules) + `start: bun dist/index.js`
4. **Source deploy**: `deployFiles: [src, package.json, node_modules]` + `start: bun run src/index.ts`
5. **CRITICAL**: do NOT use `deployFiles: dist/~` with `start: bun dist/index.js` -- tilde strips the `dist/` prefix, so the file lands at `/var/www/index.js`, not `/var/www/dist/index.js`

**Binding**: `Bun.serve({hostname: "0.0.0.0"})` -- default localhost = 502
- Elysia: `hostname: "0.0.0.0"` in constructor
- Hono: `Bun.serve({fetch: app.fetch, hostname: "0.0.0.0"})`

**Key settings**: Use `bunx` instead of `npx`. Cache: `node_modules`

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container — agent starts `bun run index.ts` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [bun i, bun build --outdir dist --target bun src/index.ts]`, `deployFiles: [dist, package.json]`, `start: bun dist/index.js`

## Deno

**Base image includes**: Deno runtime
**OS**: `os: ubuntu` REQUIRED (not available on Alpine)

**Build procedure**:
1. Set `build.base: deno@latest`, `build.os: ubuntu`
2. Permissions: `--allow-net --allow-env` minimum
3. Use `deno.jsonc`, not `deno.json`

**Binding**: `Deno.serve({hostname: "0.0.0.0"}, handler)`
**Cache**: deps in `~/.cache/deno` (auto-cached)

**Build caching**: Run `deno cache main.ts` in buildCommands to pre-download dependencies. Ensures deployments are deterministic.

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container — agent starts `deno run --allow-net --allow-env main.ts` manually via SSH for iteration)
**Prod deploy**: `deployFiles: [.]`, `start: deno run --allow-net --allow-env main.ts`
For compiled binaries: `deno compile --output app main.ts` → `deployFiles: app`, `start: ./app`

## Python

**Base image includes**: Python, `pip`, `git`

**Build procedure** (canonical pattern):
1. Set `build.base: python@3.12` (or desired version)
2. `build.addToRunPrepare: [requirements.txt]` -- copies to `/home/zerops/`
3. `run.prepareCommands: [python3 -m pip install --ignore-installed -r /home/zerops/requirements.txt]`
4. `build.buildCommands`: NO pip install needed (build container is separate)
5. `build.deployFiles: [app.py, ...]` or `[.]` for all source files
6. `run.start: gunicorn app:app --bind 0.0.0.0:8000`

**CRITICAL**: `run.prepareCommands` runs BEFORE deploy files arrive at `/var/www` but AFTER `addToRunPrepare` files are at `/home/zerops/`. Always use `/home/zerops/requirements.txt`, NOT `/var/www/requirements.txt`.

**Binding**: uvicorn `--host 0.0.0.0`, gunicorn `--bind 0.0.0.0:8000`

**Common mistakes**:
- Referencing `/var/www/requirements.txt` in `run.prepareCommands` -> file not found
- Missing `--bind 0.0.0.0` -> 502 Bad Gateway
- Missing `CSRF_TRUSTED_ORIGINS` for Django -> CSRF validation fails behind proxy

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container — agent starts `python3 app.py` manually via SSH for iteration)
**Prod deploy**: use `addToRunPrepare` + `prepareCommands` pattern for pip install, `start: gunicorn app:app --bind 0.0.0.0:8000`

## Go

**Base image includes**: Go compiler, `git`, `wget`
**Build!=Run**: compiled binary -- deploy only binary, no `run.base` needed (omit it)

**Build procedure**:
1. Set `build.base: go@latest`
2. `buildCommands`: ALWAYS use `go mod tidy` before build:
   `go mod tidy && go build -o app .`
3. `deployFiles: app` (single binary)
4. `run.start: ./app`

**Binding**: default `:port` binds all interfaces (correct, no change needed)

**NEVER set `run.base: alpine@*`** -- causes glibc/musl mismatch for CGO-linked binaries -> 502. Omit `run.base` or use `run.base: go@latest`.
**NEVER write go.sum by hand** -- checksums will be wrong, build fails with `checksum mismatch`. Let `go mod tidy` in buildCommands generate it.
**Do NOT include go.sum in source** when creating new apps -- `go mod tidy` in buildCommands handles it.
**CGO**: requires `os: ubuntu` + `CGO_ENABLED=1`. When unsure: `CGO_ENABLED=0 go build` for static binary
**Logger**: MUST output to `os.Stdout`
**Cache**: `~/go` (auto-cached)

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container — agent starts `go run .` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [go mod tidy && go build -o app .]`, `deployFiles: app`, `start: ./app`

## Java

**Base image includes**: JDK, `git`, `wget` -- **NO Maven, NO Gradle pre-installed**
NOTE: `java@latest` resolves to an older version, not the newest — always specify the exact version explicitly

**Build procedure**:
1. Set `build.base: java@21`
2. **For new projects**: set `build.os: ubuntu`, `prepareCommands: [sudo apt-get update -qq && sudo apt-get install -y -qq maven]`, `buildCommands: [mvn -q clean package -DskipTests]`
3. **For existing projects with wrapper**: `buildCommands: [./mvnw clean package -DskipTests]`
4. `deployFiles: target/app.jar` (single fat JAR)
5. `run.start: java -jar target/app.jar`

**FAT JAR REQUIRED**: deploy a single fat/uber JAR. Use `maven-shade-plugin` or `spring-boot-maven-plugin`.
**Binding**: `server.address=0.0.0.0` -- Spring Boot defaults to localhost!
**RAM**: `-Xmx` = ~75% of container max RAM
**Cache**: `.m2` or `.gradle`

**Common mistakes**:
- Bare `mvn` or `maven` in buildCommands -> "command not found" (not pre-installed)
- `apt-get` without `sudo` -> permission denied
- `apt-get` on default Alpine OS -> "command not found" (need `build.os: ubuntu`)
- Deploying thin JAR -> ClassNotFoundException at runtime
- Missing `server.address=0.0.0.0` for Spring Boot -> 502 Bad Gateway

**JAR naming**: Without `<finalName>` in pom.xml, JAR name includes version: `target/{artifactId}-{version}.jar`. If version changes, deployFiles path breaks. Normalize: add `<build><finalName>app</finalName></build>` to pom.xml, then use `deployFiles: target/app.jar`.

**Dev deploy**: `deployFiles: [.]`, install maven in prepareCommands, `start: zsc noop --silent` (idle container — agent starts `mvn -q compile exec:java` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [mvn -q clean package -DskipTests]`, `deployFiles: target/app.jar`, `start: java -jar target/app.jar`

## Rust

**Base image includes**: `cargo` (via Rust base), `git`

**Build procedure**:
1. Set `build.base: rust@latest` (or `rust@1`, `rust@nightly`)
2. `buildCommands: [cargo b --release]` -- ALWAYS `--release` (debug 10-100x slower)
3. `deployFiles: target/release/~app` (tilde extracts binary to `/var/www/`)
4. `run.start: ./app`

**Binding**: most frameworks (actix-web, axum, warp) default to `0.0.0.0` -- verify custom bindings
**Binary naming**: name in `Cargo.toml [package]` → binary at `target/release/{name}` (dashes preserved, e.g., `name = "my-app"` → `target/release/my-app`)

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container — agent starts `cargo run` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [cargo build --release]`, `deployFiles: target/release/~{binary}`, `start: ./{binary}`

**Native deps**: `apk add --no-cache openssl-dev pkgconfig` in prepareCommands
**Cache**: `target/`, `~/.cargo/registry`

## .NET

**Base image includes**: .NET SDK, ASP.NET, `git`

**Build procedure**:
1. Set `build.base: dotnet@9` (or desired version)
2. `buildCommands: [dotnet publish -c Release -o app]` -- `publish` preferred over `build`
3. `deployFiles: [app/~]` -> files at `/var/www/`
4. `run.start: dotnet {ProjectName}.dll` -- DLL name = .csproj FILENAME (NOT RootNamespace)
   Example: `myapp.csproj` → output is `myapp.dll` → `start: dotnet myapp.dll`

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container — agent starts `dotnet run` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [dotnet publish -c Release -o app]`, `deployFiles: [app/~]`, `start: dotnet {name}.dll`

**Binding (CRITICAL)**: Kestrel defaults to localhost -> 502. MUST bind in code:
```csharp
app.Urls.Add("http://0.0.0.0:5000");
```
Do NOT rely solely on `ASPNETCORE_URLS` env var.
**Cache**: `~/.nuget`

**Common mistakes**:
- Using RootNamespace as DLL name -> "file not found" (DLL name = .csproj filename, not namespace)
- Missing 0.0.0.0 binding in code -> 502 Bad Gateway
- Using `dotnet build` instead of `dotnet publish` -> missing runtime assets
- `ASPNETCORE_URLS` env var alone insufficient -> must set in code via UseUrls

## Elixir

**Base image includes**: `mix`, `hex`, `rebar`, `npm`, `yarn`, `git`, `npx`
**Build=Run**: both use `elixir@latest`

**Build procedure**:
1. Set `build.base: elixir@latest`
2. `buildCommands: [mix deps.get --only prod, mix compile, mix release]`
3. `deployFiles: _build/prod/rel/{app_name}/~` -- release name = mix.exs `app:` property (e.g. `:my_app` → `_build/prod/rel/my_app/~`)
4. `run.start: bin/{app_name} start` -- same name as mix.exs app

**Dev deploy**: `deployFiles: [.]`, `run.prepareCommands: [mix deps.get]`, `start: zsc noop --silent` (idle container — agent starts `mix run --no-halt` manually via SSH for iteration)
**Prod deploy**: build release, deploy extracted release, `start: bin/{app_name} start`

**Required env**: `PHX_SERVER=true` + `MIX_ENV=prod`
**Phoenix-specific**: Also set `PHX_HOST=${zeropsSubdomain}`
**Cache**: `deps`, `_build`

## Gleam

**OS**: `os: ubuntu` REQUIRED in both build AND run (not available on Alpine)

**Build procedure**:
1. Set `build.base: gleam@latest`, `build.os: ubuntu`
2. `buildCommands: [gleam export erlang-shipment]`
3. `deployFiles: build/erlang-shipment/~` (tilde extracts release to /var/www/)
4. `run.start: ./entrypoint.sh run` -- Erlang shipment includes entrypoint.sh
5. Set `run.os: ubuntu`

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container — agent starts `gleam run` manually via SSH for iteration)
**Prod deploy**: build erlang-shipment, deploy extracted, `start: ./entrypoint.sh run`

**VERSION WARNING**: `gleam@1.5` on Zerops is old. Modern `gleam_stdlib` versions require Gleam >=1.14.0. If dependencies fail with version mismatch, pin older dependency versions in gleam.toml.
- JavaScript target: needs Node.js runtime instead

## Ruby

**Base image includes**: Ruby, `bundler`, `gem`, `git`

**Build procedure**:
1. Set `build.base: ruby@3.4`
2. `buildCommands`:
   - With Gemfile.lock: `bundle install --deployment` (deterministic, production-ready)
   - Without Gemfile.lock: `bundle install --path vendor/bundle` (--deployment FAILS without lockfile)
3. `deployFiles: ./` (entire source + vendor/bundle)
4. `run.start: bundle exec puma -b tcp://0.0.0.0:3000`

**Dev deploy**: `deployFiles: [.]`, `run.prepareCommands: [bundle install --path vendor/bundle]`, `start: zsc noop --silent` (idle container — agent starts `bundle exec ruby app.rb` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [bundle install --deployment]`, `deployFiles: [.]`, `start: bundle exec puma -b tcp://0.0.0.0:3000`

**Cache**: `vendor/bundle`

**Rails specifics**:
- `RAILS_ENV: production`, `SECRET_KEY_BASE` via preprocessor
- Migrations: `zsc execOnce migrate-${ZEROPS_appVersionId} -- bin/rails db:migrate`
- Assets: `bundle exec rake assets:precompile` in buildCommands

## Static

**Build!=Run**: build `nodejs@22`, run `static`

**Build procedure**:
1. Set `build.base: nodejs@22`
2. `buildCommands`: framework build command
3. `deployFiles: dist/~` (tilde MANDATORY for correct root)
4. No `run.start` needed, no port config (serves on 80 internally)

**SPA fallback**: automatic ($uri -> $uri.html -> $uri/index.html -> /index.html -> 404)

**Framework outputs**:
- React/Vue: `dist/~`
- Angular: `dist/app/browser/~`
- Next.js export: `out/~`
- Nuxt generate: `.output/public/~`
- SvelteKit: `build/~`
- Astro: `dist/~`
- Remix: `build/client/~`

## Nginx

- SPA routing by default ($uri -> $uri.html -> $uri/index.html -> /index.html -> 404)
- Template: `{{.DocumentRoot}}` resolves to configured document root
- Prerender.io: `PRERENDER_TOKEN` env var

## Docker

- Runs in **VM** (not container) -- slower boot, higher overhead
- **`--network=host`** MANDATORY (or `network_mode: host` in compose)
- Resources: fixed values only (no min-max autoscaling)
- Resource change triggers VM restart
- Never `:latest` tag -- cached, won't re-pull

## Alpine

- Default base (~5MB), `apk add --no-cache`
- musl libc -- some C libraries won't compile

## Ubuntu

- Full glibc (~100MB), `apt-get update && apt-get install -y`
- Use for: CGO Go, Python C extensions, legacy glibc-dependent software
