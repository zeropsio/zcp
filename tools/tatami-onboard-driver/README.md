# tatami-onboard-driver

Drives the **fresh-registration onboarding flow on the tatami test cluster** in a
real browser and captures what the browser console cannot show: WebSocket
upgrade statuses and close codes, the exact payloads the Angular app *consumed*,
and every write to the identity blob in localStorage.

Built for the 2026-08-03 investigation
(`plans/tatami-onboarding-auth-2026-08-03/`), where it settled two questions: it
root-caused the **`/login` identity wipe** — a pre-claim `client-user/search`
seed returns the pool's placeholder row, which `app.effect.ts:537` reads 1–3 ms
before wiping `clientUserId` — and it showed the **PRD's reported WebSocket
failure did not reproduce** across 10 fresh runs (10/10 handshakes returned 101).

Sibling of `tools/onboard-ui-probe/`, which is puppeteer against
`localhost:1111` and never touches registration.

## Prerequisites

- **Node 24** (developed on v24.11.1) and `npm install` in this directory.
- Playwright's chromium build in the local cache
  (`~/Library/Caches/ms-playwright`); `npm install` fetches it if absent.
- Network access to `https://tatami.devel.zerops.dev` and `*.app-tatami.zerops.dev`.
- **Throwaway registrations are owner-sanctioned and unlimited.** Accounts are
  invented as `kh-test-<epoch>@example.com`; tatami registers instantly with no
  email verification. Credentials land in `<EVIDENCE_DIR>/account.json` so a
  control run can log back in.

## Entry points

### `driver.mjs` — the fresh-vs-control experiment

```sh
RUN=both node driver.mjs                       # register, drive the wizard, then the control path
RUN=fresh node driver.mjs                      # fresh registration only
ACCOUNT_EMAIL=… ACCOUNT_PASSWORD=… RUN=control STACK_ID=<zcp service-stack id> \
  PROBE=1 node driver.mjs                      # control path against an existing account
```

*Fresh path*: `/registration?zcp=true` → claim drain → wizard `picking` → pick an
agent → `authorizing` → auth dialog → terminal WebSocket.
*Control path*: reload → `/service-stack/<id>` → zagent chip → *Trigger
authorization process* → the same dialog. Falls back to
`/service-stack/<id>/terminal` if the chip is absent — that page mints the same
`file-browsing-access` token and opens the same `shell/stream` socket.

Produces `fresh-<ts>.json` / `control-<ts>.json` (full capture: every HTTP
request with headers, post data and response bodies for the interesting URLs;
every WebSocket with handshake request/response headers and status, frame
errors and close events; console at all levels; storage and cookie snapshots),
plus `summary-<ts>.json`, `run-<ts>.json`, and a screenshot per rendered
wizard/dialog state change.

| env | default | meaning |
|---|---|---|
| `RUN` | `both` | `fresh` \| `control` \| `both` |
| `HEADLESS` | `1` | `0` for a headed browser |
| `WATCH_MS` | `75000` | how long to sit in `authorizing` watching the WS retry loop |
| `AGENT` | `Claude Code` | which roster tile to pick |
| `STACK_ID` | *(derived)* | skip discovery of the zcp service-stack id |
| `ACCOUNT_EMAIL` / `ACCOUNT_PASSWORD` | *(new account)* | reuse an account instead of registering |
| `STOP_ON_CONNECT` | `1` | `0` keeps watching after the terminal connects — **required to capture the `bootstrap=` announce**, which lands ~17 s in |
| `H2_RETEST` | `1` | `0` disables the 60 s retry-without-reload test |
| `PROBE` | `0` | `1` runs commands in a connected terminal |
| `PROBE_CMDS_FILE` / `PROBE_CMDS` | *(defaults)* | JSON array of shell commands for `PROBE`; prefer the file, shells mangle nested quotes |
| `EVIDENCE_DIR` | `plans/tatami-onboarding-auth-2026-08-03/evidence` | output directory |
| `TATAMI_BASE` | `https://tatami.devel.zerops.dev` | cluster base URL |
| `SLOWMO` | `0` | Playwright `slowMo` ms |

On a fresh run that never connects, the driver automatically runs the **H2
re-test**: wait 60 s, dismiss the dialog and re-pick the agent *without a
reload*, which re-runs both the container resolve and the token mint and so
isolates container readiness from anything a reload changes. The outcome is
written into the capture as `H2 RETEST RESULT: …`.

### `loop.sh N` — N fresh registrations, one verdict line each

```sh
./loop.sh 5                     # WATCH_MS and EVIDENCE_ROOT overridable
```

Each run gets its own throwaway account and evidence subdirectory, and prints
the WS verdict, handshake statuses, close codes, containerId and stack id. Use
it for anything intermittent — the identity wipe reproduces at roughly **70%**,
so a single green run proves nothing.

### `wipe-probe.mjs` — the identity-wipe instrument

```sh
node wipe-probe.mjs                                              # fresh registration + claim
SMOKE=1 ACCOUNT_EMAIL=… ACCOUNT_PASSWORD=… node wipe-probe.mjs   # plumbing check, no wipe expected
```

Prints a correlated timeline interleaving every payload the app parsed with
every write to `@zerops/zerops/user-data`, flags the wiping write, and dumps the
healthy and wiping stacks side by side with their first divergent frame. A
healthy run shows a **single** full write; a wiped run shows **two** (full →
`{"userId":…}`) with a `zcp.pool@zerops.io` parse 1–3 ms before the second.
Honours `WATCH_MS`, `HEADLESS`, `EVIDENCE_DIR`, `TATAMI_BASE`, `STACK_ID`.

### `identity-probe.mjs` — lighter identity timeline

Storage writes, navigation, and the NgRx action stream if the bundle ships
StoreDevtools, then a reload to test whether the session self-heals. Superseded
by `wipe-probe.mjs` for wipe work; kept because it is smaller to reason about.

### `diff.mjs` — mechanical fresh-vs-control comparison

```sh
node diff.mjs <fresh.json> <control.json>
```

Side-by-side of the fields the investigation turns on: `file-browsing-access`
request and response, service-stack id, containerId, token charset and JWT
claims if any, WS handshake status and close code, storage keys, cookies,
every 4xx/5xx.

### `capture.mjs` / `identity-hooks.mjs` — the instrumentation libraries

`capture.mjs` attaches CDP (`Network`, `Page`, `Runtime`) to the page and to
auto-attached iframe and worker targets, and installs an in-page `WebSocket`
wrapper — CDP's `webSocketClosed` carries **no** close code, so the wrapper
supplies it.

`identity-hooks.mjs` installs, before any app script runs:

- **storage writes** to `@zerops/zerops/user-data` and `@zerops/zef/auth`, each
  with an uncapped stack (`Error.stackTraceLimit = 200`);
- **`JSON.parse` consumption** — every payload containing `roleCode`,
  `clientUserList` or `NO_ACTIVE_ACCOUNTS`, with the clientUser rows extracted
  (`id`, scalar `userId`, nested `user.id`, email, `_version`, `status`);
- **navigation** via `history.pushState` / `replaceState`, with stacks;
- **snack DOM** polling for the `No active accounts found` banner;
- a stubbed **Redux DevTools** bridge — dark on the tatami bundle, which ships
  no StoreDevtools, so the wire and parse hooks are the real channels.

## Method notes

**CDP timestamps and page-side timestamps are not comparable for ordering.** CDP
`Network.*` events reach Node over the CDP websocket; `exposeFunction` callbacks
arrive over the binding channel. The two have independent latencies, and during
this investigation comparing a `Network.responseReceived` time against a
storage-write time produced an ordering that was wrong by ~10 ms and **inverted
the conclusion** — it looked like the wipe preceded the payload that caused it.
Any cross-channel ordering claim must instead use the single-channel
`JSON.parse` hook, which timestamps consumption in the same stream as the writes.

**WebSocket upgrade status and close codes need CDP.** The console shows
neither. `Network.webSocketHandshakeResponseReceived` gives the upgrade status
(101 = the server accepted; 401/403 = it rejected), and the in-page wrapper
gives the close code. Chrome's *"WebSocket is closed before the connection is
established"* is the message for **the page** calling `close()` while
`CONNECTING`; a server rejection reads *"Unexpected response code: 401"*.

**The FE bridge lines are `console.debug`.** Capture every console level or they
are invisible. They are prefixed `[code-server bridge]`, and the `embed-ready`
announce carries `bootstrap=<version>`.

**Selector traps.** `getByRole('button', {name: /login/i})` matches **"Login
using Passkey"** first — match `/Login using email/i`. After an identity wipe a
re-login lands on the **organisation chooser**, not the dashboard. The
registration form exposes no per-control ids: use `input[name="email"]`,
`input[type="password"]`, and the two `input[type="text"]` in DOM order
(organisation, then name).

## Constraints

- **Never refresh the pool** — pool refreshes are owner-coordinated. Each
  registration claims a pool project, so report consumption after long loops.
- **Never print full tokens.** Truncate to `first6…last6` in anything logged or
  written. Decoded JWT claims are fine, but note the `file-browsing-access`
  token is a 22-character opaque base64url handle, not a JWT. Raw capture files
  under `plans/…/evidence/` do contain full tokens and are not committed.
- **Zerops search endpoints are ES-backed and lag writes.** A `client-user`
  search issued right after a pool claim can return the *pre-claim* row. That is
  not noise to work around — it is the bug this driver was built to catch, and
  the same caveat applies to any list/search assertion added later.
