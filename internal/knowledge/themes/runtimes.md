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
**Versions**: `php@8.5` (latest), `php@8.4`, `php@8.3`, `php@8.1`
**Build!=Run**: build `php@X`, run `php-nginx@X` or `php-apache@X`
**Port**: 80 fixed (exception to 80/443 rule)

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

## Node.js

**Base image includes**: Node.js, `npm`, `yarn`, `git`, `npx`

**Build procedure**:
1. Set `build.base: nodejs@22` (or desired version)
2. `buildCommands`: `npm ci` or `yarn install`, then framework build command
3. `deployFiles`: MUST include `node_modules` (runtime doesn't run npm install)
4. `run.start`: `node server.js` or framework start command

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

## Bun

**Base image includes**: Bun, `npm`, `yarn`, `git`, `npx`
**Versions**: `bun@latest` (= 1.2), `bun@1.1.34` (Ubuntu only), `bun@nightly`, `bun@canary`

**Build procedure**:
1. Set `build.base: bun@latest`
2. `buildCommands`: `bun i`, then `bun run build` or `bun build --outdir dist --target bun`
3. **Bundled deploy** (recommended): deploy `dist/` + `package.json` (NO node_modules)
4. **Source deploy**: deploy `src/` + `package.json` + `bun.lockb` + `node_modules`
5. `run.start`: `bun start`

**Binding**: `Bun.serve({hostname: "0.0.0.0"})` -- default localhost = 502
- Elysia: `hostname: "0.0.0.0"` in constructor
- Hono: `Bun.serve({fetch: app.fetch, hostname: "0.0.0.0"})`

**Key settings**: Use `bunx` instead of `npx`. Cache: `node_modules`

## Deno

**Base image includes**: Deno runtime
**OS**: `os: ubuntu` REQUIRED (not available on Alpine)

**Build procedure**:
1. Set `build.base: deno@latest`, `build.os: ubuntu`
2. Permissions: `--allow-net --allow-env` minimum
3. Use `deno.jsonc`, not `deno.json`

**Binding**: `Deno.serve({hostname: "0.0.0.0"}, handler)`
**Cache**: deps in `~/.cache/deno` (auto-cached)

## Python

**Base image includes**: Python, `pip`, `git`
**Versions**: `python@3.14` (latest), `python@3.12`, `python@3.11`

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

## Go

**Base image includes**: Go compiler, `git`, `wget`
**Version**: `go@1.22` (or `go@1`, `go@latest`)
**Build!=Run**: compiled binary -- deploy only binary, no `run.base` needed (omit it)

**Build procedure**:
1. Set `build.base: go@latest`
2. `buildCommands`: ALWAYS use `go mod tidy` before build:
   `go mod tidy && go build -o app .`
3. `deployFiles: app` (single binary)
4. `run.start: ./app`

**Binding**: default `:port` binds all interfaces (correct, no change needed)

**NEVER set `run.base: alpine@*`** -- causes glibc/musl mismatch for CGO-linked binaries -> 502. Omit `run.base` or use `run.base: go@latest`.
**CGO**: requires `os: ubuntu` + `CGO_ENABLED=1`. When unsure: `CGO_ENABLED=0 go build` for static binary
**Logger**: MUST output to `os.Stdout`
**Cache**: `~/go` (auto-cached)

## Java

**Base image includes**: JDK, `git`, `wget` -- **NO Maven, NO Gradle pre-installed**
**Versions**: `java@21` (recommended), `java@17`. NOTE: `java@latest` = `java@17`, use `java@21` explicitly

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

## Rust

**Base image includes**: `cargo` (via Rust base), `git`

**Build procedure**:
1. Set `build.base: rust@latest` (or `rust@1`, `rust@nightly`)
2. `buildCommands: [cargo b --release]` -- ALWAYS `--release` (debug 10-100x slower)
3. `deployFiles: target/release/~app` (tilde extracts binary to `/var/www/`)
4. `run.start: ./app`

**Binding**: most frameworks (actix-web, axum, warp) default to `0.0.0.0` -- verify custom bindings
**Native deps**: `apk add --no-cache openssl-dev pkgconfig` in prepareCommands
**Cache**: `target/`, `~/.cargo/registry`

## .NET

**Base image includes**: .NET SDK, ASP.NET, `git`
**Versions**: `dotnet@6`, `dotnet@7`, `dotnet@8`, `dotnet@9`

**Build procedure**:
1. Set `build.base: dotnet@9` (or desired version)
2. `buildCommands: [dotnet publish -c Release -o app]` -- `publish` preferred over `build`
3. `deployFiles: [app/~]` -> files at `/var/www/` -> `run.start: dotnet MyApp.dll`

**Binding (CRITICAL)**: Kestrel defaults to localhost -> 502. MUST bind in code:
```csharp
app.Urls.Add("http://0.0.0.0:5000");
```
Do NOT rely solely on `ASPNETCORE_URLS` env var.
**Cache**: `~/.nuget`

## Elixir

**Base image includes**: `mix`, `hex`, `rebar`, `npm`, `yarn`, `git`, `npx`
**Build=Run**: both use `elixir@latest`

**Build procedure**:
1. Set `build.base: elixir@latest`
2. `buildCommands: [mix deps.get --only prod, mix compile, mix release]`
3. `deployFiles: _build/prod/rel/app/~` (tilde extracts release)
4. `run.start: bin/app start`

**Required env**: `PHX_SERVER=true` + `MIX_ENV=prod`
**Cache**: `deps`, `_build`

## Gleam

**OS**: `os: ubuntu` REQUIRED (not available on Alpine)
- Erlang target: `gleam export erlang-shipment`, deploy `build/erlang-shipment/~`
- JavaScript target: needs Node.js runtime

## Ruby

**Base image includes**: Ruby, `bundler`, `gem`, `git`
**Versions**: `ruby@3.4` (latest), `ruby@3.3`, `ruby@3.2`

**Build procedure**:
1. Set `build.base: ruby@3.4`
2. `buildCommands`: `bundle install --deployment` + asset precompilation if needed
3. `deployFiles: ./` (entire source + vendor/bundle)
4. `run.start: bundle exec puma -b tcp://0.0.0.0:3000`

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

**Versions**: `alpine@3.23` (latest), `alpine@3.20`, `alpine@3.19`, `alpine@3.18`, `alpine@3.17`
- Default base (~5MB), `apk add --no-cache`
- musl libc -- some C libraries won't compile

## Ubuntu

- Full glibc (~100MB), `apt-get update && apt-get install -y`
- Use for: CGO Go, Python C extensions, legacy glibc-dependent software
