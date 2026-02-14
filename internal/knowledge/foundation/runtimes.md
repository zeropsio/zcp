# Runtime Exceptions

## Keywords
runtime, php, nodejs, node, bun, deno, python, go, java, rust, dotnet, elixir, gleam, static, docker, nginx, alpine, ubuntu, deploy, binding, zerops.yml

## TL;DR
Runtime-specific exceptions to Zerops core principles. Each section covers only what DIFFERS from the universal rules in core.md. If not listed here, the core rules apply unchanged.

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

- Build base: `php@X` (generic), run base: `php-nginx@X` or `php-apache@X` (different!)
- Port 80 (not configurable, exception to 80/443 rule)
- `documentRoot` required (Laravel: `public`, WordPress: `""`)
- Trusted proxies: `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` or CSRF breaks
- Multi-base builds: Laravel Jetstream uses `base: [php@8.4, nodejs@18]`
- Alpine extensions: `apk add php-<ext>`, NOT `docker-php-ext-install`
- Composer: use `--ignore-platform-reqs` on Alpine

## Node.js

- Bind to `0.0.0.0`: Express/Fastify `app.listen(port, "0.0.0.0")`, Next.js `node_modules/.bin/next start -H 0.0.0.0`
- `deployFiles` MUST include `node_modules` (runtime doesn't run `npm install`)
- Next.js SSR: deploy `.next`, `node_modules`, `package.json`, `next.config.js`, `public`
- Nuxt SSR: deploy `.output`, `node_modules`, `package.json`
- Cache: `node_modules`, `.next/cache`, `.pnpm-store`
- Fastify: use `host: "0.0.0.0"` in listen options
- NestJS: `await app.listen(port, "0.0.0.0")`
- Hono (Node.js adapter): `serve({ fetch: app.fetch, hostname: "0.0.0.0", port })`

## Bun

- **Bind to `0.0.0.0`**: `Bun.serve({ hostname: "0.0.0.0", port })` — mandatory, default `localhost` = 502 Bad Gateway
- Two deploy patterns:
  - **Bundled** (recommended): `bun build src/index.ts --outdir dist --target bun`, deploy `dist/` + `package.json`
  - **Source**: deploy `src/` + `package.json` + `bun.lockb` + `node_modules`
- Don't deploy `node_modules` for bundled output — unlike Node.js, Bun bundles dependencies
- For source deploys: include `node_modules` (runtime doesn't run `bun install`)
- Cache: `node_modules`
- Use `bunx` instead of `npx`
- Hono: `Bun.serve({ fetch: app.fetch, hostname: "0.0.0.0", port })`
- Elysia: set `hostname: "0.0.0.0"` in constructor or `.listen()` options
- Minimal zerops.yml (source deploy):
  ```yaml
  build:
    base: bun@1.2
    buildCommands:
      - bun install
    deployFiles:
      - src
      - package.json
      - bun.lockb
      - node_modules
    cache:
      - node_modules
  run:
    start: bun run src/index.ts
    ports:
      - port: 3000
        httpSupport: true
  ```

## Deno

- **Bind to `0.0.0.0`**: `Deno.serve({ hostname: "0.0.0.0", port }, handler)` — mandatory
- Permissions mandatory: `--allow-net --allow-env` minimum (or `--allow-all` for dev)
- Use `deno.jsonc`, not `deno.json`
- Fresh/Hono: `hostname: "0.0.0.0"` in serve options
- Cache: Deno caches deps globally in `~/.cache/deno` (auto-cached)
- Minimal zerops.yml:
  ```yaml
  build:
    base: deno@2
    buildCommands:
      - deno compile --allow-net --allow-env --output app src/main.ts
    deployFiles:
      - app
  run:
    start: /var/www/app
    ports:
      - port: 8000
        httpSupport: true
  ```

## Python

- `build.addToRunPrepare` copies pip packages from build to run container (listed under `build:`, not `run:`)
- Must bind to `0.0.0.0:PORT` (localhost won't work)
- FastAPI: `uvicorn main:app --host 0.0.0.0 --port 8000`
- Django: `gunicorn myproject.wsgi:application --bind 0.0.0.0:8000`
- Runtime system deps: use `run.prepareCommands` with `apk add`
- Use `--no-cache-dir` flag for pip in containers

## Go

- Default `:port` binding is correct (binds all interfaces)
- Compiled binary — deploy only the binary, not source
- CGO requires Ubuntu (`os: ubuntu`), pure Go uses Alpine (default)
- Logger MUST output to `os.Stdout` for Zerops log collection
- Cache: Go module cache in `~/go` (auto-cached, no config needed)
- Minimal zerops.yml:
  ```yaml
  build:
    base: go@latest
    buildCommands:
      - go build -v -o app .
    deployFiles:
      - app
  run:
    start: /var/www/app
    ports:
      - port: 8080
        httpSupport: true
  ```

## Java

- Set `-Xmx` to ~75% of container max RAM (e.g., 1GB → `-Xmx768m`)
- Spring Boot: `server.address=0.0.0.0` (defaults to localhost)
- Cache: `.m2` or `.gradle`
- Deploy only JAR, not entire `target/` or `build/`

## Rust

- Always use `--release` (debug builds are 10-100x slower)
- Cache: `target/`, `~/.cargo/registry`
- Deploy: `./target/release/~/myapp` (tilde extracts binary)
- Use `rust@stable` alias

## .NET

- `ASPNETCORE_URLS=http://0.0.0.0:5000` (Kestrel defaults to localhost)
- Deploy: `app/~` (tilde deploys contents to root)
- Start: `dotnet dotnet.dll`
- Alpine: use `linux-musl-x64` runtime identifier

## Elixir

- Build base: `elixir@1.16`, run base: `alpine@latest` (multi-base pattern)
- Deploy: `_build/prod/rel/app/~` (tilde extracts release)
- Start: `bin/app start`
- Phoenix: `PHX_SERVER=true` required
- `MIX_ENV=prod` in build and runtime

## Gleam

- Erlang target: `gleam export erlang-shipment`, deploy `build/erlang-shipment/~`
- JavaScript target: needs Node.js runtime

## Static

- No port config needed (serves on port 80 internally)
- Deploy: `dist/~` (tilde deploys contents to root)
- SPA fallback automatic (tries $uri → $uri.html → $uri/index.html → /index.html → 404)
- SSG pattern: build on `nodejs@22`, run on `static`
- Framework build outputs:
  - React/Vue/Solid (Vite): `dist/~`
  - Angular: `dist/app/browser/~`
  - Next.js (export): `out/~` — requires `output: 'export'` in `next.config.mjs`
  - Nuxt (generate): `.output/public/~` — use `nuxi generate` not `nuxi build`
  - SvelteKit: `build/~` — requires `@sveltejs/adapter-static` + `export const prerender = true`
  - Astro: `dist/~` (default is static)
  - Remix: `build/client/~`
- CDN: direct integration with Zerops CDN
- Wildcard domains not supported for static CDN

## Nginx

- Default routing: SPA-friendly ($uri → $uri.html → $uri/index.html → /index.html → 404)
- Template variable: `{{.DocumentRoot}}` resolves to configured document root path
- Custom `nginx.conf` override supported — use `{{.DocumentRoot}}` not hardcoded paths
- Prerender.io: built-in via `PRERENDER_TOKEN` env var
- CORS and custom headers configurable via `zerops.yml`

## Docker

- Runs in a **VM** (not container) — slower boot, higher resource overhead
- **Must use `--network=host`** (or `network_mode: host` in compose) — without it, container cannot receive traffic from Zerops routing
- Resources: fixed values only (no min-max auto-scaling ranges)
- Resource change triggers VM restart (brief downtime)
- Never use `:latest` tag — Zerops caches images, `:latest` won't be re-pulled
- Build phase runs in containers (fast), runtime is VM-based

## Alpine

- Default base for all Zerops runtimes (~5MB)
- Package manager: `apk add --no-cache`
- libc: musl (not glibc) — some C libraries won't compile
- Versions: 3.20, 3.19, 3.18, 3.17

## Ubuntu

- Full Debian-based environment (~100MB) with glibc
- Package manager: `apt-get update && apt-get install -y`
- Use when Alpine's musl causes compatibility issues
- When to use: CGO-enabled Go, Python C extensions failing on musl, legacy glibc-dependent software
- Versions: 24.04, 22.04
