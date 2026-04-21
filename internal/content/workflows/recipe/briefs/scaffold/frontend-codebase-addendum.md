# frontend-codebase-addendum

You scaffold a frontend codebase — a single-page application built by a bundler-based framework (Vite, Next, Astro, Svelte, and equivalents). You write the SPA skeleton plus a single StatusPanel component; feature panels are out-of-scope.

## Files you write

- **Bundler / build config** (`vite.config.ts`, `next.config.js`, `astro.config.mjs`, or equivalent):
  - **Routable bind on the dev server** — `server.host = '0.0.0.0'`, not `localhost`. The public dev subdomain routes to the container's pod IP; loopback is unreachable from the L7 balancer.
  - **Routable bind on the preview server** too (when the framework exposes a preview mode with its own config key), with the same `0.0.0.0` host.
  - **Host-check allow-list** — the framework's dev-server host-check key (`server.allowedHosts` in Vite, `allowed-hosts` elsewhere) includes `.zerops.app` as a wildcard suffix so both the `{hostname}dev` and `{hostname}stage` public subdomains are accepted. A missed allow-list returns a plain-text "Blocked request / Invalid Host header" response with no HTML rendered.
  - **Proxy** — during local dev, proxy `/api` to the API's dev container for zcli-VPN developer workflows.

- **HTTP helper** (`src/lib/api.ts` or equivalent) — the single fetch wrapper every component calls. Reads `VITE_API_URL` (or framework equivalent), defaults to the empty string so the dev proxy works, enforces `res.ok` and `Content-Type: application/json` on every call, and throws with a descriptive message on failure. The API URL is baked at build time from the api-role hostname pair in `Hostnames` — the later-written zerops.yaml wires the var. Components call the helper, not `fetch()` directly.

- **App entry** (`src/App.svelte`, `app/page.tsx`, or equivalent) — mounts a single `StatusPanel` and nothing else. No routing, no multi-section layout with empty slots, no tabs, no nav. The outer wrapper carries a stable test-hook attribute (e.g. `data-feature="status"` matching the status feature's `uiTestId`) so the browser walk can locate it.

- **StatusPanel component** — polls `/api/status` every 5s via the HTTP helper (never `fetch()` directly), renders one row per managed service with a colored dot (`data-status` attribute exposes the state), and handles three render states explicitly: loading, error (visible banner using `data-error`), populated. Every row carries a `data-service="{name}"` hook.

- **Stylesheet** — a single modest sheet declaring CSS custom properties for theme plus selectors that later feature panels will attach to (`[data-feature]`, `[data-row]`, `[data-hit]`, `[data-file]`, `[data-result]`, `[data-status]`, `[data-processed-at]`, `[data-error]`). Declare them once here so the feature sub-agent inherits the UX tokens.

- **`.env.example`** — at minimum `VITE_API_URL=` (or the framework's equivalent) with a one-line comment explaining the value is baked at build time.

- **`.gitignore`** — `node_modules`, `dist`, `.env`, `.DS_Store`, plus the framework's cache directories (`.svelte-kit`, `.vite`, `.next`, `.astro`, and similar).

## Build output lives in a stripped root

Production deploys serve the bundler's output directory. Your scaffold emits the build output under `dist/` (or `build/`, `public/`, per framework); the later-written zerops.yaml points `deployFiles` at that directory with the tilde suffix so its contents become the served root.

## Files you do NOT write at this substep

- `README.md` — authored later at deploy.readmes; delete if the framework scaffolder created one.
- `zerops.yaml` — authored at a later substep after your scaffold returns.
- `.git/` — delete after the scaffolder runs (the `skip-git` contract rule).
- Any feature panel beyond StatusPanel; any cross-codebase shared types file (the feature sub-agent reconciles shared types); any CORS config (the API handles CORS).
