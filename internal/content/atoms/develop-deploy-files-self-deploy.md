---
id: develop-deploy-files-self-deploy
priority: 3
phases: [develop-active]
closeDeployModes: [auto, manual, unset]
title: "Self-deploy destruction risk — narrower deployFiles destroys the target"
---

### Self-deploy destruction risk

`develop-deploy-modes` covers the high-level self-deploy vs cross-deploy classification + the deployFiles table. This atom drills into the specific destruction-risk path that motivates the `[.]` invariant.

When a self-deploying service uses a narrower deployFiles pattern (e.g. `[./out]`):

1. The build container assembles the artifact from the upload + any `buildCommands` output.
2. `deployFiles` selects — with a cherry-pick pattern, only the selected subset enters the artifact.
<!-- axis-n-keep -->
3. The runtime container's `/var/www/` is **overwritten** with that subset — source files disappear.
4. On subsequent self-deploys, `zerops_deploy` finds no source to upload — the target is unrecoverable without a manual re-push from elsewhere.

Client-side pre-flight rejects this with `INVALID_ZEROPS_YML` before any build triggers, so this failure mode cannot reach Zerops. (The atom fires for `closeDeployModes:[auto, manual, unset]` because git-push delivery uses cross-deploy semantics where this risk class doesn't apply.)
