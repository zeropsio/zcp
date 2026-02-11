# Static on Zerops

## Keywords
static, static hosting, spa, single page app, cdn, html, css, js, document root, prerender, routing

## TL;DR
Static is a simplified Nginx wrapper on Zerops with YAML-based routing configuration, SPA support out-of-the-box, and CDN integration — use it for frontend apps, switch to Nginx for custom server config.

## Zerops-Specific Behavior
- Base: Alpine + Nginx (pre-configured)
- Default routing: `$uri` → `$uri.html` → `$uri/index.html` → `/index.html` → 404
- Configuration: YAML-based in `zerops.yaml` (no nginx.conf)
- Prerender.io: Built-in via `PRERENDER_TOKEN` env var
- CORS: Configurable
- Custom headers: Configurable
- Redirects: Relative, absolute, wildcard with status codes and path/query preservation
- CDN: Direct integration with Zerops CDN

## Configuration
```yaml
zerops:
  - setup: web
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i && pnpm build
      deployFiles:
        - dist/~
      cache:
        - node_modules
    run:
      base: static
```

### With Routing Rules
```yaml
run:
  routing:
    redirects:
      - from: /old-page
        to: /new-page
        status: 301
    cors:
      allowOrigin: "*"
      allowMethods: "GET, POST"
    headers:
      - path: /*
        name: X-Frame-Options
        value: DENY
```

## Matching Priority
1. Exact path match
2. Simple path match
3. Pattern (wildcard) match

## SSG Deployment Pattern

Static service is the **run target** for SSG (Static Site Generation) builds. Build on `nodejs@22`, deploy to `static`:

```yaml
zerops:
  - setup: app
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i && pnpm build
      deployFiles:
        - dist/~            # tilde = deploy contents, not the folder
      cache:
        - node_modules
    run:
      base: static
```

## Framework Build Outputs

| Framework | Build command | Output dir | deployFiles |
|-----------|-------------|-----------|-------------|
| React (Vite) | `pnpm build` | `dist/` | `dist/~` |
| Vue (Vite) | `pnpm build` | `dist/` | `dist/~` |
| Solid (Vite) | `pnpm build` | `dist/` | `dist/~` |
| Angular | `pnpm build` | `dist/app/browser/` | `dist/app/browser/~` |
| Next.js (export) | `pnpm build` | `out/` | `out/~` |
| Nuxt (generate) | `pnpm generate` | `.output/public/` | `.output/public/~` |
| SvelteKit | `pnpm build` | `build/` | `build/~` |
| Astro | `pnpm build` | `dist/` | `dist/~` |
| Remix | `pnpm build` | `build/client/` | `build/client/~` |

**Note:** The `~` tilde suffix extracts directory contents into the service root. Without it, files are nested inside the directory name.

## Framework-Specific Requirements

| Framework | Extra requirement |
|-----------|-----------------|
| Next.js | `output: 'export'` in `next.config.mjs` |
| SvelteKit | `@sveltejs/adapter-static` + `export const prerender = true` in `+layout.js` |
| Nuxt | Use `nuxi generate` (not `nuxi build`) |
| Astro | Default is static — no extra config needed |

## Gotchas
1. **SPA works by default**: No configuration needed for React/Vue/Angular — `/index.html` fallback is automatic
2. **No custom nginx.conf**: For advanced server config, use the Nginx service instead
3. **CDN wildcard domains not supported**: `*.domain.com` doesn't work with static CDN
4. **Prerender needs `PRERENDER_TOKEN`**: SEO pre-rendering won't activate without this env var
5. **Tilde syntax required for SSG**: Use `dist/~` not `dist` — without tilde, files are nested in a subdirectory
6. **Next.js needs `output: 'export'`**: Without this config, `next build` produces SSR output incompatible with static service

## See Also
- zerops://services/nginx
- zerops://services/nodejs
- zerops://platform/cdn
- zerops://config/zerops-yml
- zerops://config/deploy-patterns
