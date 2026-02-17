# SvelteKit SSR on Zerops

SvelteKit with Node.js adapter. Requires adapter-node configuration.

## Keywords
svelte, sveltekit, nodejs, ssr, adapter-node, javascript

## TL;DR
SvelteKit SSR with `@sveltejs/adapter-node` — deploy `build/` + `node_modules` + `package.json`.

## svelte.config.js (REQUIRED)
```javascript
import adapter from '@sveltejs/adapter-node';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter()
  }
};
```

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - pnpm i
        - pnpm run build
      deployFiles:
        - build/
        - package.json
        - node_modules
    run:
      start: pnpm start
```

## import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

## Gotchas
- **@sveltejs/adapter-node** required (not auto adapter)
- **@sveltejs/vite-plugin-svelte** required to avoid errors
- Deploy includes build/, package.json, node_modules
