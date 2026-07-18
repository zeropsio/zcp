# Data Console SPA — DOM test harness

jsdom-backed tests for `../dist/app.js` (the DOM main) and its `dc-*.js`
modules. Loads the real `../dist/index.html` + `../dist/*.js` — same markup,
same script order (parsed out of `index.html`, never hand-copied) — into a
fresh jsdom window per scenario, and drives it exactly as a browser or the
VS Code extension host would: a URL fragment or a `dataconsole-init`
postMessage to boot, real `click` events on rendered elements to navigate,
and a stubbed `fetch` / postMessage `dc-rpc` broker for the network.

This is test-only tooling, outside the Go import graph and never embedded in
or served by `zcp` (`../embed.go` only embeds `../dist`).

## Run locally

```bash
cd internal/dataconsole/console/webui
npm ci
node domtest/xss.dom.test.js
node domtest/capability-gating.dom.test.js
node domtest/readonly-posture.dom.test.js
```

Or via Go (same skip-clean behavior as CI):

```bash
go test ./internal/dataconsole/console/webui/... -run TestDataConsoleSPADOM -v
```

`go test` (without `-short`) skips this suite cleanly if `node` is missing
from PATH or `npm ci` hasn't been run in this directory yet (no
`node_modules/jsdom`) — see `../spa_domtest_test.go`. It never hard-fails a
node-less or npm-ci-less environment.

## Files

- `harness.js` — `buildConsole()` + helpers (`waitFor`, `click`, `jsonRoute`,
  `blobRoute`, `hostPostMessage`). Reusable across test files; not a test
  itself (excluded from the `*.dom.test.js` glob both npm and the Go runner
  use).
- `*.dom.test.js` — one scenario group per file, run directly via `node
  <file>` (plain `assert`, no test-runner framework — matches the existing
  `../spa/*.test.js` style). Each prints `<file> OK` and exits 0 on success,
  or throws/exits non-zero on the first failing assertion.

## What's pinned here (survives the S15 renderer rewrite)

- `xss.dom.test.js` — untrusted data (`<script>`, `"`, `&`, `<img onerror>`)
  is HTML-safe wherever the SPA renders it: service hostname, tree/table
  node name, grid column name, grid cell value, blob name.
- `capability-gating.dom.test.js` — a mutating control renders only when the
  server-declared action is `enabled` AND the session is embedded +
  write-enabled; either half missing hides the control.
- `readonly-posture.dom.test.js` — standalone always shows the read-only
  badge and hides the write toggle; embedded always shows the toggle
  (independent of `writeEnabled`, which only drives its checked state).

These are behavior invariants from `plans/dataconsole-contracts-draft-2026-07-16.md`,
not characterizations of today's exact markup — S15 may change IDs/structure
as long as the invariant holds; update the selectors here, not the assertion.

## Known gap

CI (`.github/workflows/ci.yml`) does not yet run `npm ci` for this package,
so `TestDataConsoleSPADOM` currently skips in CI (no `node_modules/jsdom`).
Wiring a `setup-node` + `npm ci` step is a follow-up (DD-7 also calls for a
pinned Node version in CI) — out of scope for the harness slice that added
this suite.
