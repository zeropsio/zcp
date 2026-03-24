# Bun + Hono + PostgreSQL + Object Storage on Zerops
Bun runtime with Hono web framework, PostgreSQL database, and S3-compatible Object Storage.

## Keywords
bun, hono, typescript

## TL;DR
Bun + Hono framework with PostgreSQL and Object Storage -- includes build step, migrations via `zsc execOnce`, and mandatory 0.0.0.0 binding.

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
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        PORT: "3000"
        DATABASE_HOST: db
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

**CRITICAL**: Hono on Bun must bind to `0.0.0.0`. Default causes 502 Bad Gateway.

Export-based pattern (recommended for Hono + Bun):
```typescript
import { Hono } from "hono";

const app = new Hono();

app.get("/status", (c) => c.json({ status: "ok" }));

export default {
  hostname: "0.0.0.0",
  port: Number(process.env.PORT) || 3000,
  fetch: app.fetch,
};
```

Or explicit Bun.serve:
```typescript
Bun.serve({
  fetch: app.fetch,
  hostname: "0.0.0.0",
  port: Number(process.env.PORT) || 3000,
});
```

## Gotchas

- **`hostname: "0.0.0.0"` is mandatory** -- without it, Zerops L7 balancer cannot reach the app (502)
- **Migration path** -- `src/migrate.ts` must be built into `dist/migrate.js` via `bun build` so it exists at runtime; running `bun run src/migrate.ts` will fail because source files are not deployed
- **`zsc execOnce ${appVersionId}`** -- uses version-specific key so migrations re-run on each new deploy (not a static key like `migrate`)
- **`node_modules` in deployFiles** -- required if migration scripts import ORM packages that cannot be fully bundled (e.g., drizzle-kit, native bindings); omit if `bun build` fully resolves all imports
- **Object Storage uses MinIO** -- `AWS_USE_PATH_STYLE_ENDPOINT: "true"` is required for S3 client compatibility
- **`S3_ENDPOINT`** from `${storage_apiUrl}` is an internal URL -- use `http://`, never `https://`
- **For Drizzle ORM** -- use `drizzle-orm/node-postgres` adapter (Bun compatible)
