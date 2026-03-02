# Qwik Static on Zerops

Qwik static site generation (SSG). Requires the static adapter to be installed before deployment.

## Keywords
qwik, static, ssg, resumable, javascript, vite

## TL;DR
Qwik static export using `pnpm qwik add static` adapter — deploys `dist/~` and `public` to a static service.

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
        - public
        - dist/~
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

## Configuration
The Qwik static adapter must be added to the project before deploying:
```bash
pnpm qwik add static
```
This creates `adapters/static/vite.config.ts` and adds the `build.server` script to `package.json`. Both are required for the static build to work.

## Gotchas
- **`pnpm qwik add static` MUST be run** in the project before deploying — without it, the build produces a server bundle instead of static files
- Deploy both `public` (static assets) and `dist/~` (tilde deploys dist contents to webroot, not the `dist/` folder itself)
- Package manager is pnpm — use pnpm in all build commands
- For SSR Qwik with Node.js runtime, use the `qwik-nodejs` recipe instead
