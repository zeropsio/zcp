# Astro Static Site on Zerops

Astro static site generation deployed to a static service. Default Astro behavior without an SSR adapter.

## Keywords
astro, static, ssg, javascript, typescript

## TL;DR
Astro static build (default output mode) — builds on Node.js 20 and deploys `dist/~` to a static service. No adapter needed.

## zerops.yml
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

## import.yml
```yaml
services:
  - hostname: app
    type: static
    enableSubdomainAccess: true
```

## Gotchas
- **No adapter needed** — Astro defaults to static output; do NOT install `@astrojs/node` or set `output: "server"` unless you want SSR
- **Deploy `dist/~`** — tilde deploys directory contents to webroot, not the `dist/` folder itself
- **Builds on Node.js, runs on static** — Node.js is only used at build time; the runtime is a lightweight static file server
- **No server-side features** — API routes, server-side middleware, and dynamic SSR are not available in static mode
- For SSR Astro with server-side features, use the `astro-nodejs` recipe with Node.js runtime instead
