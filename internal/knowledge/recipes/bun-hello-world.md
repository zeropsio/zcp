---
description: "A minimal Bun application with a PostgreSQL connection, demonstrating idempotent database migrations and a health check endpoint at `/`. Used within Bun Hello World recipe for Zerops platform."
---

# Bun Hello World on Zerops

## Base Image

Includes: Bun, `npm`, `yarn`, `git`, `bunx`.
NOT included: `pnpm`.

## Binding

Zerops L7 balancer routes to the container's VXLAN IP — apps that bind `localhost` get 502 with no useful error.

`Bun.serve({hostname: "0.0.0.0"})` — default is localhost = 502
- Elysia: `hostname: "0.0.0.0"` in constructor
- Hono: `Bun.serve({fetch: app.fetch, hostname: "0.0.0.0"})`

## Resource Requirements

Zerops autoscaling needs ~10s to react — `minRam` must absorb the startup/install spike.

**Dev** (install on container): `minRam: 0.5` — `bun install` fast, lower peak than npm.
**Stage/Prod**: `minRam: 0.25` — Bun runtime lightweight.

## Gotchas

- **Tilde in `deployFiles` strips the directory prefix** — `dist/~` extracts contents to `/var/www/`, so `start: bun dist/index.js` fails because the file is at `/var/www/index.js`. Use `dist` (no tilde) to keep the directory.
- **Build and run are separate containers** — only what's in `deployFiles` exists at runtime. If `bun build --target bun` doesn't inline a dependency (native bindings), it must be in `deployFiles`.
- **`BUN_INSTALL: ./.bun` for build caching** — Zerops can only cache paths inside the project tree. Default `~/.bun` is outside it and gets lost between builds.
- **Use `bunx` instead of `npx`** — `npx` may not resolve correctly in the Bun runtime.

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
zerops:
  - setup: prod
    build:
      base: bun@1.2
      envVariables:
        BUN_INSTALL: ./.bun
      buildCommands:
        - bun install --frozen-lockfile
        - bun build src/index.ts --outfile dist/index.js --target bun
        - bun build migrate.ts --outfile dist/migrate.js --target bun
      deployFiles:
        - dist
      cache:
        - node_modules
        - .bun/install/cache
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /
    run:
      base: bun@1.2
      initCommands:
        - zsc execOnce ${appVersionId} -- bun dist/migrate.js
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      start: bun dist/index.js
      healthCheck:
        httpGet:
          port: 3000
          path: /

  - setup: dev
    build:
      base: bun@1.2
      envVariables:
        BUN_INSTALL: ./.bun
      buildCommands:
        - bun install
      deployFiles:
        - ./
      cache:
        - node_modules
        - .bun/install/cache
    run:
      base: bun@1.2
      initCommands:
        - zsc execOnce ${appVersionId} -- bun migrate.ts
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: development
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
        BUN_INSTALL: /var/www/.bun
      start: zsc noop --silent
```
