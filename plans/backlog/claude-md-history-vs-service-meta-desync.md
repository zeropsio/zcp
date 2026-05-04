# CLAUDE.md historical record desyncs from on-disk ServiceMeta

**Surfaced:** 2026-05-04, eval suite `20260504-065807` retros from
`existing-standard-appdev-only-reminders` AND
`verify-subdomain-recovery-before-browser` (two scenarios in one run).

**Why deferred:** ZCP state-discipline question that needs design
thought, not a one-line patch. Out of scope for the four-phase response
noise fixes.

## What

The `<historical record>` block at the bottom of `CLAUDE.md` (written
during bootstrap close per the init flow) claims "Bootstrap completed
on YYYY-MM-DD with session-id …" and lists adopted hostnames. But the
actual ServiceMeta files on disk (`<stateDir>/services/<host>.json`)
can be missing — the session was abandoned, the meta was never written
or got cleaned up — while the historical record stays in CLAUDE.md.

Agent quote (verify-subdomain): "The historical record at the bottom of
CLAUDE.md said appdev had been adopted in a session with ID
`5c3e2a0de217803a`. I took that at face value and tried `zerops_workflow
start workflow=develop` first. It failed with `PREREQUISITE_MISSING:
No bootstrapped services found`. When I then ran bootstrap, it gave me
back the *same* session ID — so the prior session existed in some form
but the ServiceMeta wasn't actually attached to the service."

Both retros conclude with "don't trust the historical log; run discover
+ bootstrap if develop rejects you". That's the right recovery rule, but
the lying state shouldn't exist in the first place.

## Trigger to promote

Promote when work touches CLAUDE.md write paths (init / bootstrap close)
OR when an eval shows an agent making a worse decision than just
"run bootstrap again" because of stale history. Two scenarios in one
run is already a soft signal.

## Sketch

Three possible directions, escalating cost:

1. **Derive history block at read time, don't store it.** The historical
   record is recomputable from existing ServiceMeta + reflog. CLAUDE.md
   only carries the live derivation (or a static template that says
   "see `zerops_discover` for current adopted services"). Simplest;
   eliminates the desync class entirely.

2. **Stamp the history block with a freshness gate.** When CLAUDE.md is
   written during bootstrap close, also stamp the file with the current
   ServiceMeta digest. On read, if digest mismatches, hide the historical
   record. Mid-cost; preserves the existing UX shape.

3. **Atomic close**: bootstrap close transactionally writes both
   ServiceMeta AND CLAUDE.md history; abort+rollback both on any
   failure. Prevents NEW desyncs but doesn't repair existing ones.

Option (1) is probably right — the history block adds friction
(misleads) more than it adds value (agents already check
`zerops_discover` per CLAUDE.md guidance).

## Risks

- Removing or restructuring the history block in CLAUDE.md is a
  visible UX change. Worth checking with the user/Aleš whether this
  block is consumed by anything other than agent reading.
