# Plans Backlog

Deferred ideas surfaced during plan / review / implementation work that we
explicitly chose NOT to ship in the current scope but want to keep on the
record for a later round.

**Why a backlog folder**: ideas that live only in conversation context get
lost on compaction; ideas buried inside a current plan rot when that plan
archives. This folder is the durable register.

## File convention

- One file per entry, slug-named: `plans/backlog/<topic-slug>.md`
- No date in filename — the **Surfaced** field inside the body carries the
  origin date. The filename should still read well a year from now.
- Required frontmatter-style fields at the top of each file:
  - **Surfaced**: `YYYY-MM-DD` + originating context (e.g. "live agent run on
    `build-integration=actions` confirm response")
  - **Why deferred**: what was good enough for the current scope; why this
    extra step doesn't belong in the immediate fix
  - **Trigger to promote**: what signal would flip this from "nice to have"
    to "act now" (e.g. real-world feedback, dependent feature lands, etc.)
- Optional sections: **Sketch** (initial design), **Risks**, **Refs**.

## Workflow

1. **When you defer**: create `plans/backlog/<slug>.md` with the required
   fields. Keep entries focused — one cohesive idea per file. If a backlog
   item starts growing sub-bullets that themselves should be deferrable
   independently, split into multiple files.
2. **When you decide to act**: extract the file's content into a normal
   `plans/<slug>-YYYY-MM-DD.md` plan file (use the standard plan template),
   then `git rm` the backlog file. Don't leave both — backlog tracks *open*
   deferrals only.
3. **When you reject for good**: move to `plans/backlog/rejected/<slug>.md`
   with a one-line **Why rejected** at the top so we don't keep
   re-discovering the same idea.
4. **When the underlying work ships outside the deferral path** (a fix
   lands organically while pursuing something else, or a sister plan
   absorbs the scope): `git rm plans/backlog/<slug>.md` IN THE SAME
   COMMIT as the shipping change. A stale backlog entry that points at
   already-shipped code is a documented bug class —
   `docs/audit-prerelease-internal-testing-2026-04-30-roundtwo.md`
   caught one (`c3-failure-classification-async-events.md` survived
   for ~24h after `internal/ops/events.go:30-50` shipped the fix).
   The `TestNoRetiredVocabAcrossRepo` drift gate is the safety net,
   but atomic backlog closure is the upstream discipline.

Append-only between extract / reject events. Don't silently rewrite a
historical entry — append `**Update YYYY-MM-DD**:` lines if context shifts.
