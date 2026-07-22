# welcome-bridge-harness

Local end-to-end rig for the welcome panel's **agent auth-bridge**
(`docs/spec-welcome-mode.md §4`). It proves the full browser loop that
otherwise only the cloud Zerops GUI could exercise:

```
Authorize click → host gate (welcome.js) → broadcast postMessage to window.top
  → gui-harness receiver validates + acks → webview relays the ack up
  → host validates origin (isAllowedGuiOrigin) → the tile's phase line updates
```

A localhost page (`gui-harness.html`) stands in for the embedding Zerops GUI:
it embeds a **real** code-server in a full-height iframe, listens for the
broadcast trigger, validates it by the same contract the frontend receiver
pins, and (in `ack` mode) replies. `run.mjs` drives the whole thing with
puppeteer-core and asserts the observable outcome.

## What it proves vs. what it doesn't

- **Proves:** the trigger is credential-free (exactly `channel, version, type,
  agentType, eventId, createdAt`), the broadcast reaches an embedding page, an
  ack from a `localhost` origin is trusted host-side, and the tile advances to
  "Opening authorization in the Zerops panel…" (ack) or falls back to "Zerops
  dashboard not detected — use Terminal login" (silent, no receiver).
- **Does not cover:** the actual Zerops auth dialog opening, and the real
  frontend receiver's own validation — the frontend owns those with its own
  jest spec. This harness is a **test double** of that receiver.

## Prerequisites

- Google **Chrome** installed (puppeteer-core launches `channel: "chrome"`).
- A reachable **code-server** — e.g. the container's subdomain URL — plus its
  `VSCODE_PASSWORD`. On a Zerops container the password lives in the zembed
  store:

  ```sh
  ZCP_CS_URL=https://zcp-xxxx-8080.prg1.zerops.app
  ZCP_CS_PASSWORD=$(ssh zerops@zcp \
    "python3 -c \"import json;print(json.load(open('/etc/zerops-zembed/env.json'))['VSCODE_PASSWORD'])\"")
  ```

  The target agent must be **available + installed + not-yet-authorized** so
  its "Authorize in Zerops" button is visible (a hidden button is itself a
  finding the runner reports).

## Run

From the repo root:

```sh
ZCP_CS_URL=https://zcp-xxxx-8080.prg1.zerops.app ZCP_CS_PASSWORD=… make welcome-bridge-e2e
```

Environment / flags (all optional except the two URLs above):

| Var | Default | Meaning |
|---|---|---|
| `ZCP_CS_URL` | — (required) | code-server base URL |
| `ZCP_CS_PASSWORD` | — (required) | code-server access token / `VSCODE_PASSWORD` |
| `AGENT` | `claude-code` | agent row to authorize |
| `MODE` | `ack` | `ack` (receiver replies) or `silent` (never replies) |
| `HEADLESS` | `true` | `false` to watch the browser |

```sh
# watch the silent-fallback path against codex:
ZCP_CS_URL=… ZCP_CS_PASSWORD=… AGENT=codex MODE=silent HEADLESS=false make welcome-bridge-e2e
```

Exit code `0` = PASS, `1` = assertion failed / exception (a full-page
screenshot + the bridge log are written to `artifacts/`).

## Why localhost (not 127.0.0.1)

`welcome.js`'s `isAllowedGuiOrigin` trusts `http://localhost` on any port for
inbound acks, and the container nginx `frame-ancestors` allows
`http://localhost:*` to embed code-server — `127.0.0.1` matches neither. The
server binds loopback but the browser is pinned `localhost → 127.0.0.1` so the
page origin is exactly `http://localhost:<port>`.

The code-server auth cookie is `SameSite=None; Secure; **Partitioned**`, so a
first-party login does not carry into the localhost-embedded iframe; the runner
authenticates **inside** the iframe (its own partition).

## Contract mirror — keep both ends in sync

`gui-harness.html` mirrors the frontend receiver's validation, and `run.mjs`
hard-codes the two phase strings and the six payload keys. The authoritative
home is `docs/spec-welcome-mode.md §4` and the templates it pins
(`internal/content/templates/vscode-bootstrap-welcome.{js,html}`). If the
payload shape, channel/version, ack format, origin rules, or phase copy
change, update **both** ends. **No secrets ever live in this directory.**
