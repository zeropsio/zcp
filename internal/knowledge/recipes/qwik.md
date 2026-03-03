# Qwik on Zerops

## Keywords
qwik, nodejs, ssr, static, ssg, express, resumable, javascript, typescript, vite

## TL;DR
Qwik on Zerops -- SSR with Express adapter on Node.js or static export with static adapter.

## SSR (Node.js runtime)

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm run build
      deployFiles:
        - package.json
        - public
        - server
        - dist
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      start: pnpm serve
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

The Express adapter must be added to the Qwik project before deploying:

```bash
npm run qwik add express
```

Enable trust proxy in `src/entry.express.tsx`:
```typescript
app.set('trust proxy', true);
```

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
        - public
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

### Configuration
The static adapter must be added before deploying:
```bash
pnpm qwik add static
```

## Gotchas
- **SSR: `npm run qwik add express` MUST be run first** -- without the Express adapter, SSR will not work
- **SSR: `pnpm serve`** is the start command (not `pnpm start`) -- added by the Express adapter
- **SSR: deploy four directories** -- `package.json`, `public/`, `server/`, and `dist/` are all required
- **SSR: trust proxy** -- add `app.set('trust proxy', true)` for correct client IP
- **Static: `pnpm qwik add static` MUST be run** -- without it, build produces a server bundle
- **Static: deploy `public` and `dist/~`** -- both static assets and built output are needed
