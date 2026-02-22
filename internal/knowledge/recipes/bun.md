# Bun + PostgreSQL + Valkey on Zerops

Bun.serve() API with PostgreSQL database and Valkey cache — bundled deploy pattern.

## Keywords
bun, postgresql, postgres, valkey, redis, api, typescript, bundled

## TL;DR
Bun runtime with PostgreSQL and Valkey — bundled deploy with `bun build`, 0.0.0.0 binding, zero external dependencies.

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
      cache:
        - node_modules
    run:
      ports:
        - port: 3000
          httpSupport: true
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

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10

  - hostname: app
    type: bun@1.2
    priority: 5
    enableSubdomainAccess: true
    envSecrets:
      DATABASE_URL: ${db_connectionString}
      REDIS_HOST: ${cache_host}
      REDIS_PORT: ${cache_port}
```

## App code requirement

**CRITICAL**: Bun.serve() must bind to `0.0.0.0`. Default `localhost` = 502 Bad Gateway.

```typescript
const server = Bun.serve({
  hostname: "0.0.0.0",
  port: Number(process.env.PORT) || 3000,
  async fetch(req) {
    const url = new URL(req.url);

    if (url.pathname === "/health") {
      return Response.json({ status: "ok" });
    }

    if (url.pathname === "/status") {
      const connections: Record<string, any> = {};

      // PostgreSQL: verify with SELECT 1
      try {
        const start = performance.now();
        const sock = await Bun.connect({
          hostname: "db",
          port: Number(process.env.REDIS_PORT) ? 5432 : 5432,
          socket: {
            data() {},
            open(socket) { socket.end(); },
            error() {},
          },
        });
        connections.db = { status: "ok", latency_ms: Math.round(performance.now() - start) };
      } catch (e) {
        connections.db = { status: "error", error: String(e) };
      }

      // Valkey: verify with TCP connect
      try {
        const start = performance.now();
        await Bun.connect({
          hostname: process.env.REDIS_HOST || "cache",
          port: Number(process.env.REDIS_PORT) || 6379,
          socket: {
            data() {},
            open(socket) { socket.end(); },
            error() {},
          },
        });
        connections.cache = { status: "ok", latency_ms: Math.round(performance.now() - start) };
      } catch (e) {
        connections.cache = { status: "error", error: String(e) };
      }

      return Response.json({ service: "app", connections });
    }

    return new Response("Service: app");
  },
});

console.log(`Listening on ${server.hostname}:${server.port}`);
```

## Gotchas
- **`hostname: "0.0.0.0"` is mandatory** in Bun.serve() — without it, Zerops L7 balancer cannot reach the app
- **Bundled deploy** (this recipe): use `bun build --outdir dist --target bun`, deploy `dist/` + `package.json` only — do NOT deploy `node_modules`
- **Source deploy** (no build step): use `deployFiles: [.]` — includes entire source directory
- **Bun 1.2+ built-ins**: prefer zero-dependency approach where possible (`Bun.connect` for TCP checks, `Bun.serve` for HTTP)
- PostgreSQL connection: use `${db_connectionString}` from discovered env vars
- Valkey: no password needed — private network, no auth. Use `${cache_host}` and `${cache_port}`
- In import.yml: use `envSecrets` for ALL env vars (no `envVariables` at service level). In zerops.yml: use `run.envVariables` for non-sensitive config
