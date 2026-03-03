# React on Zerops

## Keywords
react, nodejs, express, ssr, static, ssg, spa, vite, javascript, typescript

## TL;DR
React on Zerops -- SSR with Vite + Express on Node.js or static SPA to static service.

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
        - public
        - node_modules
        - dist
        - package.json
        - server.js
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

### Configuration

SSR requires a custom Express server (`server.js`). Enable trust proxy:
```javascript
// server.js
import express from 'express';
const app = express();
app.set('trust proxy', true);
// ... SSR rendering logic ...
app.listen(3000);
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
- **SSR: custom `server.js` is required** -- not a standard Vite SPA setup
- **SSR: deploy includes `node_modules/`** -- runtime dependencies needed by Express
- **SSR: deploy includes `server.js`** -- must be explicitly listed in `deployFiles`
- **SSR: trust proxy** -- `app.set('trust proxy', true)` for correct client IP
- **Static: deploy `dist/~`** -- tilde deploys directory contents to webroot
- **Static: no `ports` or `start` needed** -- `static` base serves on port 80 automatically
- **For React SSR frameworks** (Next.js, Remix), use dedicated recipes instead
