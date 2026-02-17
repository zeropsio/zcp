# Qwik Static on Zerops

Qwik SSG. Requires static adapter installation before deployment.

## Keywords
qwik, static, ssg, resumable, javascript

## TL;DR
Qwik static with `pnpm qwik add static` — deploy `dist/~` to `static` base.

## Pre-deployment (REQUIRED)
```bash
pnpm qwik add static
```

This creates:
- `adapters/static/vite.config.ts`
- package.json scripts: `build.server`

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
- **pnpm qwik add static** MUST be run before deploying
- **dist/~** syntax deploys directory contents to root (not dist/ subfolder)
- Package manager is pnpm throughout
