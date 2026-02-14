# SvelteKit Static on Zerops

SvelteKit SSG. Requires adapter-static AND explicit prerender export.

## svelte.config.js (REQUIRED)
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

## src/routes/+layout.ts (REQUIRED)
```typescript
export const prerender = true;
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
      deployFiles: build/~  # Deploys contents
    run:
      base: static
```

## Gotchas
- **Prerendering** must be explicitly enabled in layout files
- **build/~** syntax deploys directory contents (not directory itself)
- Without prerender export, SvelteKit attempts SSR (fails on static service)
