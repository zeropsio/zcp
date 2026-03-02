# Next.js SSR on Zerops

Next.js with server-side rendering on Node.js runtime. Not a static export.

## Keywords
nextjs, next.js, nodejs, ssr, react, server-side rendering, typescript

## TL;DR
Next.js SSR on Node.js — deploy the entire workspace, port 3000 with httpSupport, do NOT set `output: 'export'`.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm run build
      deployFiles: ./
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
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
- **Do NOT set `output: 'export'`** in next.config.mjs -- that disables SSR and forces static export; use the `nextjs-static` recipe for static sites
- **Deploy `./` (entire workspace)** -- Next.js SSR requires `.next/`, `node_modules/`, and `package.json` at minimum; deploying `./` is simplest
- **Port 3000** is the Next.js default -- must be declared in `ports` with `httpSupport: true` for Zerops L7 routing
- **Next.js binds 0.0.0.0** by default -- no extra host configuration needed
- **Build cache** should include `node_modules` for faster rebuilds
- **For static export** see the `nextjs-static` recipe instead (uses `static` base, no Node.js runtime)
