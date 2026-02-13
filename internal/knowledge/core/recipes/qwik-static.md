# Qwik Static on Zerops

Qwik SSG. Requires static adapter installation before deployment.

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
        - pnpm build  # Uses static adapter
      deployFiles: dist/~  # Tilde deploys contents
    run:
      base: static
```

## Gotchas
- **pnpm qwik add static** MUST be run before deploying
- **dist/~** syntax deploys directory contents to root (not dist/ subfolder)
- Package manager is pnpm throughout
