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
- Build tools pre-installed: `composer`, `git`, `wget`
- Composer: use `--ignore-platform-reqs` on Alpine
- Cache: `vendor`

## Node.js

- **BIND**: Express `app.listen(port, "0.0.0.0")`, Next.js `next start -H 0.0.0.0`, Fastify `host: "0.0.0.0"`, NestJS `app.listen(port, "0.0.0.0")`, Hono `serve({hostname: "0.0.0.0"})`
- **DEPLOY**: MUST include `node_modules` — runtime doesn't run `npm install`
- Next.js SSR: deploy `.next`, `node_modules`, `package.json`, `next.config.js`, `public`
- Nuxt SSR: deploy `.output`, `node_modules`, `package.json`
- Cache: `node_modules`, `.next/cache`, `.pnpm-store`

## Bun

- **BIND**: `Bun.serve({hostname: "0.0.0.0"})` — default localhost = 502
- Build tools pre-installed: Bun, `npm`, `yarn`, `git`, `npx`
- Build commands: `bun i`, `bun run build`
- **DEPLOY BUNDLED** (recommended): `bun build --outdir dist --target bun`, deploy `dist/` + `package.json` (NO node_modules)
- **DEPLOY SOURCE**: deploy `src/` + `package.json` + `bun.lockb` + `node_modules`
- Start: `bun start`
- Elysia: `hostname: "0.0.0.0"` in constructor, Hono: `Bun.serve({fetch: app.fetch, hostname: "0.0.0.0"})`
- Use `bunx` instead of `npx`
- Cache: `node_modules`

## Deno

- **BIND**: `Deno.serve({hostname: "0.0.0.0"}, handler)`
- **PERMISSIONS**: `--allow-net --allow-env` minimum
- Use `deno.jsonc`, not `deno.json`
- Cache: deps in `~/.cache/deno` (auto-cached)

## Python

- **BIND**: uvicorn `--host 0.0.0.0`, gunicorn `--bind 0.0.0.0:8000`
- **INSTALL PATTERN** (canonical):
  1. `build.addToRunPrepare: [requirements.txt]` — copies to `/home/zerops/` in prepare container
  2. `run.prepareCommands: [python3 -m pip install --ignore-installed -r /home/zerops/requirements.txt]`
  3. `build.buildCommands`: NO pip install needed (build container is separate)
  4. `build.deployFiles: [app.py, ...]` or `[.]` for source files only
  5. `run.start: gunicorn app:app --bind 0.0.0.0:8000` (gunicorn installed by prepareCommands)
- **CRITICAL**: `run.prepareCommands` runs BEFORE deploy files arrive at `/var/www` but AFTER `addToRunPrepare` files are at `/home/zerops/`. Always reference `/home/zerops/requirements.txt`, NOT `/var/www/requirements.txt`
- Build tools pre-installed: `pip`, `git`
- System deps: `run.prepareCommands` with `apk add` (before pip install)
- Use `--ignore-installed` for pip in `run.prepareCommands`

## Go

- **BIND**: default `:port` binds all interfaces (correct, no change needed)
- **BUILD≠RUN**: compiled binary — deploy only binary, no `run.base` needed (omit it)
- **NEVER set `run.base: alpine@*`** — use no `run.base` or `run.base: go@latest`. Alpine run base causes glibc/musl mismatch for CGO-linked binaries (502 Bad Gateway)
- Build tools pre-installed: Go compiler, `git`, `wget`
- **Build commands** (in order): `go build -o app main.go` — do NOT create `go.sum` manually, the build container runs `go mod download` automatically if `go.sum` is present and valid
- **NEVER write go.sum by hand** — checksums will be wrong. Either include a valid `go.sum` from local dev or omit it and add `go mod tidy` as first buildCommand
- Deploy: `app` (single binary)
- Start: `./app`
- **CGO**: requires `os: ubuntu` + `CGO_ENABLED=1`, pure Go uses Alpine. When unsure, use `CGO_ENABLED=0 go build` for static binary
- Logger MUST output to `os.Stdout`
- Cache: `~/go` (auto-cached)

## Java

- **BIND**: `server.address=0.0.0.0` (Spring Boot defaults to localhost!)
- **BUILD TOOLS NOT PRE-INSTALLED**: `java@21` provides only JDK, `git`, `wget` — NO Maven, NO Gradle
  - **With Maven Wrapper** (recommended): `./mvnw clean install` in `buildCommands`. Include `mvnw`, `.mvn/` in your source
  - **Without wrapper** (plain projects): set `os: ubuntu` and add `prepareCommands: ["sudo apt-get update && sudo apt-get install -y maven"]`, then use `mvn` in `buildCommands`. Alpine default does NOT have apt-get — `apk add maven` also unavailable
- **FAT JAR REQUIRED**: deploy a single fat/uber JAR with all dependencies embedded. Use `maven-shade-plugin`, `spring-boot-maven-plugin`, or `maven-assembly-plugin`. Do NOT deploy individual jar + lib/ separately
- **DEPLOY**: `deployFiles: target/app.jar` (relative path, single fat JAR)
- **START**: `java -jar target/app.jar` (relative to `/var/www`)
- **RAM**: `-Xmx` = ~75% of container max RAM
- Cache: `.m2` or `.gradle`

## Rust

- **BIND**: most frameworks (actix-web, axum, warp) default to `0.0.0.0` — verify if using custom binding
- Build tools pre-installed: `cargo` (via Rust base), `npm`, `yarn`, `git`, `npx`
- Build command: `cargo b --release` (always `--release` — debug 10-100x slower)
- Cache: `target/`, `~/.cargo/registry`
- Deploy: `target/release/~app` (tilde extracts binary to `/var/www/`)
- Start: `./app` (binary lands in `/var/www/`)
- Use `rust@latest` (or `rust@stable`, `rust@nightly`)
- Native deps (openssl, etc.): `apk add --no-cache openssl-dev pkgconfig` in prepareCommands

## .NET

- **BIND**: `ASPNETCORE_URLS=http://0.0.0.0:5000`
- Build tools pre-installed: .NET SDK, ASP.NET, `git`
- Build command: `dotnet build -o app` (or `dotnet publish -c Release -o app`)
- Deploy: `app` (the output folder)
- Start: `cd app && dotnet dnet.dll` (adjust DLL name to match your project)
- Alpine: use `linux-musl-x64` runtime identifier
- Cache: NuGet packages

## Elixir

- **BUILD=RUN**: build `elixir@latest`, run `elixir@latest` (both use Elixir base)
- Build tools pre-installed: `mix`, `hex`, `rebar`, `npm`, `yarn`, `git`, `npx`
- Build commands: `mix deps.get --only prod`, `mix compile`, `mix release`
- Deploy: `_build/prod/rel/app/~` (tilde extracts release contents to `/var/www/`)
- Start: `bin/app start` (release binary in `/var/www/`)
- `PHX_SERVER=true` + `MIX_ENV=prod` required
- Cache: `deps`, `_build`

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
- zerops://guides/deployment-lifecycle — build/deploy pipeline details
- zerops://guides/build-cache — cache architecture and per-runtime recommendations
