# adopt plan-type — show the composite spelling siblings already use

**Surfaced**: 2026-06-17, flow-eval `git-push-setup-then-actions` retrospective
(suite 20260617-171651). `zerops_discover` shows OS-prefixed types (`ubuntu/nodejs@22`,
`alpine/nodejs@22`) but the adopt plan `type` field wants the bare `nodejs@22`. The
agent guessed (the detailedGuide template showed `nodejs@22`, so it followed) and it
worked — but it was guessing.

**Why deferred**: severity is low for fix-now. The bare type VALIDATED and worked
(the CHECK is form-tolerant), the PRIMARY path (`adoptPairingChoice` reject) already
hands a correct composite paste template, and there's no quantified eval evidence of
recurring friction — one agent guessed once and self-corrected. The naive fix (add a
"bare form is also accepted" clause, or a source-pointer placeholder) would LEAK the
validation set into the presentation set — exactly the anti-pattern.

**Trigger to promote**: a second eval where an agent passes the OS-prefixed type and
gets a confusing rejection, OR a report that the form mismatch cost a round-trip.

**Sketch**: in the two adopt atoms, replace any bare-literal type examples with the
concrete composite spelling the siblings already use (`alpine/nodejs@22`,
`postgresql:single@18`) so the agent copies the right shape instead of inferring it.
Scope any "no bare literal" drift-pin to ADOPT atoms only — `classic` legitimately
hardcodes bare (it CREATES; no discover source to mirror).

**Refs**: plans/minor-findings-rootcause-2026-06-17.md (F5, dropped-from-fix-now).
