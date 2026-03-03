# Remix SSR on Zerops

Remix with server-side rendering on Node.js using a custom Express server.

## Keywords
remix, nodejs, ssr, react, express, server-side rendering, vite, typescript

## TL;DR
Remix SSR with Express — deploy `build/`, `server.js`, `package.json`, and `node_modules`, port 3000 with httpSupport.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm run build
      deployFiles:
        - build
        - server.js
        - package.json
        - node_modules
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

- **Deploy `build/`, `server.js`, `package.json`, and `node_modules`** — Remix SSR needs the Express server file, build output, and runtime dependencies; deploying `./` also works but is less precise
- **Custom Express server (`server.js`)** — this recipe uses `@remix-run/express` with a custom `server.js`; it must be included in `deployFiles`
- **Port 3000** is the default in `server.js` (reads `process.env.PORT || 3000`) — must be declared in `ports` with `httpSupport: true` for Zerops L7 routing
- **`pnpm start` runs `cross-env NODE_ENV=production node ./server.js`** — ensure `cross-env` is in `dependencies` (not just `devDependencies`) or inline the env var
- **Build cache** should include `node_modules` for faster rebuilds
