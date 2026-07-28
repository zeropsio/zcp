# 04 — Research: skill-pack salvage inventory

- `status:` closed
- `type:` research
- `assignee:` research-subagent (fired 2026-07-28)
- `blocked-by:` —

## Question

The archived branch carries the skill-pack catalog the owner wants ported (functionality, not
UI). Inventory it — reading via `git show archived/welcome-ux-redesign:<path>` /
`git diff main...archived/welcome-ux-redesign`, never a checkout:

1. What exists: `docs/spec-skill-packs.md` contract (granular selection, atomic Superpowers,
   discovery roots, live-discovery), the Go + extension code paths, tests, install/remove flows.
2. Its dependency footprint and how far it diverges from main's §6 curated-skills model — what
   replaces what in a port.
3. The **functional contract the new panel UI designs against**: the operations a user can
   perform, per-skill/per-pack states, failure modes.
4. A port sketch: the commits/files a `/flow` port slice would carry onto
   `feat/agent-first-onboarding`, and any landmines (parity pins, version bumps, gitignored
   content).

Findings: `plans/research/skillpack-salvage-2026-07-28.md`.

## Answer

Findings: [`plans/research/skillpack-salvage-2026-07-28.md`](../../research/skillpack-salvage-2026-07-28.md).

The ticket's premise was stale: **main already ships the community skill-pack installer**
(`d0be6787`, `internal/skillpacks/` + `zcp skills pack-add/remove/status`, 4 packs, manifest v2,
flock, atomic install) — landed independently of the archived branch, whose merge-base is main's
current tip. What the archive adds on top is narrow and well-bounded: the
`ReviewGranularity`/`SelectionGranularity` axes + 22-skill Matt / 14-skill Superpowers catalogs,
the declarative revision-gated `pack-set` (CLI verb + `PackSet` with preflight-then-atomic apply
and the whole-repo→reviewed-subset detach migration), an unconditional skill-roots init step, and
the picker's *behavior* (its HTML/JS is entangled with the dead hub/journey UI — re-implement
against `pack_picker.test.js`/`pack_install.test.js` as specification-by-test). Landmines: the
archived `spec-skill-packs.md` prose assumes pre-`d0be6787` main and needs an editing pass before
promotion; ext-version bump on every template edit; the Matt migration path needs a fresh test on
the target branch. Separately discovered drift: main's `spec-welcome-mode.md` §6 still describes
the deleted embedded-curated-skills model — ticket 07 owns the fix.
