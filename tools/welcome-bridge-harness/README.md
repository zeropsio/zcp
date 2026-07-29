# welcome-bridge-harness

Local end-to-end rig for the welcome panel's bridge (`docs/spec-welcome-mode.md
§4`): both the shipped **auth-trigger flow** (§4.2) and the **embed command
channel** (§4.3, the onboarding handshake: `embed-ready` → `set-mode` →
`launch-agent` → `agent-ready`/`launch-failed`). It proves the full browser
loop that otherwise only the cloud Zerops GUI could exercise.

§4.2 (`MODE=ack|silent`):

```
Authorize click → host gate (welcome.js) → broadcast postMessage to window.top
  → gui-harness receiver validates + acks → webview relays the ack up
  → host validates origin (isAllowedGuiOrigin) → the tile's phase line updates
```

§4.3 (`MODE=launch` and the rest of the scenario matrix below): no click at
all — `gui-harness.html`'s own embed-ready handler drives `set-mode` /
`launch-agent` straight back at the webview the instant it announces, and
`run.mjs` asserts the resulting outcome correlates by the command's `eventId`.

A localhost page (`gui-harness.html`) stands in for the embedding Zerops GUI:
it embeds an **embed** — either a **real** code-server (LIVE) or a locally
generated stub double (SELFTEST, see below) — in a full-height iframe, and
speaks both bridge halves exactly as the frontend receiver does. `run.mjs`
drives the whole thing with puppeteer-core and asserts the observable outcome.

## What it proves vs. what it doesn't

- **Proves:** the §4.2 trigger is credential-free (exactly `channel, version,
  type, agentType, eventId, createdAt`); an ack from a `localhost` origin is
  trusted host-side; the tile advances to "Finish signing in via the Zerops
  dialog…" (ack) or falls back to "Zerops dashboard not detected — reload the
  Zerops page" (silent, no receiver). For §4.3: the embed announces once per
  init (ordered `agents` + `bootstrapVersion`, no `installed` axis) and
  re-announces on reload; `set-mode` reaches the embed and is honored;
  `launch-agent` for a known agent yields a correlated `agent-ready`, for an
  unknown one a pre-dispatch `launch-failed "unknown-agent"`; a duplicate
  `eventId` is idempotently re-acked (same agentId) or dropped as malformed
  (different agentId); an embedder that never sends a directive gets the
  container's own default presentation after the no-directive window.
- **Does not cover:** the actual Zerops auth dialog opening, the FE's own
  wizard-state logic, and the real webview/host's own validation — those are
  owned by the frontend's jest spec and `internal/content/welcomejs/`'s own
  suite (`command_channel.test.js`, `launch_gate.test.js`,
  `receiver_lifecycle.test.js`). This harness is a **test double** of the GUI
  receiver, never a security boundary.

## Two ways to run this

### LIVE — against a real code-server

Requires `ZCP_CS_URL` + `ZCP_CS_PASSWORD` and drives a real embedded workbench
via the command palette. This is the rig the `/flow` ASSEMBLE step runs; it is
**not** run automatically — see "Live invocations" below for the exact command
per scenario.

### SELFTEST — deterministic, no live rig, no secrets

`SELFTEST_CONTRACT=old|new` skips `ZCP_CS_URL`/`ZCP_CS_PASSWORD` entirely and
generates a local stub "embed" double (`run.mjs`'s `buildStubEmbedHtml`,
written to `artifacts/` — gitignored, never a tracked file) that plays the
code-server + welcome webview's role:

- `SELFTEST_CONTRACT=old` mirrors the **pre-S1** embed: it speaks nothing at
  all on the bridge channel (no announce, no set-mode/launch-agent handling,
  no fallback). Every scenario below is expected to **FAIL** against it — this
  is the RED half of the harness's own contract-test proof.
- `SELFTEST_CONTRACT=new` mirrors the landed §4.3 command channel (announce,
  set-mode → presentation toggle, launch-agent → idempotent
  agent-ready/launch-failed, no-directive fallback). Every scenario is
  expected to **PASS** against it.

Every scenario's driver and assertion is written **once** in `run.mjs`/
`gui-harness.html` and reused for both LIVE and SELFTEST — only the embed
(real vs. generated stub) and the entry mechanics differ.

```sh
cd tools/welcome-bridge-harness && npm install --silent
SELFTEST_CONTRACT=new MODE=launch node run.mjs   # exit 0 (PASS)
SELFTEST_CONTRACT=old MODE=launch node run.mjs   # exit 1 (FAIL) — the RED proof
```

`make welcome-bridge-selftest` runs the full `contract=new` battery (every
scenario, both `set-mode` directives) in one shot — no secrets, no live rig,
safe to run anywhere Chrome is installed.

## Prerequisites

- Google **Chrome** installed (puppeteer-core launches `channel: "chrome"`) —
  needed for BOTH live and selftest runs.
- LIVE only: a reachable **code-server** — e.g. the container's subdomain URL
  — plus its `VSCODE_PASSWORD`. On a Zerops container the password lives in
  the zembed store:

  ```sh
  ZCP_CS_URL=https://zcp-xxxx-8080.prg1.zerops.app
  ZCP_CS_PASSWORD=$(ssh zerops@zcp \
    "python3 -c \"import json;print(json.load(open('/etc/zerops-zembed/env.json'))['VSCODE_PASSWORD'])\"")
  ```

  For `MODE=ack|silent` the target agent must be **available + installed +
  not-yet-authorized** so its "Authorize in Zerops" button is visible (a
  hidden button is itself a finding the runner reports).

## Environment / flags

| Var | Default | Meaning |
|---|---|---|
| `ZCP_CS_URL` | — (required unless `SELFTEST_CONTRACT` set) | code-server base URL |
| `ZCP_CS_PASSWORD` | — (required unless `SELFTEST_CONTRACT` set) | code-server access token / `VSCODE_PASSWORD` |
| `SELFTEST_CONTRACT` | unset (LIVE) | `old` \| `new` — run the deterministic contract test instead of the live rig |
| `AGENT` | `claude-code` | agent row to authorize / launch |
| `MODE` | `ack` | see the scenario matrix below |
| `DIRECTIVE` | `onboarding` | `MODE=set-mode` only: which mode the driver sends |
| `HEADLESS` | `true` | `false` to watch the browser (LIVE only, in practice) |
| `HARNESS_ACK_DELAY_MS` | `0` | `MODE=ack` only: delay before the receiver's accepted ack |
| `HARNESS_CLOCK_SKEW_MS` | `0` | receiver clock skew simulation (§4.1 freshness tolerance) |

## Scenario matrix

Each row is a `MODE` (± `DIRECTIVE`); "Live invocation" is the exact command
the `/flow` ASSEMBLE step runs against a real container (documented here, not
executed by this harness's own author — see
`plans/agent-first-onboarding-2026-07-28/briefs/S7-e2e-harness.md`).

| # | MODE | Proves | Selftest | Live invocation |
|---|---|---|---|---|
| 1 | `reload` | `embed-ready` on init (ordered agents + `bootstrapVersion`, no `installed`); a forced embed reload re-announces | `SELFTEST_CONTRACT=new MODE=reload node run.mjs` | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… MODE=reload node run.mjs` |
| 2 | `set-mode` (`DIRECTIVE=standard\|onboarding`) | `standard` reveals the presentation; `onboarding` stays dark | `SELFTEST_CONTRACT=new MODE=set-mode DIRECTIVE=standard node run.mjs` (+ `DIRECTIVE=onboarding`) | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… MODE=set-mode DIRECTIVE=standard node run.mjs` (best-effort DOM check live — see below) |
| 3 | `launch` | happy path: `set-mode "onboarding"` → `launch-agent(AGENT)` → `agent-ready`, eventId-correlated | `SELFTEST_CONTRACT=new MODE=launch node run.mjs` | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… AGENT=claude-code MODE=launch node run.mjs` |
| 4 | `launch-failed` | unknown `agentId` → pre-dispatch `launch-failed "unknown-agent"`, no terminal | `SELFTEST_CONTRACT=new MODE=launch-failed node run.mjs` | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… MODE=launch-failed node run.mjs` |
| 5 | `launch-idempotent` | same `eventId` sent twice (fresh `createdAt`) → exactly one terminal, two identical `agent-ready` outcomes | `SELFTEST_CONTRACT=new MODE=launch-idempotent node run.mjs` | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… MODE=launch-idempotent node run.mjs` |
| 6 | `launch-eventid-reuse` | same `eventId`, a DIFFERENT `agentId` on the second send → dropped as malformed, no second outcome, no terminal for the other agent | `SELFTEST_CONTRACT=new MODE=launch-eventid-reuse node run.mjs` | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… MODE=launch-eventid-reuse node run.mjs` |
| 7 | `no-directive` | no `set-mode` ever sent → after the no-directive window expires, the container's own default (empty workbench → revealed) applies | `SELFTEST_CONTRACT=new MODE=no-directive node run.mjs` | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… MODE=no-directive node run.mjs` (poll window ~16s — best-effort DOM check live) |
| — | `ack` | §4.2 auth trigger, receiver replies | not applicable (needs a real auth-dialog round trip) | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… AGENT=claude-code MODE=ack node run.mjs` |
| — | `silent` | §4.2 auth trigger, receiver never replies → dashboard-not-detected fallback | not applicable | `ZCP_CS_URL=… ZCP_CS_PASSWORD=… AGENT=codex MODE=silent HEADLESS=false node run.mjs` |

Scenarios 2 and 7 read presentation state via DOM (`readEmbedHidden` in
`run.mjs`): against the SELFTEST stub this is a hard assertion (the stub's own
`#panel` marker); against a LIVE container it reads the real webview's
`document.body.hasAttribute("data-preload")` gate
(`vscode-bootstrap-welcome.html`'s `body[data-preload] { display: none; }`,
removed only by the host's `{type:"reveal"}` post) but is **best-effort** —
a failed read there logs `(best-effort DOM check, non-fatal)` and does not
fail the run, since the exact live DOM shape is unverified outside a real
container (see the S7 brief's stop condition: a needed `internal/**` change,
not a harness workaround, halts the slice instead of forcing a fragile guess).
Scenarios 1/3/4/5/6 assert purely off the bridge log (`window.__bridgeLog`)
and are hard assertions in both LIVE and SELFTEST.

```sh
# watch the silent-fallback path against codex (LIVE):
ZCP_CS_URL=… ZCP_CS_PASSWORD=… AGENT=codex MODE=silent HEADLESS=false make welcome-bridge-e2e
```

Exit code `0` = PASS, `1` = assertion failed / exception (a full-page
screenshot + the bridge log are written to `artifacts/`).

## Why localhost (not 127.0.0.1)

`welcome.js`'s `isAllowedGuiOrigin` trusts `http://localhost` on any port for
inbound acks, and the container nginx `frame-ancestors` allows
`http://localhost:*` to embed code-server — `127.0.0.1` matches neither. The
server binds loopback but the browser is pinned `localhost → 127.0.0.1` so the
page origin is exactly `http://localhost:<port>`. SELFTEST's stub embed is
served from a SECOND ephemeral loopback port (a genuinely different origin,
same as a real code-server's), so `frameByOrigin`/`isAllowedGuiOrigin`-style
origin matching behaves identically to LIVE.

The code-server auth cookie is `SameSite=None; Secure; **Partitioned**`, so a
first-party login does not carry into the localhost-embedded iframe; the runner
authenticates **inside** the iframe (its own partition). SELFTEST's stub has no
login at all.

## Contract mirror — keep both ends in sync

`gui-harness.html` mirrors the frontend receiver's validation (§4.2) and the
embed command channel drivers (§4.3); `run.mjs` hard-codes the phase strings,
the six §4.2 payload keys, and the seven §4.3 announce keys. The
authoritative home is `docs/spec-welcome-mode.md §4` and the templates it pins
(`internal/content/templates/vscode-bootstrap-welcome.{js,html}`). If the
payload shape, channel/version, ack format, origin rules, or phase copy
change, update **both** ends. Currently pinned:

- §4.2 phase copy (`AUTH_PHASE_TEXT`): `"Contacting the Zerops dashboard…"` →
  `"Finish signing in via the Zerops dialog…"` (ack) or `"Zerops dashboard not
  detected — reload the Zerops page"` (silent/no-dashboard). Rendered into
  `[data-agent-status="<agent>"]` (there is no separate "Build" nav tab — the
  agent rows are directly on the single panel).
- §4.2 trigger keys: `channel, version, type, agentType, eventId, createdAt`.
- §4.3 announce keys: `channel, version, type, eventId, createdAt, agents,
  bootstrapVersion` — **no** `installed` axis.
- §4.3 types: `embed-ready` (embed→FE), `set-mode` (FE→embed, `mode:
  "onboarding"|"standard"`), `launch-agent` (FE→embed, `agentId` only),
  `agent-ready` / `launch-failed` (embed→FE, `agentId` + the command's
  `eventId`; `launch-failed` also carries `reason`).
- The manual entry command is **`zerops.panel`** ("Zerops: Open Panel") — the
  old "Zerops: Get Started" walkthrough command is gone.

**No secrets ever live in this directory** — SELFTEST needs none at all;
LIVE's `ZCP_CS_PASSWORD` is passed as an env var by the caller, never written
to a file here.
