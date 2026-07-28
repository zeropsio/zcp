# Map: Agent-first onboarding

`wayfinder:map` · local-markdown tracker · opened 2026-07-28 · branch `feat/agent-first-onboarding`
· **COMPLETE 2026-07-28** — all 11 tickets closed, fog empty; the destination artifact is the
spec pair + `handoff-flow.md`. Next step: `/flow` per the handoff.

## Destination

A rewritten **`docs/spec-welcome-mode.md`** — the contract for the new concept: onboarding moves
to the embedding frontend (agent pick + authorization in an overlay over the vscode embed), the
container side announces itself over the bridge, executes the FE's **launch-agent** command in a
terminal with the onboarding prompt, and signals **agent-ready** to the top window; the welcome
surface itself reduces to an agent panel (launcher + skills + guided + Data Studio entry) —
**plus** the `docs/spec-dataconsole.md` reconciliation for the single-tab Data Studio surface,
**plus** (amended by ticket 01) the FE side in `../frontend-legacy`: the fullscreen overlay flow,
the FE bridge state machine, and a dev entry into onboarding mode from a logged-in project. That
spec pair + FE contract is the hand-off artifact: `/flow` takes it from there, building on
`feat/agent-first-onboarding` (branched off latest main) and a `../frontend-legacy` branch.

The map is done when nothing about the *shape* of the flow, the panel, or the handshake contract
is left to decide.

## Notes

**Domain.** The `zcp-bootstrap` extension welcome surface (`internal/content/templates/
vscode-bootstrap-*`, behavior pinned by `internal/content/welcomejs/*.test.js`) and the
`zcp-studio` Data Console extension (`internal/dataconsole/extension/`). Baseline is **latest
main** — the prior redesign effort never merged.

**Skills every session should consult.** `/grilling` + `/domain-modeling` default; `/research`
for AFK tickets; `/prototype` for the panel UX ticket.

**Standing preferences for this effort:**

- **The live container is the prototyping surface.** Ship edits to `zcp` in project `localflow`
  with `make zcp-dev-deploy`; treat the rendered surface as a mock where functions don't work.
- **No backward compatibility.** Never shipped to a customer; migration and versioned-state
  upgrades are non-concerns. Delete freely.
- **`archived/welcome-ux-redesign` is a parts donor.** Functional seams may be ported (the
  skill-pack machinery explicitly will be); UI is never ported. Read it via
  `git show archived/welcome-ux-redesign:<path>` — never check it out, never cite its plans.
- **Mechanics taken as working, not what this map decides:** bridge authorization (§4) +
  `zcp agent mark-oauth`, the terminal launch seams, the versioned bootstrap install (§2), the
  Data Console broker/write-token posture (`spec-dataconsole.md` §4.1/§5).
- **Copy voice** (carried from the archived effort's closed decision): every line is written from
  the point of view of a developer seeing the surface for the first time — internal state
  vocabulary, our architecture as explanation, and positioning statements are out.
- **FE work is ours** (amended by ticket 01; supersedes "FE implementation is out of our
  hands"): the overlay flow, FE bridge state machine, and dev entry live in `../frontend-legacy`.
  Test loop: local GUI dev server over the `localflow` zcp service (ticket 10).
- **Artifacts in English** (repo convention); conversation in Czech.
- **The panel prototype pins layout + behavior, never visual design** (owner, closing ticket
  06): the click-through mock is the contract for structure and state behavior; the visual
  design is produced properly during implementation, not lifted from the mock's pixels.

**Settled while charting (2026-07-28), so no ticket re-opens them:**

| Decision | Consequence |
|---|---|
| Onboarding moves to the FE overlay (agent pick + auth) | welcome loses ALL onboarding content; no in-vscode auth during first run; container side reacts to bridge commands (launch/mode — amended by ticket 01) + envs (state display) |
| Destination is the spec pair handed to `/flow` | map is planning-only; no execution tickets |
| Baseline is latest main; prior effort archived on `archived/welcome-ux-redesign` | its two open tickets (write §0, clickthrough) died with it |
| Guided stays | sits with the skills settings in the panel |
| Skill-pack functionality is salvaged from the archive | granular selection machinery ports; UI/UX is redesigned from scratch |
| One new surface: the reduced welcome webview panel | legacy launcher survives ONLY as the `app.zerops.io` suppress-fallback; convergence deferred |
| Onboarding layout = Explorer open + maximized terminal running the agent | no panel, no tabs during onboarding (owner screenshot on ticket 01) |
| Subsequent visits auto-open the panel | launcher + skills setup + Data Studio entry at the top |
| Panel rows: authorized → `Open terminal` + `Open extension` (where one exists); unauthorized → `Authorize` via the existing bridge trigger; not-installed → informative row | §4 bridge mechanics unchanged, new UI only |
| Data Studio always opens as ONE WebviewPanel tab with in-tab service switching | the sidebar webview view dies; the activity-bar icon stays as a third entry that opens the tab directly |
| The onboarding prompt stays the fixed `"Onboard me to Zerops."`, submitted at launch | terminal argv path for every agent — the VS Code extension is not the onboarding vehicle |

## Tracker conventions

- The map is this file. Tickets are `tickets/NN-slug.md`.
- A ticket header carries `status`, `type`, `assignee`, `blocked-by`.
- **Claim** a ticket by setting `assignee:` before any work.
- A ticket is **unblocked** when every id in `blocked-by:` has `status: closed`.
- The **frontier** is every ticket that is `status: open`, unblocked, and has `assignee: —`.
- Resolve by appending an `## Answer` section, setting `status: closed`, and adding one line to
  **Decisions so far** below.

**Known drift found while researching** — RESOLVED by ticket 07: main's `spec-welcome-mode.md`
§6 described the embedded-curated-skills model that commit `d0be6787` deleted; the rewritten §7
now describes the community skill-pack installer, deferring mechanics to `spec-skill-packs.md`
(promoted during the /flow port slice).

## Decisions so far

<!-- one line per closed ticket -->

- [04 — Research: skill-pack salvage inventory](tickets/04-research-skillpack-salvage.md) — main
  already ships the pack installer; the salvage is only the granularity axes + revision-gated
  `pack-set` + skill-roots init step + picker *behavior* (spec-by-test, UI re-done). Findings:
  [`plans/research/skillpack-salvage-2026-07-28.md`](../research/skillpack-salvage-2026-07-28.md).
- [03 — Research: terminal launch + readiness facts](tickets/03-research-terminal-launch-facts.md) —
  `claude "prompt"` auto-submits interactively; no VS Code API can prove a TUI is running (honest
  ceiling: terminal created / text sent / maybe command-started via shell integration) — the
  ready-signal honesty level is ticket 02's call; kickoff wrapper deletable across 5 sites;
  `handleOnboard` launches `opens[0]` (= plugin for claude), so terminal-only onboarding can't
  reuse it as-is. Findings:
  [`plans/research/terminal-launch-readiness-2026-07-28.md`](../research/terminal-launch-readiness-2026-07-28.md).
- [01 — Startup policy & auto-launch semantics](tickets/01-startup-and-autolaunch.md) — launch is
  an explicit FE→embed bridge command (announce → retained `ev.source` → launch-agent →
  retry-until-agent-ready), never env observation; one-shot lives in the FE wizard, no container
  marker; startup branches on env-derived state + FE mode directive + container-owned
  `hasEditors` rule; standalone always shows the panel; `startup.json` keeps one init-derived
  bool, renamed. Scope amendment: FE work in `../frontend-legacy` is in scope (tickets 08–10).
- [02 — Bidirectional bridge contract](tickets/02-agent-ready-handshake.md) — same channel
  `@zerops/zcp-agent-auth-bridge` v1 extended (verified: no deployed receiver to protect); five
  new types (`embed-ready` roster / `set-mode` / text-free `launch-agent` / `agent-ready` = "command
  dispatched to a live terminal", sent immediately / `launch-failed` pre-dispatch only); one
  outcome per eventId, idempotently re-acked, retry = same eventId; shipped ack validation
  reversed, NO authorized gate on launch (zembed lag); contract home = bridge § of the rewritten
  spec, no FE copy.
- [05 — Research: Data Studio single-tab readiness](tickets/05-research-datastudio-single-tab.md) —
  the "prepared by design" claim holds: multi-service-in-one-tab is dormant shipped functionality
  behind one boolean (`shouldHideServiceRail`); the conversion is a bounded deletion of the sidebar
  subsystem. Icon-as-entry needs a stub view that auto-forwards + collapses (VS Code won't allow a
  zero-view activity-bar item); the deep-link rail-visibility rule is a small shape decision left
  to the spec ticket. Findings:
  [`plans/research/datastudio-single-tab-2026-07-28.md`](../research/datastudio-single-tab-2026-07-28.md).
- [08 — FE overlay flow (agent pick + auth + bridge state machine)](tickets/08-fe-overlay-flow.md) —
  the wizard is a richer layer over the shipped claim-flow skeleton (embed prewarms + announces
  behind it; the load+3s/45s dismissal timers and dead `zcp-vscode-ready` die); explicit entry
  only, abandonment deliberately unhandled (recovery = standard path, no prompted-launch state);
  pick = single-select from `ZCP_AGENTS` via FE userData; auth = existing dialog over the layer,
  zero rebuild; launch auto-fires on auth completion (no CTA, ready = S2); failure/30s-timeout →
  one Continue closing layer + embed to the Zerops GUI; state machine = root signals service
  (evolved `ZcpClaimOverlayService`), announce listener stays in `CodeServerOverlayFeature`.
- [06 — Panel UX](tickets/06-panel-ux.md) — winner: single-column stack with Data Studio in a
  compact box top-right (agents primary), agent list collapsed to active rows + `+ Add another
  agent` expander once ≥1 agent is set up, skills ownership line ("packs are just a shortcut");
  the mock pins layout + behavior only, never visual design. Asset:
  [`prototype/panel-clickthrough.html`](prototype/panel-clickthrough.html) (variant D).
- [10 — Test rig: local Zerops GUI over the localflow zcp service](tickets/10-local-fe-test-rig.md) —
  rig stands: dev server http://localhost:1111 (FE branch `feat/agent-first-onboarding` =
  devel + bridge receiver), `localhost` trusted built-in so `ZCP_WELCOME_BRIDGE_ORIGINS`
  stays untouched, container loop = `eval/scripts/build-deploy.sh` + `zcp init` (agent-run,
  verified live); transport proof owned by the repeatable `welcome-bridge-e2e` harness.
- [11 — Post-drop failure presentation in vscode](tickets/11-post-drop-failure-vscode.md) — the
  terminal IS the whole answer for every launch path (no panel auto-open / notification /
  transient row state — any reaction would sit on best-effort shell-integration detection);
  steady state = plain `authorized` row, and the reload-with-restored-terminal edge (`hasEditors`
  suppresses the panel) is deliberately accepted.
- [09 — Dev entry: invoke onboarding mode from a logged-in project](tickets/09-dev-entry-trigger.md) —
  onboarding is strictly once for users (no re-onboard surface anywhere; re-entry = standard
  vscode + panel); dev entry = one-shot self-stripping query param on project detail, shipping
  dark (no gate, no UI); pure bypass of the cookie machinery — the effect runs the drain's tail
  inline and enters the wizard at `picking`; project without a ZCP → silent no-op.
- [07 — Write the new spec pair](tickets/07-write-new-spec.md) — **the destination artifact,
  delivered**: `docs/spec-welcome-mode.md` rewritten (concept / receiver lifecycle / bridge §4 /
  launch §5 / panel §6 / FE contract §8 / deletion inventory §11 / W1–W15),
  `docs/spec-dataconsole.md` reconciled (invariant 7 + §4.4 single-tab), `handoff-flow.md` for
  /flow; names pinned (`agentFirst`, `?zcpOnboard=1`, `zerops.panel`, `zcpStudio.open*`);
  ticket-lattice inconsistency resolved via the `awaiting-mode` transient receiver; Codex
  adversarial review applied (5 blockers folded in). Spec-authored decisions open to veto are
  listed in the ticket's Answer.

## Not yet specified

*(empty — the map is complete. The last two fog items were decided inside the ticket-07 spec
rewrite: the CTA kickoff prompts die with the journey — spec §11; the panel a11y contract is
spec §6 + W15.)*

<!-- Graduated 2026-07-28 after 06+08 closed: "Failure UX after the overlay drops" → ticket 11;
     "Re-onboarding entry" → folded into ticket 09's dev-only-or-ships call. -->

## Out of scope

- **Launcher→panel convergence.** The legacy launcher stays as the `app.zerops.io`
  suppress-fallback exactly as shipped; folding it into the new panel is a future effort.
- **The production `app.zerops.io` dashboard onboarding** — drives itself, unchanged.
- **Agent VS Code extensions as the onboarding vehicle.** Terminal-only now; the extension
  remains a panel convenience action (`Open extension`).
- **New community skill packs** beyond what the salvaged catalog already holds.
