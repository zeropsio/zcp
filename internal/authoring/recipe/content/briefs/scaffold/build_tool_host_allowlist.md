## Build-tool host-allowlist (Vite / Webpack / Rollup)

Modern bundler dev servers reject requests whose `Host` header is not
in their allowlist. Zerops dev / stage subdomains are dynamic; the
default allowlist (`localhost`, `127.0.0.1`) does not include them, so
the first browser-walk against the dev URL hits a `Blocked request.
This host is not allowed.` page.

Set the bundler's host-allowlist knob explicitly at scaffold time:

- **Vite**: `server.allowedHosts: true` in `vite.config.{js,ts}`
  (preview server uses `preview.allowedHosts: true` for cross-deploy
  stage builds).
- **Webpack-Dev-Server**: `devServer.allowedHosts: 'all'`.
- **Rollup-based dev servers**: follow the equivalent knob.

This is the bundler's intended extension point for hosted dev
environments; setting it once in the config covers every Zerops
subdomain the porter's project provisions. No per-tier override
needed.

The trap recurred in three consecutive run-12, run-12, run-13
dogfoods; the framing matters — it's a positive Vite/Webpack config
knob, not a Zerops-side workaround.
