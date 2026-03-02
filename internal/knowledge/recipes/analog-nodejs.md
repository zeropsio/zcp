# Analog SSR on Zerops

Angular SSR via Analog framework on Node.js runtime.

## Keywords
analog, angular, nodejs, ssr, vite, nitro, javascript, typescript

## TL;DR
Analog Angular SSR -- builds with pnpm on Node.js 20, deploys `dist/~` and starts with `node dist/analog/server/index.mjs` on port 3000.

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
        - dist/~
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      start: node dist/analog/server/index.mjs
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

Analog uses Nitro under the hood for SSR. The build produces a `dist/analog/server/index.mjs` entry point and static assets under `dist/analog/public/`. The deploy path `dist/~` extracts the contents of `dist/` into `/var/www/`, so the start command references `dist/analog/server/index.mjs` relative to the deploy root.

No special configuration is needed in `vite.config.ts` -- Analog auto-detects the correct Nitro preset during build.

## Gotchas
- **Deploy `dist/~`** -- the tilde extracts contents to `/var/www/` so start path is relative to that root
- **Port 3000** is the default Analog/Nitro port -- must be declared in `ports` with `httpSupport: true`
- **Analog binds 0.0.0.0** by default in SSR mode -- no extra host configuration needed
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
- **For static pre-rendered sites** use the `analog-static` recipe with `static` base instead of `nodejs` runtime
