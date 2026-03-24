# Qwik on Zerops

Qwik City with resumable SSR via Express adapter on Node.js, or static export.

## Keywords
qwik, qwik-city, resumable, qwikcity

## TL;DR
SSR requires `npm run qwik add express` before deploying. Deploy `package.json`, `public/`, `server/`, `dist/` — runtime does NOT run `npm install`. Start command is `node server/entry.express.js` (not `npm start`). Trust proxy required.

## SSR (Node.js runtime)

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - package.json
        - public
        - server
        - dist
        - node_modules
      cache: node_modules
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node server/entry.express.js
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

### import.yml
```yaml
services:
  - hostname: app
    type: nodejs@22
    enableSubdomainAccess: true
```

The Express adapter must be added before deploying:
```bash
npm run qwik add express
```

Add trust proxy in `src/entry.express.tsx`:
```typescript
app.set('trust proxy', true);
```

## Static Export

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist/~
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

The static adapter must be added before deploying:
```bash
npm run qwik add static
```

## Gotchas
- **SSR: `npm run qwik add express` MUST be run first** — without the Express adapter, there is no `server/` directory and SSR fails
- **SSR: deploy `node_modules`** — runtime does NOT run `npm install`; add to `deployFiles`
- **SSR: deploy four directories** — `package.json`, `public/`, `server/`, `dist/` and `node_modules` are all required
- **SSR: trust proxy** — add `app.set('trust proxy', true)` in `src/entry.express.tsx` for correct client IP behind Zerops L7 balancer
- **Static: `npm run qwik add static` MUST be run** — without it, build produces a server bundle instead of static output
- **Static: deploy `dist/~`** — tilde deploys directory contents to webroot
