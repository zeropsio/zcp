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

- **deployFiles is for stage/production** — this recipe shows the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy. With `[.]`, build output stays in its original directory under `/var/www/` — adjust `start` path accordingly (see Deploy Semantics in platform reference).
- **Deploy `build/`, `server.js`, `package.json`, and `node_modules`** — Remix SSR needs the Express server file, build output, and runtime dependencies; deploying `./` also works but is less precise
- **Custom Express server (`server.js`)** — this recipe uses `@remix-run/express` with a custom `server.js`; it must be included in `deployFiles`
- **Port 3000** is the default in `server.js` (reads `process.env.PORT || 3000`) — must be declared in `ports` with `httpSupport: true` for Zerops L7 routing
- **`pnpm start` runs `cross-env NODE_ENV=production node ./server.js`** — ensure `cross-env` is in `dependencies` (not just `devDependencies`) or inline the env var
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
- **Build cache** should include `node_modules` for faster rebuilds
