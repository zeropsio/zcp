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

Writes screenshots into the working directory and prints a JSON summary:

| shot | state captured |
|---|---|
| `shot-claiming.png` | the claiming cover, before userData resolves into `picking` |
| `shot-picker.png` | the static agent tiles (light theme) |
| `shot-picker-dark.png` | the same picker under `zef-dark-theme` |
| `shot-hover.png` | mouse parked on the second tile — the hover treatment |
| `shot-after-pick.png` | whatever follows the pick (auth dialog, or launch-ready) |
| `shot-after-dismiss.png` | *(auth path)* after ESC/X on the dialog — must be `picking` again |
| `shot-launch-ready.png` | *(authorized path)* the confirmation gate with the CTA |
| `shot-launching.png` | *(only with `DO_LAUNCH=1`)* after pressing the primary CTA |

The JSON reports the wizard's rendered text, the layer's computed `z-index`,
whether a dialog pane exists and — via `elementFromPoint` at the pane's own
centre — whether that dialog is actually **on top** (the field that catches
stacking regressions), plus: `hoverMoved` (bounding-rect delta of the hovered
tile — pins the no-jump contract), tile count / `aria-pressed` values / badge
count, and the focus contracts (`focusOnSelected` after a dismissal bounce,
`ctaFocused` in launch-ready).

`AGENT` selects which tile is clicked (default `codex`). An agent that is NOT
yet authorized exercises the auth path — the probe then dismisses the dialog
and verifies the bounce back to `picking` with the pick retained. An
already-authorized one skips to `launch-ready` (spec-welcome-mode.md §8.1/§8.2);
add `DO_LAUNCH=1` to also press the CTA and capture `launching`.

## Credentials

Passed by env only, never written to disk. Nothing in this directory may ever
contain a secret.
