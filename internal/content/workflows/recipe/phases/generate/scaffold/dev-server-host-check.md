# Dev-server host-check allow-list

When the framework's dev server enforces an HTTP `Host`-header allow-list (most modern bundler-based dev servers do), the Zerops public dev subdomain must appear in that allow-list or the dev server returns `Blocked request` / `Invalid Host header` to the browser.

This is a framework-config concern — the key name lives in the framework's dev-server config and varies by framework (`allowedHosts`, `allowed-hosts`, `disable-host-check`, and similar). During research, look up the current host-check config name for the framework's dev server in its official docs. Bake the correct setting into the config file the dev server reads (`vite.config.ts`, `webpack.config.js`, `angular.json`, `next.config.js`, or the framework's equivalent) at scaffold time.

Add `.zerops.app` as a wildcard suffix so both the `{hostname}dev-{zeropsSubdomainHost}-{port}.prg1.zerops.app` URL and the `{hostname}stage-{zeropsSubdomainHost}.prg1.zerops.app` URL are accepted without per-URL churn. If the framework's preview mode has a separate host-check (some bundlers do), configure both modes. The symptom of a missing entry is a 403 or plain-text `Blocked request` response with no HTML rendered.
