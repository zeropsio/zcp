# Astro on Zerops

SSR with `@astrojs/node` adapter on Node.js, or static output (default mode) to static service.

## Keywords
astro, adapter-node, island-architecture, content-collections

## TL;DR
SSR requires `@astrojs/node` adapter in standalone mode. Deploy `dist`, `package.json`, and `node_modules` separately — runtime does NOT run `npm install`. `HOST: 0.0.0.0` required. Static: deploy `dist/~`.

## SSR (Node.js runtime)

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist
        - package.json
        - node_modules
      cache: node_modules
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        PORT: "3000"
        HOST: 0.0.0.0
      start: npm start
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

### import.yml
```yaml
services:
  - hostname: app
    type: nodejs@22
    enableSubdomainAccess: true
```

Requires `@astrojs/node` adapter in `astro.config.mjs`:
```javascript
import node from "@astrojs/node";
export default defineConfig({
  output: "server",
  adapter: node({ mode: "standalone" }),
});
```
Without this adapter, Astro defaults to static output and fails on a Node.js service.

## Static Export

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles: dist/~
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

Astro defaults to static output — no adapter or config change needed.

## Gotchas
- **SSR: `@astrojs/node` adapter required** — install with `npm add @astrojs/node`, configure `mode: "standalone"` and `output: "server"`
- **SSR: deploy `dist`, `package.json`, `node_modules` separately** — the runtime does NOT run `npm install`; `node_modules` must be in `deployFiles`
- **SSR: `HOST: 0.0.0.0`** — binding to all interfaces is required by Zerops; set via `PORT`/`HOST` env vars
- **SSR: port 3000** — declare in `ports` with `httpSupport: true` for Zerops L7 routing
- **Static: `dist/~`** — tilde deploys directory contents to webroot
- **Static: no adapter needed** — Astro defaults to static output without any configuration
