# SolidJS on Zerops

## Keywords
solidjs, solidstart, vinxi, solid-router, fine-grained-reactivity

## TL;DR
SolidJS SPA — deploy `dist/~` to static service. SolidStart SSR — deploy `.output/` to Node.js runtime with `trust proxy`.

## Static SPA (Vite + vite-plugin-solid)

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

## SSR (SolidStart + Vinxi)

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles:
        - .output
        - public
        - node_modules
        - package.json
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
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

## Gotchas
- **Static: `dist/~`** — tilde deploys directory contents to webroot; no `ports` or `start` needed on static base
- **SSR: `.output/` is the Vinxi build artifact** — deploy `.output/`, `public/`, `node_modules/`, and `package.json`; all four required at runtime
- **SSR: port 3000** — Vinxi default; declare in `ports` with `httpSupport: true`
- **SSR: `trust proxy`** — set in `app.config.ts` or the Nitro/H3 adapter config for correct IP behind the Zerops L7 balancer
- **SSR: config file is `app.config.ts`** — not `vite.config.ts`; SolidStart uses Vinxi, not plain Vite
