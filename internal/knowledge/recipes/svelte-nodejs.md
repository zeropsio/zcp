# SvelteKit SSR on Zerops

SvelteKit with Node.js adapter. Requires adapter-node configuration.

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

## Gotchas
- **@sveltejs/adapter-node** required (not auto adapter)
- **@sveltejs/vite-plugin-svelte** required to avoid errors
- Deploy includes build/, package.json, node_modules
