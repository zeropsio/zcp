# 07 — Write the new spec pair

- `status:` closed
- `type:` task
- `assignee:` claude (session 2026-07-28)
- `blocked-by:` 01, 02, 05, 06, 08, 09, 11

## Question

Produce the destination artifact:

- Rewrite `docs/spec-welcome-mode.md` around the new concept: FE-driven onboarding via the
  bidirectional bridge contract (announce / mode directive / launch-agent / agent-ready —
  tickets 01–03), the reduced panel (ticket 06), startup policy (`startup.json` bool renamed per
  ticket 01 §5), the salvaged skill-pack contract (ticket 04 → replaces §6), and a deletion
  inventory (journey/CTA UI, kickoff wrapper per ticket 03 facts, hint/video content, env-watch
  auto-launch trigger). The legacy launcher survives only as the `app.zerops.io`
  suppress-fallback.
- Reconcile `docs/spec-dataconsole.md` for the single-tab surface (ticket 05).
- The FE-side contract section is what the `../frontend-legacy` work builds against (overlay
  flow per ticket 08, dev entry per ticket 09).
- Hand-off note for `/flow` on `feat/agent-first-onboarding`, including the skill-pack port and
  the FE work in `../frontend-legacy`.

## Answer

Resolved 2026-07-28 (task, AFK; Codex adversarial review applied). Produced:

- **`docs/spec-welcome-mode.md`** — full rewrite: §0 concept (FE = brain, container =
  validated executor), §1 startup policy + receiver lifecycle, §2 install (unchanged), §3
  state model (probes display-only), §4 bridge (§4.1 envelope/validation, §4.2 auth trigger
  as shipped, §4.3 embed command channel — numbering exactly as ticket 02 anticipated), §5
  terminal-only launch execution, §6 agent panel (variant D structure/behavior + row-state
  table + a11y), §7 skill packs + guided, §8 FE contract (tickets 08+09), §9 security floor,
  §10 portable, §11 deletion inventory, invariants W1–W15.
- **`docs/spec-dataconsole.md`** — reconciled: reach item 1, product invariant 7, new §4.4
  (singleton reveal-and-switch, rail always visible embedded, `zcpStudio.open`/`openService`,
  activity-bar stub view with every-visible-transition re-entry).
- **`plans/agent-first-onboarding-2026-07-28/handoff-flow.md`** — /flow entry brief: 6
  slices, rig runbook pointer, PROVE gates, landmines.
- CLAUDE.md key-specs line updated; the §6 curated-skills drift noted on the map is fixed by
  the rewrite.

**Names pinned** (delegated to this ticket): `startup.json` bool = `agentFirst`; dev-entry
param = `?zcpOnboard=1`; panel command = `zerops.panel`; console commands = `zcpStudio.open` /
`zcpStudio.openService`.

**Ticket-07-authored decisions** (not owner-ticketed; veto in spec review):

1. **Transient receiver with `awaiting-mode`** (§1.3) — reconciles a real inconsistency in
   the ticket lattice: ticket 01's no-forced-panel-over-restored-editors × tickets 02/08's
   announce-on-every-embed-boot × VS Code having no invisible webview (without announce, the
   ticket-09 dev entry on a container with restored editors could never deliver
   `launch-agent`). The surface boots always (embedded), announces, never self-closes before
   a directive or a 10 s window, self-closes on standard+`hadRestoredEditors`.
2. **10 s no-directive fallback** — a mute third-party embedder never bricks the container.
3. **Launch executor selects `mode:"terminal"` explicitly, never `opens[0]`** — resolves
   research 03's open question; claude's plugin stays `opens[0]` for the panel's Open
   extension only.
4. **Data Console rail always visible when embedded** (deep link only preselects) — resolves
   research 05's flagged shape decision.
5. **CTA kickoff prompts die with the journey** (map fog item "Fate of the CTA journey
   prompts") — the fixed onboarding prompt is the single entry; any build-vs-integrate fork
   happens inside the agent conversation.
6. **Panel a11y contract** (map fog item) — focus retention/handoff rules + one polite
   live-region announcement per state delta (W15).
7. **Legacy launcher surfaces context-key-hidden under agent-first** — two launch surfaces
   never render at once.

**Codex review** (2026-07-28, verdict "fix listed items first" — all five blockers + the
actionable should-fixes folded in): `awaiting-mode` (env default could close the only relay
before the FE directive), FE **queued launch intent** + 30 s intent timer (auth can beat
announce — §8.1), `createdAt` stamped by the **sending browser context** (webview vs FE page
per direction), embed classification = "the **host page** is itself framed" (`window.top !==
window` is always true inside a webview) as a /flow PROVE gate, **`relay-forwarded`** receipt
before receiver teardown, in-flight dedup entries + bounds, row-state taxonomy table
(matrix Reconnect ≠ transport failure; unobservable "finishing" phase removed), FE/container
`ZCP_AGENTS` parser parity fixture, stub-view re-entry on every visible transition, argv
PROVE gate. One dissent kept: Codex wanted `docs/spec-skill-packs.md` promoted before
hand-off; the map assigns promotion to the port slice (research 04), so the spec carries an
explicit forward reference and handoff slice 1 sequences it first.
