# Runtime Deltas

## Keywords
runtime, php, nodejs, node, bun, deno, python, go, java, rust, dotnet, elixir, gleam, static, docker, nginx, alpine, ubuntu, deploy, binding, 0.0.0.0, zerops.yml

## TL;DR
Runtime-specific deltas from universal grammar. Each section lists ONLY what differs. If not listed, grammar defaults apply: build.base = run.base, os = alpine, bind 0.0.0.0, deployFiles mandatory.

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
| `static` | Static |
| `docker@*` | Docker |
| `nginx@*` | Nginx |
| `alpine@*` | Alpine |
| `ubuntu@*` | Ubuntu |

## PHP

- **BUILD≠RUN**: build `php@X`, run `php-nginx@X` or `php-apache@X`
- **PORT**: 80 fixed (exception to 80/443 rule)
- **documentRoot**: required — Laravel: `public`, WordPress: `""`
- **TRUSTED_PROXIES**: `"127.0.0.1,10.0.0.0/8"` or CSRF breaks
- **Multi-base**: `base: [php@8.4, nodejs@18]` for Vite/Inertia assets
- Alpine extensions: `apk add php84-<ext>` (version prefix matches PHP major+minor, e.g. `php84-redis`, `php84-pdo_pgsql`, `php83-curl`)
- Composer: use `--ignore-platform-reqs` on Alpine

## Node.js

- **BIND**: Express `app.listen(port, "0.0.0.0")`, Next.js `next start -H 0.0.0.0`, Fastify `host: "0.0.0.0"`, NestJS `app.listen(port, "0.0.0.0")`, Hono `serve({hostname: "0.0.0.0"})`
- **DEPLOY**: MUST include `node_modules` — runtime doesn't run `npm install`
- Next.js SSR: deploy `.next`, `node_modules`, `package.json`, `next.config.js`, `public`
- Nuxt SSR: deploy `.output`, `node_modules`, `package.json`
- Cache: `node_modules`, `.next/cache`, `.pnpm-store`

## Bun

- **BIND**: `Bun.serve({hostname: "0.0.0.0"})` — default localhost = 502
- **DEPLOY BUNDLED** (recommended): `bun build --outdir dist --target bun`, deploy `dist/` + `package.json` (NO node_modules)
- **DEPLOY SOURCE**: deploy `src/` + `package.json` + `bun.lockb` + `node_modules`
- Elysia: `hostname: "0.0.0.0"` in constructor, Hono: `Bun.serve({fetch: app.fetch, hostname: "0.0.0.0"})`
- Use `bunx` instead of `npx`

## Deno

- **BIND**: `Deno.serve({hostname: "0.0.0.0"}, handler)`
- **PERMISSIONS**: `--allow-net --allow-env` minimum
- Use `deno.jsonc`, not `deno.json`
- Cache: deps in `~/.cache/deno` (auto-cached)

## Python

- **BIND**: uvicorn `--host 0.0.0.0`, gunicorn `--bind 0.0.0.0:8000`
- **INSTALL**: `build.addToRunPrepare` copies pip packages to run container
- System deps: `run.prepareCommands` with `apk add`
- Use `--no-cache-dir` for pip

## Go

- **BIND**: default `:port` binds all interfaces (correct, no change needed)
- **BUILD≠RUN**: compiled binary — deploy only binary, no run base needed
- **CGO**: requires `os: ubuntu` + `CGO_ENABLED=1`, pure Go uses Alpine
- Logger MUST output to `os.Stdout`
- Cache: `~/go` (auto-cached)

## Java

- **BIND**: `server.address=0.0.0.0` (Spring Boot defaults to localhost!)
- **RAM**: `-Xmx` = ~75% of container max RAM
- Cache: `.m2` or `.gradle`
- Deploy only JAR, not entire `target/`

## Rust

- **BIND**: most frameworks (actix-web, axum, warp) default to `0.0.0.0` — verify if using custom binding
- Always `--release` (debug 10-100x slower)
- Cache: `target/`, `~/.cargo/registry`
- Deploy: `target/release/~myapp` (tilde extracts binary to `/var/www/`)
- Start: `./myapp` (binary lands in `/var/www/`)
- Use `rust@stable` (or `rust@nightly`)
- Native deps (openssl, etc.): `apk add --no-cache openssl-dev pkgconfig` in prepareCommands

## .NET

- **BIND**: `ASPNETCORE_URLS=http://0.0.0.0:5000`
- Deploy: `app/~`, start: `dotnet dotnet.dll`
- Alpine: use `linux-musl-x64` runtime identifier

## Elixir

- **BUILD≠RUN**: build `elixir@1.16`, run `alpine@latest`
- Deploy: `_build/prod/rel/app/~`
- `PHX_SERVER=true` + `MIX_ENV=prod` required

## Gleam

- Erlang target: `gleam export erlang-shipment`, deploy `build/erlang-shipment/~`
- JavaScript target: needs Node.js runtime

## Static

- **BUILD≠RUN**: build `nodejs@22`, run `static`
- No port config (serves on 80 internally)
- Deploy: `dist/~` (tilde mandatory for correct root)
- SPA fallback automatic ($uri → $uri.html → $uri/index.html → /index.html → 404)
- Outputs: React/Vue `dist/~`, Angular `dist/app/browser/~`, Next.js export `out/~`, Nuxt generate `.output/public/~`, SvelteKit `build/~`, Astro `dist/~`, Remix `build/client/~`

## Nginx

- SPA routing by default ($uri → $uri.html → $uri/index.html → /index.html → 404)
- Template: `{{.DocumentRoot}}` resolves to configured document root
- Prerender.io: `PRERENDER_TOKEN` env var

## Docker

- Runs in **VM** (not container) — slower boot, higher overhead
- **`--network=host`** MANDATORY (or `network_mode: host` in compose)
- Resources: fixed values only (no min-max autoscaling)
- Resource change triggers VM restart
- Never `:latest` tag — cached, won't re-pull

## Alpine

- Default base (~5MB), `apk add --no-cache`
- musl libc — some C libraries won't compile

## Ubuntu

- Full glibc (~100MB), `apt-get update && apt-get install -y`
- Use for: CGO Go, Python C extensions, legacy glibc-dependent software

## See Also
- zerops://foundation/grammar — universal rules
- zerops://foundation/services — managed services
