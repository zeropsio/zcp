# Nuxt Static on Zerops

Nuxt 3 static site generation (SSG) deployed to a static service. Uses `nuxi generate` for pre-rendering.

## Keywords
nuxt, vue, static, ssg, nitro, javascript, typescript

## TL;DR
Nuxt 3 static export with `nuxi generate` -- builds on Node.js 20 and deploys `.output/public/~` to a static service.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - yarn
        - yarn nuxi generate
      deployFiles:
        - .output/public/~
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

Nuxt generates static HTML into `.output/public/` when using `nuxi generate`. The deploy path `.output/public/~` uses the Zerops tilde wildcard to extract the directory contents directly into the webroot.

No special configuration is needed in `nuxt.config.ts` -- `nuxi generate` handles static pre-rendering automatically.

## Gotchas
- **Use `nuxi generate`** not `nuxt build` -- `nuxt build` produces an SSR server, `nuxi generate` produces static HTML
- **Deploy `.output/public/~`** -- the tilde extracts contents to webroot, not the folder itself
- **No server-side features** at runtime -- API routes, server middleware, and SSR are not available in static mode
- **For SSR Nuxt** use the `nuxt` recipe with `nodejs` runtime instead of `static`
