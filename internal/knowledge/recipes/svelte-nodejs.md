# SvelteKit SSR on Zerops

SvelteKit with `@sveltejs/adapter-node` for Node.js server-side rendering.

## Keywords
svelte, sveltekit, nodejs, ssr, adapter-node, javascript, typescript

## TL;DR
SvelteKit SSR with `@sveltejs/adapter-node` — deploy `build/`, `node_modules`, and `package.json`, port 3000.

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

## import.yml
```yaml
services:
  - hostname: app
    type: nodejs@20
    enableSubdomainAccess: true
```

## Configuration

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

## Gotchas

- **deployFiles is for stage/production** — this recipe shows the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy. With `[.]`, build output stays in its original directory under `/var/www/` — adjust `start` path accordingly (see Deploy Semantics in platform reference).
- **`@sveltejs/adapter-node` is required** -- the default auto adapter does not produce a Node.js server; install and configure it in `svelte.config.js`
- **`@sveltejs/vite-plugin-svelte` must be installed** -- required for `vitePreprocess()` to avoid build errors
- **Port 3000** is the adapter-node default -- must be declared in `ports` with `httpSupport: true`
- **Deploy `build/` + `package.json` + `node_modules`** -- all three are required; `build/` alone is not sufficient
- **SvelteKit binds 0.0.0.0** by default with adapter-node -- no extra host configuration needed
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
