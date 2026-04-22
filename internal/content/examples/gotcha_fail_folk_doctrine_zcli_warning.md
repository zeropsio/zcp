---
surface: gotcha
verdict: fail
reason: folk-doctrine
title: "Benign zcli warning reassurance (v38 appdev CRIT #4)"
---

> ### Benign zcli build-log warning
>
> The `zcli` warning "dist/~ paths not found" shows up in build logs
> regardless of whether the deploy succeeded. You can safely ignore it —
> it's a known cosmetic issue that does not affect deployment.

**Why this fails the gotcha test.**
This is invented reassurance. The author saw a warning, could not explain
it, and wrote a "safe to ignore" gotcha to cover the confusion. The
`deploy-files` guide explains the actual mechanism — the warning fires
when the tilde-stripped base directory is actually missing, and when it
fires on a successful deploy it means the platform is about to silently
drop the tilde semantic. Shipping "safely ignore" guidance makes the
real failure mode harder to debug.

**Correct routing**: either (a) DISCARD entirely if the warning never
blocks a deploy in this recipe, OR (b) cite the `deploy-files` guide and
teach the actual mechanism (when `./dist/~` must exist; what the tilde
strip does). Never ship "safely ignore" prose for a warning whose root
cause the author does not understand.
