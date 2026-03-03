# SvelteKit on Zerops

## Keywords
svelte, sveltekit, nodejs, ssr, static, ssg, adapter-node, adapter-static, javascript, typescript, vite

## TL;DR
SvelteKit on Zerops -- SSR with `@sveltejs/adapter-node` on Node.js or static export with `@sveltejs/adapter-static`.

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
      deployFiles:
        - build
        - package.json
        - node_modules
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
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

SvelteKit requires `@sveltejs/adapter-node` for Zerops deployment. The default auto adapter will not work.

```javascript
// svelte.config.js
import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter()
  }
};
```

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
      deployFiles: build/~
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
Two files must be configured for static export:

**svelte.config.js** -- must use adapter-static:
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

**src/routes/+layout.ts** -- must enable prerendering:
```typescript
export const prerender = true;
export const ssr = false;
```

## Gotchas
- **SSR: `@sveltejs/adapter-node` is required** -- the default auto adapter does not produce a Node.js server
- **SSR: `@sveltejs/vite-plugin-svelte` must be installed** -- required for `vitePreprocess()`
- **SSR: deploy `build/` + `package.json` + `node_modules`** -- all three are required
- **SSR: port 3000** is the adapter-node default -- declare in `ports` with `httpSupport: true`
- **SSR: binds 0.0.0.0** by default with adapter-node
- **Static: prerendering must be explicitly enabled** -- add `export const prerender = true;` in `src/routes/+layout.ts`
- **Static: adapter-static required** -- install and configure in `svelte.config.js`
- **Static: deploy `build/~`** -- tilde deploys directory contents to webroot
