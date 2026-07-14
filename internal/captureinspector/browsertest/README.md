# Capture Inspector browser smoke

This is an optional, test-only Playwright harness. It is outside the Go import
graph and is never embedded in or started by `zcp`.

## Deterministic Go fixture

```bash
cd internal/captureinspector/browsertest
npm ci
npm run install-browser
cd ../../..
go test -tags=captureinspector_browser \
  ./internal/captureinspector/internal/web \
  -run TestBrowserSmoke -count=1
```

The tagged Go test creates a finalized synthetic capture, starts the inspector on
a random loopback port, and passes its one-time URL to `smoke.cjs`.

## Existing capture root

Start the UI and pass the printed one-time URL directly:

```bash
go run ./cmd/zcp capture ui --root tmp/container-capture-runs --no-open
cd internal/captureinspector/browsertest
npm run smoke -- 'http://127.0.0.1:PORT/launch/CAPABILITY'
```

Set `ZCP_BROWSER_OUTPUT` to select a screenshot directory. Screenshots are
reveal-gated plaintext artifacts and must not be committed. Set
`ZCP_PLAYWRIGHT_MODULE` only when using an already-installed Playwright module
outside this directory.

The smoke covers capability removal from the current URL, pre-reveal raw denial,
Cards/Flow/Split, keyboard edge selection, formatted context detail, single
inspector/drawer ownership, strict-CSP inline-style absence, browser errors, and
1024/2560 px overflow.
