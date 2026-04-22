---
surface: gotcha
verdict: fail
reason: self-inflicted
title: "zsc execOnce records a successful seed that produced zero output (v28 apidev gotcha #1)"
---

> ### `zsc execOnce` can record a successful seed that produced zero output
>
> Our seed script exited 0 without inserting any rows because of a silent
> JSON-parse failure. `zsc execOnce` honored the exit code and recorded the
> deploy as seeded. Next deploy saw the stale `appVersionId` marker and
> skipped the seed. Result: no seed data after two deploys.

**Why this fails the gotcha test.**
The mechanism described (`zsc execOnce` gating on exit code) is what
`zsc execOnce` is documented to do. The actual bug was in the seed script —
it should have failed loudly on empty inserts. A reasonable porter bringing
their own seed script will not ship a silent-exit-zero script, so there is
nothing Zerops-specific to teach here.

**Correct routing**: DISCARD. Fix the seed script; do not publish as a gotcha.
Classification: self-inflicted (see `docs/spec-content-surfaces.md` §7).
