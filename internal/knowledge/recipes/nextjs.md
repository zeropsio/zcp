# Next.js on Zerops

## Keywords
nextjs, next.js, react, ssr, static, ssg, export, typescript, javascript

## TL;DR
Next.js on Zerops -- SSR on Node.js runtime or static export to static service.

## SSR (Node.js runtime)

### zerops.yml
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
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

### import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

### Configuration
Do NOT set `output: 'export'` in next.config.mjs -- that disables SSR. Deploy `./` (entire workspace) since Next.js SSR requires `.next/`, `node_modules/`, and `package.json`. Next.js binds 0.0.0.0 by default.

## Static Export

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm build
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

### Configuration
The project must have `output: 'export'` set in `next.config.mjs` (or `next.config.js`):
```javascript
export default {
  output: 'export'
}
```
Without this setting, Next.js defaults to SSR mode which requires a Node.js runtime and will fail on a static service.

## Gotchas
- **SSR: deploy `./` (entire workspace)** -- `.next/`, `node_modules/`, and `package.json` are all required
- **SSR: port 3000** is the Next.js default -- declare in `ports` with `httpSupport: true`
- **Static: `output: 'export'`** in next.config is MANDATORY -- without it, Next.js attempts SSR which fails on a static service
- **Static: no server-side features** -- getServerSideProps, API routes, ISR, and middleware are incompatible with static export
- **Static: deploy `out/~`** -- tilde deploys directory contents to webroot
- **Build cache** should include `node_modules` for faster rebuilds
