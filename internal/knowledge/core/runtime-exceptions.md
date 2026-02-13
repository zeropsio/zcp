# Runtime Exceptions

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

## PHP

- Build base: `php@X` (generic), run base: `php-nginx@X` or `php-apache@X` (different!)
- Port 80 (not configurable, exception to 80/443 rule)
- `documentRoot` required (Laravel: `public`, WordPress: `""`)
- Trusted proxies: `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` or CSRF breaks
- Multi-base builds: Laravel Jetstream uses `base: [php@8.4, nodejs@18]`
- Alpine extensions: `apk add php-<ext>`, NOT `docker-php-ext-install`
- Composer: Use `--ignore-platform-reqs` on Alpine

## Node.js

- `deployFiles` MUST include `node_modules` (runtime doesn't run `npm install`)
- Next.js: deploy `.next`, `node_modules`, `package.json`, `next.config.js`, `public`
- Nuxt SSR: deploy `.output`, `node_modules`, `package.json`
- Cache: `node_modules`, `.next/cache`, `.pnpm-store`

## Python

- `build.addToRunPrepare` copies files from build to run (listed under `build:`, not `run:`)
- Must bind to `0.0.0.0:PORT` (localhost won't work)
- FastAPI: `uvicorn main:app --host 0.0.0.0 --port 8000`
- Django: `gunicorn myproject.wsgi:application --bind 0.0.0.0:8000`
- Runtime system deps: use `run.prepareCommands` with `apk add`

## Go

Follows default pattern (compiled binary, no special requirements). CGO requires Ubuntu (`os: ubuntu`).

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

## Bun

- Don't deploy `node_modules` (unlike Node.js, deploy only `package.json` + `dist`)
- `bun.lockb` is binary format (not editable)
- Use `bunx` instead of `npx`

## Deno

- Use `deno@1` (recipes use 1.x)
- Permissions mandatory: `--allow-net` or app can't open ports
- Deploy: `dist` + `deno.jsonc` (not entire dir)
- Use `deno.jsonc`, not `deno.json`

## Static

- No port config needed (serves on port 80 internally)
- Deploy: `dist/~` (tilde deploys contents to root)
- SPA fallback automatic
- SSG pattern: build on `nodejs@22`, run on `static`
- Next.js: `output: 'export'` in `next.config.mjs`
- SvelteKit: `@sveltejs/adapter-static` + `export const prerender = true`
- Nuxt: use `nuxi generate` (not `nuxi build`)
