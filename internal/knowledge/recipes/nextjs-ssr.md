# Next.js SSR on Zerops

Next.js with server-side rendering on Node.js. Not a static export.

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
        - .next
        - node_modules
        - package.json
    run:
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
      start: pnpm start
```

## Gotchas
- **Do NOT set `output: 'export'`** in next.config.mjs — that disables SSR and forces static export
- **Deploy `.next` + `node_modules` + `package.json`** — all three are required for SSR
- **Port 3000** is the default Next.js port — match it in `ports[]`
- **Bind 0.0.0.0** — Next.js binds `0.0.0.0` by default, no extra config needed
- **For static export** see `nextjs-static` recipe instead
