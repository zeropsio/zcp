# SolidJS Static on Zerops

SolidJS static site built with Vite. Client-side only, no SSR.

## Keywords
solidjs, solid, static, ssg, vite, javascript, typescript

## TL;DR
SolidJS static site with Vite — build to `dist/` and deploy `dist/~` to a static service.

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
- **Deploy `dist/~`** (tilde deploys directory contents to webroot, not the `dist/` folder itself)
- **No `ports` or `start` needed** — the `static` base serves files on port 80 automatically
- **This is a client-side SPA** — uses `vite-plugin-solid` and plain Vite, not SolidStart
- **For SSR SolidStart** with Node.js runtime, use the `solidjs-nodejs` recipe instead
- **Build cache** should include `node_modules` for faster rebuilds
