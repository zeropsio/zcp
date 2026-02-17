# Qwik SSR on Zerops

Qwik with Node.js SSR. Requires Express adapter installation before deployment.

## Keywords
qwik, nodejs, ssr, express, resumable, javascript

## TL;DR
Qwik SSR with Express adapter — run `npm run qwik add express` before deploying.

## Pre-deployment (REQUIRED)
```bash
npm run qwik add express
```

This creates:
- `adapters/express/vite.config.ts`
- `src/entry.express.tsx`
- package.json scripts: `build.server`, `serve`

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm run build
    run:
      start: pnpm serve
```

## import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

## Gotchas
- **npm run qwik add express** MUST be run before deploying to Zerops
- Without Express adapter, Qwik SSR will not work
- Package manager is pnpm (not npm) throughout
