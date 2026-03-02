# SvelteKit Static on Zerops

SvelteKit static site generation (SSG) with adapter-static. Requires explicit prerender configuration.

## Keywords
svelte, sveltekit, static, ssg, adapter-static, javascript, vite

## TL;DR
SvelteKit static export with `@sveltejs/adapter-static` — requires `export const prerender = true` in the root layout and deploys `build/~` to a static service.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles: build/~
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
Two files must be configured for static export:

**svelte.config.js** — must use adapter-static:
```javascript
import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter()
  }
};
```

**src/routes/+layout.ts** — must enable prerendering:
```typescript
export const prerender = true;
```

## Gotchas
- **Prerendering must be explicitly enabled** — add `export const prerender = true;` in `src/routes/+layout.ts` or SvelteKit attempts SSR which fails on a static service
- **adapter-static required** — install `@sveltejs/adapter-static` and configure in `svelte.config.js`
- Deploy `build/~` (tilde deploys directory contents to webroot, not the `build/` folder itself)
- For SSR SvelteKit with Node.js runtime, use the `svelte-ssr` recipe instead
