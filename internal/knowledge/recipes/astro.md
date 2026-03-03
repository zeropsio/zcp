# Astro on Zerops

## Keywords
astro, nodejs, ssr, static, ssg, adapter-node, typescript, javascript

## TL;DR
Astro on Zerops -- SSR with `@astrojs/node` adapter on Node.js or static export (default mode) to static service.

## SSR (Node.js runtime)

### zerops.yml
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

### import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

### Configuration
The project must use the `@astrojs/node` adapter in standalone mode in `astro.config.mjs`:
```javascript
import node from "@astrojs/node";

export default defineConfig({
  output: "server",
  adapter: node({ mode: "standalone" }),
});
```
Without this adapter, Astro defaults to static output which cannot run on a Node.js service.

## Static Export

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm build
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

## Gotchas
- **SSR: `@astrojs/node` adapter is required** -- install with `pnpm add @astrojs/node` and configure with `mode: "standalone"`
- **SSR: `output: "server"`** must be set in astro.config.mjs to enable SSR
- **SSR: deploy `dist`, `package.json`, and `node_modules`** -- SSR needs runtime dependencies
- **SSR: HOST must be `0.0.0.0`** -- binding to all interfaces required by Zerops
- **Static: no adapter needed** -- Astro defaults to static output
- **Static: deploy `dist/~`** -- tilde deploys directory contents to webroot
- **Static: no server-side features** -- API routes and SSR are not available
