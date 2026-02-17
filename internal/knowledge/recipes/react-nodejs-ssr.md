# React SSR with Express on Zerops

React with Vite + Express SSR. Requires custom server.js implementation.

## Keywords
react, nodejs, express, ssr, vite, server-side rendering

## TL;DR
React SSR with Vite + Express — requires custom `server.js` and deploys `node_modules` for runtime dependencies.

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
        - public/
        - node_modules/
        - dist/
        - package.json
        - server.js
    run:
      ports:
        - port: 3000
          httpSupport: true
      start: pnpm start
```

## import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

## Gotchas
- **Custom server.js** MUST be implemented for Express server
- Not a standard Vite setup — requires manual SSR server implementation
- Deploy includes node_modules (runtime dependencies needed)
- For static React (SPA), use `static` base with `npm run build` instead
