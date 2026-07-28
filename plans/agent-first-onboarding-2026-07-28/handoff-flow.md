# Hand-off: agent-first onboarding → /flow

The wayfinder map `map.md` is complete. This note is the entry brief for the implementing
`/flow` run(s). The **contract** is the spec pair — read those first, they are authoritative;
this note only adds sequencing, facts, and landmines that don't belong in a spec.

## The contract

- `docs/spec-welcome-mode.md` — rewritten in full: agent-first concept, startup policy
  (`agentFirst`), receiver lifecycle (§1.3), bridge §4 (envelope / auth trigger / embed command
  channel), terminal-only launch execution §5, agent panel §6, skills+guided §7, **FE contract
  §8** (the section `../frontend-legacy` builds against — no FE copy exists by design),
  deletion inventory §11, invariants W1–W15 (rows marked *(new)* are /flow's to pin). The spec
  passed a Codex adversarial review (2026-07-28); its five blockers are already folded in
  (`awaiting-mode` receiver state, FE queued launch intent, sender-context `createdAt`
  stamping, host-page embed-classification predicate, `relay-forwarded` teardown receipt).
- `docs/spec-dataconsole.md` — reconciled for the single-tab Data Studio surface: product
  invariant 7 + new §4.4 (singleton reveal-and-switch, rail-always-visible-embedded, entry
  points incl. the activity-bar stub view, sidebar-subsystem deletion).

Spec-ahead-of-code: both describe the TARGET state; the code on this branch is still main's.
LAND reconciles the *(new)* invariant pins and promotes `docs/spec-skill-packs.md` (below).

## Branches

- **zcp**: build on `feat/agent-first-onboarding` (this branch, branched off latest main).
- **FE**: `../frontend-legacy` branch `feat/agent-first-onboarding` (= `origin/devel` tip
  `022d0af03` + the 4 bridge-receiver commits; the receiver never merged to devel — verified,
  so the extended v1 channel breaks no deployed receiver).

## Suggested slices (each independently green)

1. **Skill-pack port** (zcp; independent of everything else). Salvage from
   `archived/welcome-ux-redesign` per `plans/research/skillpack-salvage-2026-07-28.md` §4 port
   sketch: granularity axes + catalogs (`eaa8f73d` + catalog/errors halves of `36d920d2`),
   `pack-set` + `fetchCommit` + status fields, CLI verb, unconditional skill-roots init step;
   replay the archived Go tests as RED first. Promote `docs/spec-skill-packs.md` with an
   **editing pass** — its prose assumes pre-`d0be6787` main; verify every claim against
   main's actual `internal/skillpacks/`. Write a fresh Matt whole-repo→subset detach-migration
   test on THIS branch (don't trust the archived fixture's starting state).
2. **Container: bridge command channel + startup + launch** (zcp). §4.3 five types +
   one-outcome-per-eventId store; §1.1/§1.3 startup policy rename + receiver lifecycle; §5
   terminal-only executor (select `mode:"terminal"` explicitly, never `opens[0]`); §11
   deletion inventory (kickoff wrapper: 5 sites listed in
   `plans/research/terminal-launch-readiness-2026-07-28.md` §4 — keep
   `seedOpenWithPrompt`/`shellQuoteArg`/`ONBOARD_PROMPT`).
3. **Container: agent panel** (zcp). §6 layout/behavior per the click-through
   (`prototype/panel-clickthrough.html`, variant D — structure and states only, never its
   pixels); §7 picker behavior spec-by-test from the archived
   `pack_picker.test.js`/`pack_install.test.js` (re-implement UI, port assertions).
4. **Data Studio single-tab** (zcp). Per `spec-dataconsole.md` §4.4 +
   `plans/research/datastudio-single-tab-2026-07-28.md`: bounded deletion of the sidebar
   subsystem (§2A file list), command + stub-view entries, `shouldHideServiceRail` flip
   (embedded → always show rail), uitest harness re-entry helpers (sidebar path dies).
5. **FE: wizard + dev entry** (`../frontend-legacy`). §8: wizard service (evolved claim
   overlay service), listener extension in the code-server overlay feature, `?zcpOnboard=1`
   effect. Delete the load+3s/45s/`zcp-vscode-ready` machinery.
6. **E2E**: extend `tools/welcome-bridge-harness` (runbook in its README) with the §4.3
   message types; exercise announce→set-mode→launch-agent→agent-ready on the localflow rig.

## Test rig (stood up, verified live — ticket 10)

- Local GUI: `cd ../frontend-legacy && npm run start:zerops` → **http://localhost:1111**
  (real prg1 API; log in with the real account). Trust is built-in for hostname `localhost`
  (never 127.0.0.1) — `ZCP_WELCOME_BRIDGE_ORIGINS` stays febridge-only, no env change.
- Container loop (VPN up: `zcli vpn up gRLfpBNrSziMKj0VEfk6vw`):
  `./eval/scripts/build-deploy.sh` then `ssh zcp "cd /var/www && zcp init"`, reload the
  code-server window.
- Embed path and further facts: ticket `tickets/10-local-fe-test-rig.md`.

## Facts & landmines to carry

- **Every welcome/studio template edit ships with its version-const bump in the same commit**
  (`BootstrapExtVersion` / `studioExtVersion` parity pins) — an unbumped edit never reaches a
  running fleet.
- **zembed env propagation lags ~5–10 s** behind the GUI's flag write — the reason launch has
  no authorized-flag gate (spec §4.3); don't "fix" it by adding one.
- **PROVE gates** (spec-mandated, run before BUILD): every registry agent's initial-prompt
  argv live-proven (`claude "prompt"` auto-submit rests on upstream issues #11476/#17284, not
  the docs page; cursor/grok have no in-repo pin — spec §5.1 names the fallback options), and
  the §1.3 embed-classification predicate ("host page itself framed") proven across
  standalone / custom-GUI / `app.zerops.io` browser shapes.
- No VS Code API can prove an agent TUI is running — `agent-ready` is deliberately S2
  ("command dispatched to a live terminal"); don't strengthen it.
- The receiver-lifecycle model (spec §1.3: boot-always + `awaiting-mode` + self-close on
  standard+`hadRestoredEditors`) is ticket 07's reconciliation of an inconsistency between
  tickets 01/02/08, hardened by the Codex review — treat its exact mechanics as SHAPE-phase
  material, not as an owner-blessed pixel-level contract.
- The archived branch is a parts donor: functional seams only, never UI, never its plans.
  Read via `git show archived/welcome-ux-redesign:<path>`.

## Left open deliberately

- Visual design of the panel (produced during implementation; the mock pins structure only).
- Exact new-test names for *(new)* invariant rows W10–W14.
- Data Console reload-survival (serializer) — pre-existing gap, kept; revisit only if the
  single-tab UX proves it painful.
