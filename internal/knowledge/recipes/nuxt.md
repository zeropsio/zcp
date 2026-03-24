# Nuxt on Zerops

SSR with Nitro `node-server` preset on Node.js, or static pre-rendering to static service.

## Keywords
nuxt, vue, nitro, node-server, nuxi

## TL;DR
Deploy `.output/~` for SSR — tilde strips the prefix so start path is `server/index.mjs`. Static: `nuxi generate` → `.output/public/~`. Port 3000, binds 0.0.0.0 by default.

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
        - .output/~
      cache: node_modules
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server/index.mjs
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

Nuxt uses Nitro under the hood. The default preset is `node-server`, auto-detected during build. `.output/~` deploys contents of `.output/` to `/var/www/`, so the start command is `server/index.mjs` (not `.output/server/index.mjs`).

## Static Pre-rendering

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npx nuxi generate
      deployFiles:
        - .output/public/~
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

`nuxi generate` produces static HTML into `.output/public/`. No special `nuxt.config.ts` changes needed.

## Gotchas
- **SSR: deploy `.output/~`** — tilde extracts contents so start path is `server/index.mjs`, not `.output/server/index.mjs`
- **SSR: port 3000** — declare in `ports` with `httpSupport: true` for Zerops L7 routing
- **SSR: do NOT use `nuxi generate`** — that produces static HTML; use `nuxt build` for SSR
- **Static: use `nuxi generate`** not `nuxt build` — `nuxt build` produces an SSR server
- **Static: deploy `.output/public/~`** — tilde extracts contents to webroot
