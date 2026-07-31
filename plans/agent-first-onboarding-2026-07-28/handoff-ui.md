# Hand-off: continuing UI work on the agent-first onboarding

Entry point for a fresh session that needs to keep changing the onboarding UI.
This file is a **map plus the operational knowledge that would otherwise die with a session** —
it deliberately does not restate the contract or the history, because both are already durable:

| what you need | where it authoritatively lives |
|---|---|
| What the system must do, and why | `docs/spec-welcome-mode.md` (§1 startup, §4 bridge, §5 launch, §6 panel, §8 FE wizard) |
| What was built, in what order, with SHAs | `plans/agent-first-onboarding-2026-07-28/flow.md` |
| Why any single change was made | `git log` in both repos — the commit bodies carry the reasoning |

Read the spec section you are about to touch. Do not re-derive intent from the code.

## Two repos

- **Container / CLI**: `/Users/macbook/Documents/Zerops-MCP/zcp`, branch `feat/agent-first-onboarding`.
- **Frontend**: `/Users/macbook/Documents/Zerops-MCP/frontend-legacy`, branch
  `kh-agent-first-onboarding` (renamed from `feat/agent-first-onboarding` 2026-07-30 — FE
  repo convention: no slashes, `<initials>-` prefix; local-only, never pushed under the old
  name). 30+ commits past its base `0d6423924`.

The bridge contract has **one home** — the zcp spec. Never copy contract prose into the FE repo.

## State as of this hand-off

Everything below is landed and verified, not in flight:

- Container: bundle **0.1.25** — command channel, receiver lifecycle, terminal-only launch,
  agent panel, skill packs, single-tab Data Studio, E2E harness. welcomejs 325/325.
- FE: the onboarding wizard end to end — bridge validator fork, wizard state machine, tile
  picker, dev entry — plus the 2026-07-29 owner rework (S6k–S6m): `launch-ready`
  confirmation gate, real auth-completion signals, dismissal bounce, static registry
  roster, `successNavigation:'none'`, card-tile redesign with `--zcp-wizard-*` tokens.
  Suite 197/197, tsc + lint clean.
- The full live battery passed on the rig, and the pre-rework wizard was driven end to end
  in a real browser. The reworked flow has NOT had a live probe run yet (needs
  `ZE_EMAIL`/`ZE_PASS`) — that is the first thing to do in a session that has them.

Still open: **ASSEMBLE** — an independent verifier pass over the whole feature, the Verify Trace
in the plan file, and an owner retest pack. Nothing blocks further UI work.

## Running it

```sh
cd ../frontend-legacy && npm run start:zerops     # → http://localhost:1111
```

The container side already runs on the rig; you only redeploy it if you change zcp code:

```sh
./eval/scripts/build-deploy.sh && ssh zerops@zcp "cd /var/www && zcp init"
```

**Dev entry into onboarding** (ships dark, no gate — spec §8.2):

```
http://localhost:1111/project/gRLfpBNrSziMKj0VEfk6vw?zcpOnboard=1
```

Log in with the real account first; the dev server talks to the live prg1 API. The param strips
itself from the URL, so a reload does not re-trigger — re-paste it to run again.

Second, complementary aid — replay the REAL cookie-drain path (which the param bypasses):
set `ZGUI_ENABLE_SIMULATE_ZCP_POOL_CLAIM="true"` in the gitignored `apps/zerops/.env`;
every reload then re-runs the full drain tail (wizard up → ZCP resolve → authorized
snapshot → picking) for the logged-in account, no `?zcp=true` signup needed.

## Seeing your changes without clicking

```sh
ZE_EMAIL=… ZE_PASS=… AGENT=codex node tools/onboard-ui-probe/probe.mjs
```

Screenshots every wizard state and reports the layer's z-index plus **whether a dialog is
actually on top**. Details: `tools/onboard-ui-probe/README.md`.

Use it. Three of the defects found on this feature were invisible to a green test suite and only
appeared against the running app: a layer stacked above the dialog band, hover tints that were
white-on-white in the default theme, and a param-strip that navigated off the project route.
jsdom applies no component styles and computes no stacking — a DOM-only assertion cannot see any
of that.

## Where the UI lives

- Wizard layer + tile picker: `apps/zerops/src/modules/core/zcp-pool-claim-base/zcp-onboard-wizard.component.ts`
  (standalone, inline template + styles) and its `.spec.ts`.
- Wizard state machine: `zcp-onboard-wizard.service.ts` in the same directory. The component is
  **pure presentation** over its signals — keep it that way.
- Bridge listener + auth dispatch: `apps/zerops/src/modules/feature/code-server-overlay/code-server-overlay.feature.ts`.
- Entry points: the cookie drain in `zcp-pool-claim-base.effect.ts` (real users) and the dev param
  in `pages/+project-detail/project-detail.effect.ts`.
- Container-side panel (a different surface — inside vscode, not the overlay):
  `internal/content/templates/vscode-bootstrap-welcome.{js,html}` in the zcp repo.

## Decisions already made — don't quietly reverse them

- **Layer z-index is 500**, bracketed between the code-server embed (250) and the CDK overlay
  container (1000). At a higher value the auth dialog opens *underneath* the layer and auth
  becomes unreachable. Pinned by a test that reads the declaration and asserts the band.
- **Agent marks come from the design system** (`zui-claude-mark` etc.), keyed by the same ids the
  roster uses. Never substitute a placeholder logo.
- **Tile treatments are theme tokens**, not hardcoded colours: the layer paints
  `--z-app-background`, which is light by default and dark under the app's dark-theme classes.
- **Agent names are never truncated** (owner call). The label reserves two lines so a wrapping
  name keeps every tile the same height.
- **One row, always.** The picker reads as a single rank; tiles shrink rather than wrap.
- **The roster is the static registry** (`SUPPORTED_AGENT_TYPES`), rendered instantly — no
  `ZCP_AGENTS` parsing, no skeleton, no mid-wizard mutation (owner call; §8.1 carries the
  restricted-pool invariant). Authorized ids still come from the FE's own userData and only
  steer the skip.
- **Launch is always CTA-initiated.** Auth completing lands on `launch-ready` (layer stays
  up); the intent (eventId + 30 s timer) is minted only by the primary button. Reverses the
  earlier no-CTA ruling — owner call after live use.
- **Auth completion = the `markAuthorized` action** (stack + picked agent), never
  `manualOpenResult` — its `ok` only means "the dialog-open request resolved". Dismiss
  (X/ESC) bounces to `picking` with the pick retained; wizard-owned dialog opens pass
  `successNavigation:'none'` so success cannot re-dock the embed underneath the layer.
- **All non-agent exits converge**: Skip-for-now, the `launch-ready` secondary, and the
  failure Continue all close the wizard + the code-server overlay and land on project
  detail. There is no standard-mode reveal path out of the wizard.
- **Tiles never move under the cursor** — hover is background/border tint only, inside
  `@media (hover: hover)`; transition list is exactly those two properties (pinned by
  source-string tests + the probe's `hoverMoved` field).

## Traps that bit every agent on this feature

- **jest cannot transform the repo's ESM-only transitive deps.** Importing a real barrel pulls
  transloco / `lodash-es` / `date-fns/esm` and the suite dies for an unrelated reason. The
  established fix is a local `jest.mock(...)` inside your own spec file — see
  `zcp-pool-claim-base.effect.spec.ts`'s header. Do **not** edit the shared `jest.config.ts`.
- **The component's styles live inside a template literal**, so a backtick in a CSS comment ends
  the string and the build fails with a confusing `TS1005`. Write comments without backticks.
- **`crypto.randomUUID` does not exist in this repo's jsdom**, so any code path using it is
  silently untested. Polyfill it in the spec if you need to exercise such a path.
- **The FE repo caps a phase at ≤5 files with owner approval between phases**
  (`frontend-legacy/CLAUDE.md`). That cap is what produced most of the integration gaps on this
  feature: each phase was green alone while the seam between phases was never joined. If you slice
  work, **grep across the seam afterwards** — `embedBridgeEvents$` once had zero subscribers and
  every test still passed.
- **Any edit to a `vscode-bootstrap-*` template in the zcp repo ships with a `BootstrapExtVersion`
  bump** in the same commit (plus the template `package.json` and the `diagnostics.test.js` golden
  literal). code-server reloads off the index version — an unbumped edit never reaches a running
  container, and you will debug a change that was never deployed.
- **No worktree isolation on these branches.** Two agents (or an agent and a human) editing the
  same file concurrently is a real hazard here, not a hypothetical — it happened. Prefer one
  writer per file at a time.
