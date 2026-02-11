# Deploy Patterns on Zerops

## Keywords
deploy, deployFiles, tilde, dist, build, run, base, multi-base, static, ssr, ssg, binary, deploy pattern, build base, run base, addToRunPrepare

## TL;DR
Three deploy patterns: (A) single-base (build=run), (B) multi-base (build≠run, e.g. Node→Static), (C) multi-runtime (e.g. Elixir→Alpine). The `~` tilde syntax deploys directory **contents** without the directory itself.

## Tilde Syntax (`~`)

The `~` suffix on a deploy path extracts the directory contents into the service root instead of deploying the directory itself.

```yaml
# WITHOUT tilde: creates /var/www/dist/index.html
deployFiles:
  - dist

# WITH tilde: creates /var/www/index.html
deployFiles:
  - dist/~
```

**When to use tilde:**
- Static/SSG sites where the run service expects files in document root
- Compiled binaries in deep paths (e.g., `./target/release/~/myapp`)
- Any case where you need contents flattened into `/var/www`

**Common tilde paths per framework:**

| Framework | Output dir | deployFiles |
|-----------|-----------|-------------|
| React/Vue/Solid/Angular (Vite) | `dist/` | `dist/~` |
| Next.js (static export) | `out/` | `out/~` |
| Nuxt (generate) | `.output/public/` | `.output/public/~` |
| SvelteKit (static) | `build/` | `build/~` |
| Astro (static) | `dist/` | `dist/~` |
| Remix (static) | `build/client/~` | `build/client/~` |
| Elixir release | `_build/prod/rel/app/` | `_build/prod/rel/app/~` |
| Rust binary | `./target/release/` | `./target/release/~/myapp` |
| .NET build | `app/` | `app/~` |
| Gleam shipment | `build/erlang-shipment/` | `build/erlang-shipment/~` |

## Deploy Pattern A: Single-Base

Build and run use the same runtime. Typical for SSR apps and interpreted languages.

```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22        # same runtime
      buildCommands:
        - pnpm i && pnpm build
      deployFiles:
        - .next
        - node_modules
        - package.json
    run:
      start: pnpm start
      ports:
        - port: 3000
          httpSupport: true
```

**Use cases:** Node.js SSR (Next.js, Nuxt, Remix), PHP, Python, Java

## Deploy Pattern B: Multi-Base (Build→Static)

Build on a full runtime, run on lightweight static service. For SSG/SPA sites.

```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22        # full runtime for build
      buildCommands:
        - pnpm i && pnpm build
      deployFiles:
        - dist/~             # tilde: contents only
    run:
      base: static           # lightweight nginx
```

**Use cases:** React SPA, Vue SPA, Astro static, Next.js export, Nuxt generate, SvelteKit static

## Deploy Pattern C: Multi-Runtime (Compiled→Alpine)

Build on language runtime, run on minimal Alpine. For compiled languages producing self-contained binaries.

```yaml
zerops:
  - setup: app
    build:
      base: elixir@1.16      # compile environment
      buildCommands:
        - mix local.hex --force && mix local.rebar --force
        - mix deps.get --only prod
        - MIX_ENV=prod mix compile
        - MIX_ENV=prod mix release
      deployFiles:
        - _build/prod/rel/app/~
    run:
      base: alpine@latest     # minimal runtime
      start: bin/app start
```

**Use cases:** Elixir/Phoenix releases, Rust binaries, Go binaries

## `addToRunPrepare` Pattern

`addToRunPrepare` is listed under `build:` and copies files/directories from the build container into the run container's base image. Used for Python pip packages:

```yaml
zerops:
  - setup: app
    build:
      base: python@3.12
      addToRunPrepare:
        - .                          # copies installed packages to run container
      buildCommands:
        - pip install -r requirements.txt
      deployFiles: ./
    run:
      start: gunicorn app:app --bind 0.0.0.0:8000
      ports:
        - port: 8000
          httpSupport: true
```

## Deploy Files by App Type

| App type | deployFiles | Notes |
|----------|-------------|-------|
| Node.js SSR | `.next`, `node_modules`, `package.json` | Must include node_modules |
| PHP | `./` | Entire project |
| Python | `./` | Source + addToRunPrepare |
| Go | `./app` | Single binary |
| Rust | `./target/release/~/myapp` | Binary via tilde |
| Java Spring | `./target/api.jar` | Single JAR |
| .NET | `app/~` | Build output via tilde |
| Elixir release | `_build/prod/rel/app/~` | Release via tilde |
| Gleam shipment | `build/erlang-shipment/~` | Shipment via tilde |
| Bun | `package.json`, `dist` | No node_modules |
| Deno | `dist`, `deno.jsonc` | Specific output files |
| Static/SPA | `dist/~` | Contents only via tilde |

## Gotchas
1. **Missing tilde = nested directory**: `dist` deploys as `/var/www/dist/`, `dist/~` deploys contents to `/var/www/`
2. **Node.js SSR needs node_modules**: Runtime has no package manager — deploy node_modules alongside
3. **PHP deploys everything**: Use `./` — framework needs full source tree
4. **Multi-base run needs matching start command**: Binary path changes when deploying with tilde to alpine
5. **`addToRunPrepare` is under `build:`**: It copies files from build to run — it's not a runtime command

## See Also
- zerops://config/zerops-yml
- zerops://examples/zerops-yml-runtimes
- zerops://services/static
- zerops://services/_common-runtime
