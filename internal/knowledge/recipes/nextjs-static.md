# Next.js Static Export on Zerops

Next.js static site generation (SSG) deployed to a static service. Requires `output: 'export'` in next.config.

## Keywords
nextjs, next.js, static, ssg, react, export, javascript

## TL;DR
Next.js static export with `output: 'export'` in next.config — builds on Node.js 20 and deploys `out/~` to a static service.

## zerops.yml
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

## import.yml
```yaml
services:
  - hostname: app
    type: static
    enableSubdomainAccess: true
```

## Configuration
The project must have `output: 'export'` set in `next.config.mjs` (or `next.config.js`):
```javascript
export default {
  output: 'export'
}
```
Without this setting, Next.js defaults to SSR mode which requires a Node.js runtime and will fail on a static service.

## Gotchas
- **output: 'export'** in next.config is MANDATORY for static build — without it, Next.js attempts SSR which fails on a static service
- **No server-side features** available: getServerSideProps, API routes, ISR, and middleware are all incompatible with static export
- Deploy `out/~` (tilde deploys directory contents to webroot, not the `out/` folder itself)
- For SSR Next.js, use the `nextjs-ssr` recipe with `nodejs` runtime instead
