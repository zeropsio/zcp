# Qwik SSR on Zerops

Qwik with Node.js SSR. Requires Express adapter installation before deployment.

## Pre-deployment (REQUIRED)
```bash
npm run qwik add express
```

This creates:
- `adapters/express/vite.config.ts`
- `src/entry.express.tsx`
- package.json scripts: `build.server`, `serve`

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm run build  # Uses build.server from adapter
    run:
      start: pnpm serve  # Uses serve script from adapter
```

## Gotchas
- **npm run qwik add express** MUST be run before deploying to Zerops
- Without Express adapter, Qwik SSR will not work
- Package manager is pnpm (not npm) throughout
