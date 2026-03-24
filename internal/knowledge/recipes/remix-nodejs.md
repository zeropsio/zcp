# Remix SSR on Zerops

Remix with server-side rendering on Node.js using a custom Express server.

## Keywords
remix, react-router, express, vite-remix

## TL;DR
Deploy `build/`, `server.js`, `package.json`, and `node_modules` — runtime does NOT run `npm install`. Port 3000 with `httpSupport: true`. `pnpm start` runs `node ./server.js` (not the default Remix dev server).

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - build
        - server.js
        - package.json
        - node_modules
      cache: node_modules
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server.js
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

## import.yml
```yaml
services:
  - hostname: app
    type: nodejs@22
    enableSubdomainAccess: true
```

## Gotchas
- **Deploy `build/`, `server.js`, `package.json`, `node_modules`** — runtime does NOT run `npm install`; `node_modules` must be in `deployFiles`
- **`server.js` must be in `deployFiles`** — the Express server entry point is not inside `build/`; omitting it causes startup failure
- **Port 3000** — must be declared in `ports` with `httpSupport: true` for Zerops L7 routing
- **`cross-env` must be in `dependencies`** — if `server.js` uses `cross-env`, it must not be devDependencies-only, or inline the env var instead
