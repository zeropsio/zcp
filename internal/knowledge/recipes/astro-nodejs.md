# Astro SSR on Zerops (Node.js)

Astro with server-side rendering using the `@astrojs/node` adapter on Node.js runtime.

## Keywords
astro, nodejs, ssr, server-side rendering, typescript

## TL;DR
Astro SSR with `@astrojs/node` adapter in standalone mode — deploy `dist`, `package.json`, and `node_modules`, port 3000 with httpSupport. Requires `output: "server"` in astro.config.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm run build
      deployFiles:
        - dist
        - package.json
        - node_modules
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        PORT: 3000
        HOST: 0.0.0.0
      start: pnpm start
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

## import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

## Configuration
The project must use the `@astrojs/node` adapter in standalone mode in `astro.config.mjs`:
```javascript
import node from "@astrojs/node";

export default defineConfig({
  output: "server",
  adapter: node({ mode: "standalone" }),
});
```
Without this adapter, Astro defaults to static output which cannot run on a Node.js service.

## Gotchas
- **`@astrojs/node` adapter is required** — install with `pnpm add @astrojs/node` and configure in astro.config.mjs with `mode: "standalone"`
- **`output: "server"`** must be set in astro.config.mjs to enable SSR (without it, Astro generates static files)
- **Deploy `dist`, `package.json`, and `node_modules`** — SSR needs runtime dependencies; deploying `./` also works but is larger
- **HOST must be `0.0.0.0`** — Zerops internal routing requires binding to all interfaces, not localhost
- **Port 3000** is the default — must be declared in `ports` with `httpSupport: true` for Zerops L7 routing
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
- For static Astro sites without SSR, use the `astro-static` recipe instead (uses `static` base, no Node.js runtime)
