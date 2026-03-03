# Bun + PostgreSQL + Valkey on Zerops
Bun.serve() API with PostgreSQL database and Valkey cache -- bundled deploy pattern.

## Keywords
bun, postgresql, postgres, valkey, redis, api, typescript, bundled

## TL;DR
Bun runtime with PostgreSQL and Valkey -- bundled deploy with `bun build`, 0.0.0.0 binding, zero external dependencies at runtime.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: bun@1.2
      buildCommands:
        - bun install
        - bun build src/index.ts --outdir dist --target bun
      deployFiles:
        - dist
        - package.json
      cache:
        - node_modules
    run:
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
        PORT: "3000"
        DB_NAME: db
        DB_HOST: db
        DB_USER: db
        DB_PASS: ${db_password}
      start: bun run dist/index.js
      healthCheck:
        httpGet:
          port: 3000
          path: /status
```

## import.yml
```yaml
#yamlPreprocessor=on
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10

  - hostname: api
    type: bun@1.2
    priority: 5
    enableSubdomainAccess: true
    envSecrets:
      DATABASE_URL: postgresql://${db_user}:${db_password}@db:${db_port}/${db_user}
      REDIS_HOST: ${cache_host}
      REDIS_PORT: ${cache_port}
```

## Configuration

**CRITICAL**: Bun.serve() must bind to `0.0.0.0`. Default `localhost` causes 502 Bad Gateway.

```typescript
const server = Bun.serve({
  hostname: "0.0.0.0",
  port: Number(process.env.PORT) || 3000,
  async fetch(req) {
    const url = new URL(req.url);

    if (url.pathname === "/status") {
      return Response.json({ status: "ok" });
    }

    return new Response("Service: api");
  },
});

console.log(`Listening on ${server.hostname}:${server.port}`);
```

## Gotchas

- **deployFiles is for stage/production** — this recipe shows the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy. With `[.]`, build output stays in its original directory under `/var/www/` — adjust `start` path accordingly (see Deploy Semantics in platform reference).
- **`hostname: "0.0.0.0"` is mandatory** in Bun.serve() -- without it, Zerops L7 balancer cannot reach the app (502)
- **Bundled deploy** -- use `bun build --outdir dist --target bun`, deploy `dist/` + `package.json` only, do NOT deploy `node_modules`
- **`DATABASE_URL` completeness** -- the full connection string is assembled from individual env var references in import.yml `envSecrets`
- **DB env vars** -- both `DATABASE_URL` (via envSecrets) and individual `DB_*` vars (via zerops.yml envVariables) are available at runtime
- **Valkey** -- no password needed on the private network, use `${cache_host}` and `${cache_port}` for connection
- **envSecrets vs envVariables** -- use `envSecrets` in import.yml for values containing cross-service references; use `envVariables` in zerops.yml for static config
- **Health check** -- `/status` endpoint is recommended for Zerops readiness verification
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
