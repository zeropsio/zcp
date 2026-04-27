---
id: develop-deploy-files-self-deploy
priority: 3
phases: [develop-active]
title: "Self-deploy requires deployFiles: [.] — narrower patterns destroy the target"
---

### Self-deploy invariant

Any service self-deploying MUST have `deployFiles: [.]` or `[./]` in
the matching setup block. See `develop-deploy-modes` for how ZCP
classifies self-deploy vs cross-deploy.

A narrower pattern destroys the target's working tree on the next
deploy:

1. The build container assembles the artifact from the upload + any
   `buildCommands` output.
2. `deployFiles` selects — with a cherry-pick pattern like `[./out]`,
   only the selected subset enters the artifact.
3. The runtime container's `/var/www/` is **overwritten** with that subset —
   source files disappear.
4. On subsequent self-deploys, `zerops_deploy` finds no source to
   upload — the target is unrecoverable without a manual re-push from
   elsewhere.

Client-side pre-flight rejects this with
`INVALID_ZEROPS_YML` before any build triggers, so this failure mode
cannot reach Zerops.

Cross-deploy has opposite semantics; see `develop-deploy-modes` for
the full contrast.
