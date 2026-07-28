# 06 — Panel UX

- `status:` closed
- `type:` prototype
- `assignee:` krls2020 (session 2026-07-28)
- `blocked-by:` 04

## Question

Design the reduced panel as a reactable artifact (state click-through + live container):

- Agent rows over the §3 axes: authorized → `Open terminal` + `Open extension` (where one
  exists); unauthorized → `Authorize` (bridge trigger); not-installed → informative. Reconnect
  and in-flight bridge phases as row states.
- Skills setup: a from-scratch UI over the salvaged granular skill-pack functionality
  (contract from ticket 04). Guided sits here.
- Data Studio entry at the top, opening the single tab.
- Copy per the first-time-developer voice rule; no hints/videos/journey content.

## Answer

Resolved 2026-07-28 (prototype click-through, two reaction rounds with owner). Asset:
[`../prototype/panel-clickthrough.html`](../prototype/panel-clickthrough.html) — four variants,
six state scenarios, width simulator; brief in
[`../prototype/BRIEF-panel-clickthrough.md`](../prototype/BRIEF-panel-clickthrough.md).

**Winner: variant D** — variant A's single-column stack structure with three owner amendments:

1. **Data Studio sits in a compact box top-right**, beside the header — agents are the primary
   content ("people will primarily want to start with an agent"); Data Studio is secondary but
   immediately visible. At narrow panel widths the box stacks between header and agents.
2. **Collapsed agent list once the user has an agent.** With ≥1 agent in an active state
   (authorized / authorizing / reconnect) the panel renders only those rows plus a subtle
   `+ Add another agent` expander that reveals the rest (unauthorized rows with `Authorize`,
   not-installed informative rows) and toggles to `Hide available agents`. With zero active
   agents the full list shows, no expander. Effect: a set-up panel is short and the skills
   section is visible without scrolling.
3. **Skills ownership line** under the Skill packs heading (verbatim): "Skill packs are just a
   shortcut — this workspace is yours, and you or your agent can add skills to it directly at
   any time."

Confirmed from the base design (all variants shared these; owner raised no objection): agent
row states over the §3 axes with bridge phases as row states (authorizing: contacting → finish
signing in via the Zerops dialog → finishing; reconnect: "Can't reach the Zerops dashboard —
retrying…" + `Try again`); Matt picker with 22-skill catalog, per-category select-all, pending
add/remove counter, conflict → reload behavior; guided row with Claude-Code-only lock; pack
state copy for absent/installing/installed/subset/incomplete/modified/broken/retired;
first-time-developer voice throughout.

**Scope note (owner, explicit): the prototype pins LAYOUT and BEHAVIOR, not visual design.**
The visual design is produced properly later; ticket 07's spec must carry the structure and
state behavior, never the prototype's pixels. (Also recorded as a standing preference on the
map.)

Interaction details left open (already tracked in Not yet specified, unchanged by this answer):
re-onboarding entry (the mock offers `Open terminal`/`Open extension` only), failure UX after
the overlay drops, accessibility contract of state-delta re-renders.
