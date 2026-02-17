# Next.js Static Export on Zerops

Next.js SSG requires explicit output configuration.

## Keywords
nextjs, next.js, static, ssg, react, export

## TL;DR
Next.js static export with `output: 'export'` in next.config.mjs — deploy `out/~` to `static` base.

## next.config.mjs (REQUIRED)
```javascript
export default {
  output: 'export'  // CRITICAL for static export
}
```

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
- **output: 'export'** in next.config.mjs is MANDATORY for static build
- Without this, Next.js attempts SSR (fails on static service)
- No server-side features available (getServerSideProps, API routes, ISR)
- Deploy `out/~` (tilde deploys contents, not folder)
