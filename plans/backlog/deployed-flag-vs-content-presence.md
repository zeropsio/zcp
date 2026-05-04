# `deployed: true` envelope flag is set on FirstDeployedAt stamp, not on actual content presence

**Surfaced:** 2026-05-04, eval suite `20260504-065807`
`verify-subdomain-recovery-before-browser` retro.

**Why deferred:** semantic-ambiguity finding requiring design thought,
not a one-line patch. Out of scope for the four-phase response noise
fixes.

## What

After bootstrap-adopt completes, the develop envelope reports
`appdev — bootstrapped=true, deployed=true`. But:

- `/var/www/<host>` mount has only `.git/` (no app files).
- Public subdomain returns 502 (no working content).

The `deployed` flag is computed from
`ServiceMeta.FirstDeployedAt` being non-empty, which is set by the
`record-deploy` mechanism (zcli/CI/CD bridge) OR adopt-at-ACTIVE — long
before any agent-side validation that the content actually serves.

Agent quote: "Whatever flag drives `deployed` is set by something
earlier in the service's lifecycle (FirstDeployedAt being stamped,
presumably), not by 'there's a working artifact in the runtime
container.' If a service shows `deployed=true` but the URL is broken,
treat the flag as cosmetic and check the mount."

The recovery worked but cost a re-read; the flag's name doesn't match
its semantics.

## Trigger to promote

Promote when adopt-route lifecycle work is on the table, OR if another
eval has agents acting on `deployed=true` without verifying. The
existing recipe is already "if URL is broken, treat the flag as
cosmetic" — agents are working around it correctly, but they're
working around it.

## Sketch

Two possible directions:

1. **Rename the flag** to reflect what it actually measures:
   `firstDeployStamped` or `everSawDeploy`. Loses no information,
   matches reality, no API change beyond the name. JSON consumers (the
   LLM) would read `firstDeployStamped: true` and not infer
   "content currently serves".

2. **Compute a real `deployed` from observable state** — non-empty
   `/var/www/<host>` directory + last-deploy timestamp within a sane
   window. Heavier; introduces a probe at envelope assembly. Matches
   the LLM's natural reading of `deployed: true`.

Option (1) is subtractive — fix the label, keep the mechanics. Probably
the right call.

## Risks

- Atoms reference `ServiceSnapshot.Deployed`. Rename touches atom
  `references-fields` lints — must be done with the corpus update.
- Behavioral atoms gated on `deployStates: [never-deployed]` rely on
  this flag flipping at the right moment. Renaming preserves the gate
  semantics; recomputing (option 2) might shift it.
