---
id: develop-git-push-broken
priority: 2
phases: [develop-active]
gitPushStates: [broken]
closeDeployModes: [auto, unset]
modes: [standard, simple, local-stage, local-only]
deployStates: [deployed]
multiService: aggregate
title: "Git-push capability broke — repair it before pushing"
references-atoms: [develop-git-push-delivery, setup-git-push-container, setup-git-push-local]
---

Git-push delivery was configured for this service, but the capability is now `gitPush=broken` — a previously-working credential stopped authenticating (typical cause: the token was rotated or revoked upstream). Pushing now will be rejected by `zerops_deploy strategy="git-push"` pre-flight (`PREREQUISITE_MISSING`).

Repair runs the same setup action — ask the user for a fresh token (never invent one) and re-run:

```
{services-list:zerops_workflow action="git-push-setup" service="{hostname}" gitToken="<fresh PAT>"}
```

The setup probe re-verifies end-to-end auth against the recorded remote and rewrites the credential in place — no manual cleanup of the previous state is needed. `git-push-setup` is per-pair (one call from the dev half repairs capability for both halves of a standard pair); once `gitPush=configured` again, the next develop response delivers the push command.
