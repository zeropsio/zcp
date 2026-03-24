# Next.js on Zerops

SSR on Node.js runtime or static export to static service.

## Keywords
nextjs, next.js, app-router, pages-router, standalone

## TL;DR
Node.js runtime for SSR (`deployFiles: ./` — entire workspace required). Static service for `output: 'export'` mode (`deployFiles: out/~`). Port 3000, binds 0.0.0.0 by default.

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
      deployFiles: ./
      cache: node_modules
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: npm start
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

Deploy `./` (entire workspace) — Next.js SSR requires `.next/`, `node_modules/`, and `package.json` at runtime. Do NOT set `output: 'export'` in next.config — that disables SSR.

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
      deployFiles: out/~
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

Requires `output: 'export'` in `next.config.mjs`. Without it, Next.js defaults to SSR mode and fails on a static service.

## Gotchas
- **SSR: deploy `./` (entire workspace)** — `.next/`, `node_modules/`, and `package.json` are all required at runtime; the runtime container does NOT run `npm install`
- **SSR: port 3000** — declare in `ports` with `httpSupport: true` for Zerops L7 routing
- **Static: `output: 'export'` is MANDATORY** — without it, Next.js attempts SSR and fails on a static service
- **Static: `out/~`** — tilde deploys directory contents to webroot
- **Static: no server-side features** — API routes, `getServerSideProps`, ISR, and middleware are incompatible with static export
