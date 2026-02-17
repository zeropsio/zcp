# Bun + Hono + PostgreSQL + Object Storage on Zerops

Bun runtime with Hono web framework, PostgreSQL database, and S3-compatible Object Storage.

## Keywords
bun, hono, postgresql, postgres, object-storage, s3, api, typescript

## TL;DR
Bun + Hono framework with PostgreSQL and Object Storage — includes build step, migrations, and 0.0.0.0 binding.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: bun@1.2
      buildCommands:
        - bun install
        - bun build src/index.ts --outdir dist --target bun
      deployFiles:
        - dist
        - package.json
        - bun.lockb
      cache:
        - node_modules
    run:
      ports:
        - port: 3000
          httpSupport: true
      initCommands:
        - zsc execOnce migrate -- bun run src/migrate.ts
      start: bun run dist/index.js
      envVariables:
        PORT: "3000"
```

## import.yml
```yaml
#yamlPreprocessor=on
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    priority: 10

  - hostname: app
    type: bun@1.2
    priority: 5
    enableSubdomainAccess: true
    envSecrets:
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
```

## App code requirement

**CRITICAL**: Hono on Bun must bind to `0.0.0.0`. Default = 502 Bad Gateway.

```typescript
import { Hono } from "hono";

const app = new Hono();

app.get("/health", (c) => c.text("OK"));

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
- **`hostname: "0.0.0.0"` is mandatory** — without it, Zerops L7 balancer cannot reach the app
- Bundled output (`bun build --target bun`): deploy `dist/` only, no `node_modules`
- Migrations via `zsc execOnce` — runs exactly once across all containers (HA-safe)
- Object Storage uses MinIO — `AWS_USE_PATH_STYLE_ENDPOINT: true` is required
- `S3_ENDPOINT` from `${storage_apiUrl}` is internal URL — use `http://`, never `https://`
- For Drizzle ORM: use `drizzle-orm/node-postgres` adapter (Bun compatible)
