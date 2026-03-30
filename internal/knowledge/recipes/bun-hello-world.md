---
description: "A Bun application connected to PostgreSQL, running on Zerops with six ready-made environment configurations — from AI agent and remote development to stage and highly-available production."
---

# Bun Hello World on Zerops

## Keywords
bun, bunx, hono, elysia, javascript, typescript

## TL;DR
Bun runtime with npm/yarn/git pre-installed. Bundle to `dist/`, deploy without node_modules. Bind `0.0.0.0` via `Bun.serve`.

## Binding

`Bun.serve({hostname: "0.0.0.0"})` -- default localhost = 502
- Elysia: `hostname: "0.0.0.0"` in constructor
- Hono: `Bun.serve({fetch: app.fetch, hostname: "0.0.0.0"})`

## Resource Requirements

**Dev** (install on container): `minRam: 0.5` — `bun install` fast, lower peak than npm.
**Stage/Prod**: `minRam: 0.25` — Bun runtime lightweight.

## Common Mistakes

- `deployFiles: dist/~` with `start: bun dist/index.js` -- tilde strips the `dist/` prefix, so the file lands at `/var/www/index.js`, not `/var/www/dist/index.js`
- Not binding `0.0.0.0` = 502 Bad Gateway
- Use `bunx` instead of `npx` -- `npx` may not resolve correctly in Bun runtime

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
zerops:
  # Production setup — bundle TypeScript into standalone files, deploy minimal artifacts.
  # bun build --target bun inlines all dependencies: no node_modules at runtime.
  - setup: prod
    build:
      base: bun@1.2
      envVariables:
        # Redirect bun's install cache into the project tree so Zerops can cache it
        # between builds. Default ~/.bun is outside the project and cannot be cached.
        BUN_INSTALL: ./.bun
      buildCommands:
        # --frozen-lockfile: fail if bun.lock would change — reproducible builds
        - bun install --frozen-lockfile
        # Bundle app and migration into standalone files; all pg imports are inlined
        - bun build src/index.ts --outfile dist/index.js --target bun
        - bun build migrate.ts --outfile dist/migrate.js --target bun
      deployFiles:
        # Only bundled artifacts — no node_modules, no source. 156 KB total.
        - dist
      cache:
        - node_modules
        - .bun/install/cache  # Matches BUN_INSTALL path above

    # Readiness check: Zerops verifies each new runtime container responds before the
    # project balancer routes traffic to it — prevents deploying a broken build.
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /

    run:
      base: bun@1.2
      # initCommands run on every container start, before the start command.
      # zsc execOnce runs migration exactly once per version across all containers —
      # prevents race conditions when scaling to multiple containers.
      # In initCommands (not buildCommands) so migration and code deploy atomically.
      initCommands:
        - zsc execOnce ${appVersionId} -- bun dist/migrate.js
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
        DB_NAME: db
        # Zerops generates connection variables per service using {hostname}_{key} pattern
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      start: bun dist/index.js

  # Development setup — deploy full source so developers can work via SSH immediately.
  # Bun is pre-installed; run 'bun --hot src/index.ts' to start with hot reload.
  - setup: dev
    build:
      base: bun@1.2
      envVariables:
        BUN_INSTALL: ./.bun
      buildCommands:
        # Install all dependencies including devDependencies — no compilation.
        # Developer runs the app themselves via SSH.
        - bun install
      deployFiles:
        # Deploy entire working directory: source, node_modules, and bun's package cache
        - ./
      cache:
        - node_modules
        - .bun/install/cache

    run:
      base: bun@1.2
      initCommands:
        # Migration runs at deploy time — DB is ready when developer SSHs in
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
        # Lets developer use 'bun add' via SSH — reuses the cached packages shipped in ./
        BUN_INSTALL: /var/www/.bun
      # Zerops starts nothing — developer drives via SSH: 'bun --hot src/index.ts'
      start: zsc noop --silent
```
