## SPA static runtime — `base: static` (Nginx)

A single-page app that compiles to a static `dist/` (Vite, Webpack,
Rollup, esbuild) ships on `base: static` — Zerops' Nginx-backed
runtime. Not on `nodejs@22 + npx serve`.

Wrong shape (what run-19 shipped, fail validation):

```yaml
zerops:
  - setup: appstage
    build:
      base: nodejs@22
      buildCommands:
        - npm ci
        - npm run build
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: npx --no-install serve -s dist -l 3000   # WRONG
```

Right shape:

```yaml
zerops:
  - setup: appstage
    build:
      base: nodejs@22                # build container is nodejs (Vite needs node)
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist
    run:
      base: static                    # runtime is Nginx, no node needed
      documentRoot: dist              # Nginx serves dist/
      # No `ports:` — `base: static` defaults to 80 + httpSupport
      # No `start:` — Nginx handles process supervision
```

Why this is the right shape:

- **No node runtime needed at runtime.** The bundle is static
  HTML/CSS/JS. Running `npx serve` on `nodejs@22` wastes ~80 MB RAM
  per replica vs Nginx's ~2 MB.
- **Nginx handles SPA fallback** automatically when `documentRoot`
  is set — no `-s` flag required, no port arg.
- **Subdomain L7 routing** works the same on `base: static` as on
  any other runtime; `enableSubdomainAccess: true` in the tier
  import.yaml still applies.

## Build-time-baked URLs — pair with the workspace project envs

Vite + friends inline `import.meta.env.VITE_*` constants at build
time. The bundle ships with whatever value was in the env at
`vite build` time. Reading `${apistage_zeropsSubdomain}` directly is
the run-19 trap: that alias resolves only after apistage's first
deploy.

Correct pattern uses the workspace project envs taught in
`cross-service-urls.md`:

```yaml
build:
  envVariables:
    VITE_API_URL: ${STAGE_API_URL}      # resolves at provision time
```

Where `STAGE_API_URL` was set on the project at provision via:

```bash
zerops_env project=true action=set \
  STAGE_API_URL="https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app"
```

The build container reads `STAGE_API_URL` (a real URL by then),
substitutes into the bundle, ships compiled JS that fetches the
right origin. No ordering dance, no deferred deploy.

## Dev variant

The dev codebase still runs Vite's dev server (HMR, browser-walk
during scaffold) — that's `nodejs@22` + `zsc noop --silent` + agent-
owned `zerops_dev_server` per `dev-loop.md`. Only the **stage**
setup flips to `base: static`. Two setup blocks in the same yaml.

## When NOT to use `base: static`

SSR frameworks (Next.js, Nuxt, SvelteKit-SSR, Remix) are NOT static
SPAs — they need a node runtime at request time for the SSR pass.
Those stay on `base: nodejs@22` with `start: <SSR entry>`. The rule
of thumb: if `npm run build` produces only static assets in `dist/`
or `build/` with no node entry script, it's an SPA → `base: static`.
If the build produces a server entry (`server/index.js`, `.output/server`,
etc.), it's SSR → `base: nodejs@22`.
