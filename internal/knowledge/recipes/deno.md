# Deno on Zerops

Deno runtime with PostgreSQL. Deploy source files and run with Deno permissions.

## Keywords
deno, typescript, javascript, web server, postgresql, oak, hono, fresh

## TL;DR
Deno 2 with PostgreSQL -- deploy source files, use explicit permission flags, bind `0.0.0.0` with `Deno.serve`.

## zerops.yml

```yaml
zerops:
  - setup: api
    build:
      base: deno@2
      buildCommands:
        - deno cache main.ts
      deployFiles:
        - main.ts
        - deno.json
        - deno.lock
    run:
      ports:
        - port: 8000
          httpSupport: true
      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      healthCheck:
        httpGet:
          port: 8000
          path: /status
      start: deno run --allow-net --allow-env --allow-read main.ts
```

## import.yml

```yaml
services:
  - hostname: api
    type: deno@2
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

HTTP server with health check (Deno.serve):

```typescript
Deno.serve({ port: 8000, hostname: "0.0.0.0" }, (req: Request) => {
  const url = new URL(req.url);
  if (url.pathname === "/status") {
    return new Response(JSON.stringify({ status: "ok" }), {
      headers: { "content-type": "application/json" },
    });
  }
  return new Response("Hello from Deno on Zerops");
});
```

Database connection:

```typescript
import { Client } from "https://deno.land/x/postgres/mod.ts";

const client = new Client({
  hostname: Deno.env.get("DB_HOST"),
  port: Deno.env.get("DB_PORT"),
  user: Deno.env.get("DB_USER"),
  password: Deno.env.get("DB_PASS"),
  database: Deno.env.get("DB_NAME"),
});
await client.connect();
```

## Gotchas

- **Bind `0.0.0.0`** -- use `Deno.serve({ port: 8000, hostname: "0.0.0.0" }, handler)` or the L7 balancer cannot reach your app
- **Explicit permission flags** -- Deno requires `--allow-net --allow-env --allow-read` (or `--allow-all` for development); missing flags cause silent startup failures
- **Deploy specific files** -- deploy only `main.ts`, `deno.json`, `deno.lock` and any source directories; do not deploy `/` which includes unnecessary build artifacts
- **Cache dependencies in build** -- `deno cache main.ts` pre-downloads dependencies so the runtime start is fast
- **Use `deno@2` in import.yml** -- Zerops supports `deno@2` as the type; do not use `deno@1` (deprecated) or `deno@latest` in import.yml
- **DB env vars use cross-service references** -- use `${db_hostname}`, `${db_port}`, `${db_user}`, `${db_password}` syntax, not hardcoded service names
