# Nuxt on Zerops

## Keywords
nuxt, vue, nodejs, ssr, static, ssg, nitro, node-server, javascript, typescript

## TL;DR
Nuxt 3 on Zerops -- SSR with `node-server` preset on Node.js or static pre-rendering with `nuxi generate` to static service.

## SSR (Node.js runtime)

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - yarn
        - yarn build
      deployFiles:
        - .output/~
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
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
    type: nodejs@20
    enableSubdomainAccess: true
```

### Configuration

Nuxt uses Nitro under the hood. The default preset is `node-server`, auto-detected during build. The deploy path `.output/~` extracts contents into `/var/www/`, so the start command is `server/index.mjs` (not `.output/server/index.mjs`).

## Static Pre-rendering

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - yarn
        - yarn nuxi generate
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

### Configuration

`nuxi generate` produces static HTML into `.output/public/`. No special `nuxt.config.ts` configuration needed.

## Gotchas
- **SSR: deploy `.output/~`** -- tilde extracts contents so start path is `server/index.mjs`
- **SSR: port 3000** is the default Nuxt/Nitro port -- declare in `ports` with `httpSupport: true`
- **SSR: binds 0.0.0.0** by default in SSR mode
- **SSR: do NOT use `nuxt generate`** for SSR -- that produces static HTML; use `nuxt build`
- **Static: use `nuxi generate`** not `nuxt build` -- `nuxt build` produces an SSR server
- **Static: deploy `.output/public/~`** -- tilde extracts contents to webroot
- **Static: no server-side features** at runtime -- API routes and SSR are not available
