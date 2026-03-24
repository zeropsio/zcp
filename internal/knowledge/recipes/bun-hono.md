# Bun + Hono on Zerops

Bun runtime with Hono web framework, PostgreSQL, and S3-compatible object storage.

## Keywords
bun, hono, bun-runtime, bun-serve, drizzle

## TL;DR
Bun + Hono with PostgreSQL and object storage — `hostname: "0.0.0.0"` mandatory, migrations via `zsc execOnce`, build outputs to `dist/`.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: bun@1.2
      buildCommands:
        - bun install
        - bun build src/index.ts --outdir dist --target bun
        - bun build src/migrate.ts --outdir dist --target bun
      deployFiles:
        - dist
        - package.json
        - node_modules
      cache:
        - node_modules
    run:
      base: bun@1.2
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        PORT: "3000"
        DATABASE_HOST: ${db_hostname}
        DATABASE_PORT: ${db_port}
        DATABASE_NAME: ${db_dbName}
        DATABASE_USER: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        S3_ENDPOINT: ${storage_apiUrl}
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
        S3_BUCKET: ${storage_bucketName}
        S3_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_USE_PATH_STYLE_ENDPOINT: "true"
      initCommands:
        - zsc execOnce ${appVersionId} -- bun run dist/migrate.js
      start: bun run dist/index.js
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

## import.yml
```yaml
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10

  - hostname: app
    type: bun@1.2
    priority: 5
    enableSubdomainAccess: true
```

## Configuration

Hono on Bun must bind to `0.0.0.0`. Default localhost binding causes 502 Bad Gateway behind the Zerops L7 balancer:

```typescript
// src/index.ts — export-based pattern
export default {
  hostname: "0.0.0.0",
  port: Number(process.env.PORT) || 3000,
  fetch: app.fetch,
};

// or explicit Bun.serve
Bun.serve({
  fetch: app.fetch,
  hostname: "0.0.0.0",
  port: Number(process.env.PORT) || 3000,
});
```

## Gotchas

- **`hostname: "0.0.0.0"` is mandatory** — without it, Zerops L7 balancer cannot reach the app (502 Bad Gateway)
- **Build migration scripts** — `src/migrate.ts` must be compiled to `dist/migrate.js` via `bun build`; source files are not deployed, so `bun run src/migrate.ts` fails at runtime
- **`zsc execOnce ${appVersionId}`** — version-specific key means migrations re-run on each new deploy
- **`node_modules` in deployFiles** — required if migration scripts import packages with native bindings (e.g., drizzle-kit); omit if `bun build` fully bundles all imports
- **Object Storage uses MinIO** — `AWS_USE_PATH_STYLE_ENDPOINT: "true"` required for S3 client compatibility
- **Drizzle ORM** — use `drizzle-orm/node-postgres` adapter (Bun-compatible)
