# Analog on Zerops

Angular meta-framework — SSR via Nitro on Node.js runtime, or static pre-rendering to static service. SSR mode supports server routes for API endpoints and managed service connections.

## Keywords
analog, angular, nodejs, ssr, static, ssg, vite, nitro, javascript, typescript

## TL;DR
Analog on Node.js (SSR) or static service. Deploy `dist/~` for SSR, start `node analog/server/index.mjs` (NOT `dist/analog/...` — tilde strips the prefix). Port 3000 default. Wire managed services via `${hostname_varName}` refs in server routes.

## SSR (Node.js runtime)

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles:
        - dist/~
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
      start: node analog/server/index.mjs
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

### import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

### Configuration

Analog uses Nitro under the hood. `dist/~` deploys the **contents** of `dist/` to `/var/www/`, so the start path is `analog/server/index.mjs` (not `dist/analog/server/index.mjs`). Nitro binds `0.0.0.0:3000` by default — no HOST override needed.

## Static Pre-rendering

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles:
        - dist/analog/public/~
      cache: node_modules
    run:
      base: static
```

### import.yml
```yaml
services:
  - hostname: app
    type: static
    enableSubdomainAccess: true
```

For static export, set `ssr: false` in the analog plugin options in `vite.config.ts`. Without this, Analog produces an SSR server that cannot run on a static service.

## Wiring Managed Services (SSR only)

Cross-service pattern: `${hostname_varName}` — resolved at container start. After adding a managed service via import, run `zerops_discover includeEnvs=true` to see exact var names. Map ONLY discovered vars — guessing names causes silent failures.

Server routes access env vars via `process.env`:
```typescript
// src/server/routes/api/data.ts
export default defineEventHandler(async () => {
  const dbUrl = process.env.DATABASE_URL;
});
```

### + Database (PostgreSQL)

**Add to import.yml:**
```yaml
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

**Add to zerops.yml envVariables:**
```yaml
        DATABASE_URL: ${db_connectionString}
```

Or individual vars: `DB_HOST: ${db_hostname}`, `DB_PORT: ${db_port}`, `DB_USER: ${db_user}`, `DB_PASSWORD: ${db_password}`, `DB_NAME: ${db_dbName}`.

### + Cache (Valkey)

**Add to import.yml:**
```yaml
  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10
```

**Add to zerops.yml envVariables:**
```yaml
        REDIS_URL: redis://${cache_hostname}:${cache_port}
```

No auth needed — Valkey on private network. Use `${cache_hostname}`, NOT `${cache_host}` (`host` does not exist as a Valkey env var).

### + File Storage (Object Storage)

**Add to import.yml:**
```yaml
  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

**Add to zerops.yml envVariables:**
```yaml
        S3_ACCESS_KEY: ${storage_accessKeyId}
        S3_SECRET_KEY: ${storage_secretAccessKey}
        S3_BUCKET: ${storage_bucketName}
        S3_ENDPOINT: ${storage_apiUrl}
        S3_REGION: us-east-1
```

Requires `forcePathStyle: true` in S3 SDK config (Zerops uses MinIO backend).

### + Search, Message Brokers, etc.

Same pattern for any managed service. After import:
1. `zerops_discover includeEnvs=true` — find available var names
2. Map in zerops.yml: `MY_VAR: ${hostname_varName}`
3. Install the Node.js client package

## Dev vs Stage

Managed services are **shared** — both dev and stage connect to the same `db`, `cache`, `storage`. Only app-specific config differs:

| | Dev | Stage |
|---|-----|-------|
| `NODE_ENV` | `development` | `production` |
| `healthCheck` | omit | `/ on :3000` |
| `readinessCheck` | omit | `/ on :3000` |
| Service refs | `${db_hostname}`, etc. | **same** |

Stage zerops.yml additions:
```yaml
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /
```

## Scaffolding

```bash
npm create analog@latest .
pnpm install
```

SSR is the default mode. For static pre-rendering, set `ssr: false` in `vite.config.ts` analog plugin options.

## Gotchas
- **SSR: start path after tilde deploy** — `dist/~` extracts contents to `/var/www/`, so start is `node analog/server/index.mjs`, NOT `node dist/analog/server/index.mjs`
- **SSR: port 3000** is the default Nitro port — declare in `ports` with `httpSupport: true`
- **SSR: binds 0.0.0.0** by default — no HOST override needed
- **Static: `ssr: false` required** — without it, Analog builds an SSR server that fails on static service
- **Static: deploy `dist/analog/public/~`** — tilde extracts contents to webroot
- **Static: no server routes** — API endpoints and server-side features are not available at runtime
- **Valkey var is `hostname`** — use `${cache_hostname}`, NOT `${cache_host}`. `host` does not exist
- **S3: `forcePathStyle: true`** — required in SDK config for Zerops object storage (MinIO backend)
- **No `.env` files** — all env vars via zerops.yml `envVariables`. `.env` files shadow OS env vars
- **Build cache** — `cache: node_modules` for faster rebuilds; `pnpm` store can also be cached
