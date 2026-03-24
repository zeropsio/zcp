# SvelteKit on Zerops

SSR with `@sveltejs/adapter-node` on Node.js, or static export with `@sveltejs/adapter-static`.

## Keywords
svelte, sveltekit, adapter-node, adapter-static, kit

## TL;DR
SSR requires `@sveltejs/adapter-node` (auto adapter does not produce a Node.js server). Deploy `build/`, `package.json`, and `node_modules` — runtime does NOT run `npm install`. Static: `build/~`.

## SSR (Node.js runtime)

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - build
        - package.json
        - node_modules
      cache: node_modules
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node build
      healthCheck:
        httpGet:
          port: 3000
          path: /
```

### import.yml
```yaml
services:
  - hostname: app
    type: nodejs@22
    enableSubdomainAccess: true
```

Requires `@sveltejs/adapter-node` in `svelte.config.js`. The default auto adapter does not produce a standalone Node.js server.

## Static Export

### zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
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

Requires `@sveltejs/adapter-static` in `svelte.config.js` and `export const prerender = true` in `src/routes/+layout.ts`.

## Gotchas
- **SSR: `@sveltejs/adapter-node` is required** — the default auto adapter does not produce a Node.js server
- **SSR: deploy `build/` + `package.json` + `node_modules`** — runtime does NOT run `npm install`; all three required
- **SSR: start command is `node build`** — not `npm start`; adapter-node output is a directory, not a script
- **SSR: port 3000** — adapter-node default; declare in `ports` with `httpSupport: true`
- **Static: `export const prerender = true`** must be in `src/routes/+layout.ts` — prerendering is not automatic
- **Static: adapter-static required** — configure in `svelte.config.js`
- **Static: deploy `build/~`** — tilde deploys directory contents to webroot
