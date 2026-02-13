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

- **Bind to `0.0.0.0`**: Express/Fastify: `app.listen(port, "0.0.0.0")`. Next.js: `node_modules/.bin/next start -H 0.0.0.0`
- `deployFiles` MUST include `node_modules` (runtime doesn't run `npm install`)
- Next.js: deploy `.next`, `node_modules`, `package.json`, `next.config.js`, `public`
- Nuxt SSR: deploy `.output`, `node_modules`, `package.json`
- Cache: `node_modules`, `.next/cache`, `.pnpm-store`
- Fastify: use `host: "0.0.0.0"` in listen options
- NestJS: `await app.listen(port, "0.0.0.0")`
- Hono (Node.js adapter): `serve({ fetch: app.fetch, hostname: "0.0.0.0", port })`

## Python

- `build.addToRunPrepare` copies files from build to run (listed under `build:`, not `run:`)
- Must bind to `0.0.0.0:PORT` (localhost won't work)
- FastAPI: `uvicorn main:app --host 0.0.0.0 --port 8000`
- Django: `gunicorn myproject.wsgi:application --bind 0.0.0.0:8000`
- Runtime system deps: use `run.prepareCommands` with `apk add`

## Go

- Default `:port` binding is correct (binds all interfaces)
- Compiled binary — deploy only the binary, not source
- CGO requires Ubuntu (`os: ubuntu`), pure Go uses Alpine (default)
- Minimal zerops.yml pattern:
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
- Logger MUST output to `os.Stdout` for Zerops log collection
- No `node_modules` equivalent — single binary deploy
- Cache: Go module cache is in `~/go` (auto-cached, no config needed)

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

- **Bind to `0.0.0.0`**: `Bun.serve({ hostname: "0.0.0.0", port })` — mandatory, default `localhost` = 502 Bad Gateway
- Two deploy patterns:
  - **Bundled** (recommended): `bun build src/index.ts --outdir dist --target bun`, deploy `dist/` + `package.json`
  - **Source**: deploy `src/` + `package.json` + `bun.lockb` + `node_modules`
- `deployFiles` must include `bun.lockb` + `package.json` for source deploys
- **Don't deploy `node_modules` for bundled output** — unlike Node.js, Bun bundles dependencies
- For source deploys: include `node_modules` (runtime doesn't run `bun install`)
- Cache: `node_modules` (speeds up subsequent builds)
- `bun.lockb` is binary format (not editable)
- Use `bunx` instead of `npx`
- Hono framework: `Bun.serve({ fetch: app.fetch, hostname: "0.0.0.0", port })`
- Elysia framework: set `hostname: "0.0.0.0"` in Elysia constructor or `.listen()` options
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
- Deploy: compiled output or source + `deno.jsonc`
- Use `deno.jsonc`, not `deno.json`
- Fresh/Hono: `hostname: "0.0.0.0"` in serve options
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
- Alternative (source deploy): deploy `src/` + `deno.jsonc`, start with `deno run --allow-net --allow-env src/main.ts`
- Cache: Deno caches deps globally in `~/.cache/deno` (auto-cached)

## Static

- No port config needed (serves on port 80 internally)
- Deploy: `dist/~` (tilde deploys contents to root)
- SPA fallback automatic
- SSG pattern: build on `nodejs@22`, run on `static`
- Next.js: `output: 'export'` in `next.config.mjs`
- SvelteKit: `@sveltejs/adapter-static` + `export const prerender = true`
- Nuxt: use `nuxi generate` (not `nuxi build`)
