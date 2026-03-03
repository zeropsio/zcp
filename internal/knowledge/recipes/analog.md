# Analog on Zerops

## Keywords
analog, angular, nodejs, ssr, static, ssg, vite, nitro, javascript, typescript

## TL;DR
Analog (Angular meta-framework) on Zerops -- SSR on Node.js runtime or static export to static service.

## SSR (Node.js runtime)

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
        - dist/~
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      start: node dist/analog/server/index.mjs
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

Analog uses Nitro under the hood. The build produces `dist/analog/server/index.mjs`. The deploy path `dist/~` extracts contents into `/var/www/`. No special `vite.config.ts` configuration needed.

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
      deployFiles:
        - dist/analog/public/~
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
- **SSR: deploy `dist/~`** -- tilde extracts contents to `/var/www/`, start path is relative to that root
- **SSR: port 3000** is the default Analog/Nitro port -- declare in `ports` with `httpSupport: true`
- **SSR: binds 0.0.0.0** by default in SSR mode
- **Static: deploy `dist/analog/public/~`** -- tilde extracts contents to webroot
- **Static: builds on Node.js but runs on static** -- no Node.js runtime at serve time
