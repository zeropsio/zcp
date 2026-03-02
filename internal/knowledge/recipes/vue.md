# Vue.js Static Site on Zerops

Vue.js single-page application built with Vite and deployed to a static service.

## Keywords
vue, vuejs, vite, static, spa, javascript

## TL;DR
Vue.js SPA built with Vite — builds on Node.js 20 and deploys `dist/~` to a static service. No server runtime needed.

## zerops.yml
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

## import.yml
```yaml
services:
  - hostname: app
    type: static
    enableSubdomainAccess: true
```

## Gotchas
- **Deploy `dist/~`** — tilde deploys directory contents to webroot, not the `dist/` folder itself
- **Builds on Node.js, runs on static** — Node.js is only used at build time; the runtime is a lightweight static file server
- **SPA routing** — for client-side routing (Vue Router in history mode), configure a fallback to `index.html` in Zerops static service settings
- **Environment variables at build time only** — Vite embeds env vars during build; runtime env vars are not available in a static service
