# SolidJS on Zerops

## Keywords
solidjs, solidstart, nodejs, ssr, static, ssg, vite, vinxi, javascript, typescript

## TL;DR
SolidJS on Zerops -- SSR with SolidStart/Vinxi on Node.js or static SPA with Vite to static service.

## SSR (SolidStart + Node.js runtime)

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

## Static SPA

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
- **SSR: deploy `.output/`, `public/`, `node_modules/`, and `package.json`** -- all four required at runtime
- **SSR: port 3000** is the Vinxi default -- declare in `ports` with `httpSupport: true`
- **SSR: `pnpm start` runs `vinxi start`** -- Vinxi serves the SSR application
- **SSR: configuration lives in `app.config.ts`** -- not `vite.config.ts` (SolidStart uses Vinxi)
- **Static: this is a client-side SPA** -- uses `vite-plugin-solid` and plain Vite, not SolidStart
- **Static: deploy `dist/~`** -- tilde deploys directory contents to webroot
