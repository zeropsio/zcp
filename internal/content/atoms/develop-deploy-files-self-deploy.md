---
id: develop-deploy-files-self-deploy
priority: 3
phases: [develop-active]
title: "Self-deploy requires deployFiles: [.] — DM-2"
---

### Self-deploy invariant (DM-2)

Any service self-deploying (`sourceService == targetService` — the
default when sourceService is omitted; typical pattern for dev services
and simple mode) MUST have `deployFiles: [.]` or `[./]` in the matching
setup block.

A narrower pattern destroys the target's working tree on the next
deploy:

1. Build container assembles the artifact from the upload + any
   `buildCommands` output.
2. `deployFiles` selects — with a cherry-pick pattern like `[./out]`,
   only the selected subset enters the artifact.
3. Runtime container's `/var/www/` is **overwritten** with that subset —
   source files disappear.
4. On subsequent self-deploys, `zcli push` finds no source to upload —
   the target is unrecoverable without a manual re-push from elsewhere.

Client-side pre-flight rejects DM-2 violations with
`INVALID_ZEROPS_YML` before any build triggers, so this failure mode
cannot reach the platform.

### Cross-deploy has opposite semantics

Cross-deploy (`sourceService != targetService`, or
`strategy=git-push`) ships build output to a **different** service —
source is not at risk. Cross-deploy's `deployFiles` typically
cherry-picks (`./out`, `./dist`, `./build`) and DM-2 does NOT apply.
See `develop-deploy-modes` atom for the full contrast.

**Reference**: `docs/spec-workflows.md` §8 Deploy Modes.
