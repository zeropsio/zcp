# Nuxt on Zerops

Nuxt 3 SSR with node-server preset on Node.js runtime.

## Keywords
nuxt, vue, nodejs, ssr, nitro, node-server, javascript, typescript

## TL;DR
Nuxt 3 SSR with `NITRO_PRESET=node-server` — deploy `.output/~` and start with `node server/index.mjs`.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@20
      buildCommands:
        - yarn
        - yarn build
      deployFiles:
        - .output/~
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
      start: node server/index.mjs
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

Nuxt uses Nitro under the hood. The default Nitro preset for Zerops is `node-server`, which produces a standalone Node.js server. No explicit preset configuration is needed in `nuxt.config.ts` -- Nuxt auto-detects the correct preset during build.

The deploy path `.output/~` uses the Zerops tilde wildcard to extract the contents of `.output/` directly into `/var/www/`, so the start command references `server/index.mjs` (not `.output/server/index.mjs`).

## Gotchas

- **deployFiles is for stage/production** — this recipe shows the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy. With `[.]`, build output stays in its original directory under `/var/www/` — adjust `start` path accordingly (see Deploy Semantics in platform reference).
- **Deploy `.output/~`** -- the tilde extracts contents to `/var/www/` so start path is `server/index.mjs` not `.output/server/index.mjs`
- **Port 3000** is the default Nuxt/Nitro port -- must be declared in `ports` with `httpSupport: true`
- **Nuxt binds 0.0.0.0** by default in SSR mode -- no extra host configuration needed
- **Do NOT use `nuxt generate`** for SSR -- that produces static HTML; use `nuxt build` (or `yarn build`) for SSR mode
- **For static pre-rendered sites** use `nuxt generate` with `static` base instead of `nodejs` runtime
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
