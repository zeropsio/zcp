# onboard-ui-probe

Drives the onboarding wizard in a real browser against the local FE dev server
and screenshots each state. Built during the agent-first /flow run because the
wizard's defects were **invisible to unit tests** — three of them (a layer
stacked above the dialog band, white-on-white hover, a navigate that bounced
off the project route) only showed up against the running app.

Use it whenever you change the wizard UI: it is faster than clicking, and it
catches the class of bug that jsdom cannot see.

## Prerequisites

- The FE dev server running: `cd ../frontend-legacy && npm run start:zerops`
  → http://localhost:1111
- Chrome installed (puppeteer-core launches `channel: "chrome"`).
- Deps: this script reuses `tools/welcome-bridge-harness/node_modules`
  (run `npm install` there once if absent).
- A real Zerops account — the dev server talks to the live prg1 API.

## Run

```sh
ZE_EMAIL='you@example.com' ZE_PASS='…' AGENT=codex \
  node tools/onboard-ui-probe/probe.mjs
```

Writes four screenshots into the working directory and prints a JSON summary:

| shot | state captured |
|---|---|
| `shot-skeleton.png` | the ~2 s waiting state before the roster resolves |
| `shot-picker.png` | the agent tiles, roster resolved |
| `shot-hover.png` | mouse parked on the second tile — the hover treatment |
| `shot-after-pick.png` | whatever follows the pick (auth dialog, or launching) |

The JSON reports the wizard's rendered text, the layer's computed `z-index`,
whether a dialog pane exists, and — via `elementFromPoint` at the pane's own
centre — whether that dialog is actually **on top**. That last field is the
one that catches stacking regressions; a dialog rendered underneath the layer
is present in the DOM and looks fine to every DOM-only assertion.

`AGENT` selects which tile is clicked (default `codex`). Pick an agent that is
NOT yet authorized to exercise the auth path; an already-authorized one skips
straight to launching (spec-welcome-mode.md §8.2).

## Credentials

Passed by env only, never written to disk. Nothing in this directory may ever
contain a secret.
