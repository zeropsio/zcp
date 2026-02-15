# Nuxt on Zerops

Nuxt 3 with two deployment modes: SSR (server) and static (pre-rendered).

## SSR Mode (zerops.yml)
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      envVariables:
        SERVER_PRESET: node-server
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles: .output/~
    run:
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
      start: node .output/server/index.mjs
```

## Static Mode (zerops.yml)
```yaml
zerops:
  - setup: web
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm generate
      deployFiles: .output/public/~
    run:
      base: static
```

## Gotchas
- **SSR uses `SERVER_PRESET: node-server`** — builds a standalone Node.js server
- **Deploy `.output/~`** for SSR — tilde extracts contents to `/var/www/`
- **Static uses `pnpm generate`** not `pnpm build` — generates pre-rendered HTML
- **Static deploys to `static` base** — no Node.js runtime needed
- **Bind 0.0.0.0** — Nuxt binds `0.0.0.0` by default in SSR mode
- **Port 3000** is the default Nuxt port — match it in `ports[]`
