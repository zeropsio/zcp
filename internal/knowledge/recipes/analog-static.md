# Analog Static on Zerops

Angular static site generation via Analog framework deployed to a static service.

## Keywords
analog, angular, static, ssg, vite, javascript, typescript

## TL;DR
Analog Angular static export -- builds with pnpm on Node.js 20 and deploys `dist/analog/public/~` to a static service.

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
        - dist/analog/public/~
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

Analog produces static output under `dist/analog/public/` when configured for static site generation. The deploy path uses the tilde wildcard (`~`) to extract the directory contents directly into the webroot rather than deploying the `public/` folder itself.

## Gotchas
- **Deploy `dist/analog/public/~`** -- the tilde extracts contents to webroot, not the folder itself
- **Builds on Node.js but runs on static** -- no Node.js runtime at serve time
- **For SSR** use the `analog-nodejs` recipe with `nodejs` runtime instead of `static`
