# Static on Zerops

## Keywords
static, ssg, spa, react, vue, angular, svelte, nextjs, nuxt, astro, vite, zerops.yml

## TL;DR
Static service serves pre-built HTML/CSS/JS. Build with `nodejs@22`, run with `static`. Use tilde (`~`) in `deployFiles` for correct webroot. No start command needed.

### Build != Run

Build `nodejs@22`, run `static`.

### Build Procedure

1. Set `build.base: nodejs@22`
2. `buildCommands`: framework build command
3. `deployFiles: dist/~` (tilde MANDATORY for correct root)
4. No `run.start` needed, no port config (serves on 80 internally)

### SPA Fallback

Automatic ($uri -> $uri.html -> $uri/index.html -> /index.html -> 404).

### Framework Output Directories

- React/Vue: `dist/~`
- Angular: `dist/app/browser/~`
- Next.js export: `out/~`
- Nuxt generate: `.output/public/~`
- SvelteKit: `build/~`
- Astro: `dist/~`
- Remix: `build/client/~`
