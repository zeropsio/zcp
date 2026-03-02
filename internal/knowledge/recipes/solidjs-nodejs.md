# SolidStart SSR on Zerops

SolidStart with server-side rendering on Node.js using Vinxi.

## Keywords
solidjs, solidstart, nodejs, ssr, vinxi, javascript, typescript

## TL;DR
SolidStart SSR with Vinxi — deploy `.output/`, `public/`, `node_modules/`, and `package.json`, port 3000 with httpSupport.

## zerops.yml
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

## import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

## Gotchas
- **Deploy `.output/`, `public/`, `node_modules/`, and `package.json`** — SolidStart builds to `.output/` via Vinxi; all four are required at runtime
- **Port 3000** is the Vinxi default — must be declared in `ports` with `httpSupport: true` for Zerops L7 routing
- **`pnpm start` runs `vinxi start`** — Vinxi serves the SSR application in production mode
- **SolidStart uses Vinxi under the hood** — configuration lives in `app.config.ts`, not `vite.config.ts`
- **Build cache** should include `node_modules` for faster rebuilds
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
- **For static SolidJS** see the `solidjs-static` recipe instead (uses `static` base, no Node.js runtime)
