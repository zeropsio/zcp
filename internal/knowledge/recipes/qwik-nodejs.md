# Qwik SSR on Zerops

Qwik with Express adapter for Node.js server-side rendering. Requires adapter installation before deployment.

## Keywords
qwik, nodejs, ssr, express, resumable, javascript, typescript

## TL;DR
Qwik SSR with Express adapter — run `npm run qwik add express` before deploying, deploy `package.json`, `public/`, `server/`, `dist/`.

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

## import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

## Configuration

The Express adapter must be added to the Qwik project before deploying to Zerops. This is a one-time setup step:

```bash
npm run qwik add express
```

This creates:
- `adapters/express/vite.config.ts`
- `src/entry.express.tsx`
- `build.server` and `serve` scripts in package.json

The Express adapter provides `trust proxy` support. Enable it in the generated `src/entry.express.tsx`:

```typescript
app.set('trust proxy', true);
```

## Gotchas
- **`npm run qwik add express` MUST be run first** -- without the Express adapter, Qwik SSR will not work on Zerops; this is a one-time project setup step
- **Port 3000** is the Express adapter default -- must be declared in `ports` with `httpSupport: true`
- **Deploy four directories** -- `package.json`, `public/`, `server/`, and `dist/` are all required for the SSR server
- **`pnpm serve`** is the start command (not `pnpm start`) -- added by the Express adapter
- **trust proxy** -- add `app.set('trust proxy', true)` in `entry.express.tsx` for correct client IP behind Zerops L7 balancer
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
