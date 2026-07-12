# CI self-target `-g` deploy wipes container git wiring; recall + push-proof mask it

**Surfaced**: 2026-07-12 — L1/CI-CD trace during the git-contract review
(`plans/git-sdlc-review-2026-07-12.md` §7a; found by code trace, not yet
live-reproduced).

**Why deferred**: pre-existing latent bug, orthogonal to the P1/P2/P4
contract fix that shipped; needs its own design pass (post-CI reconcile vs
probe-based recall) and a live CI round-trip to confirm the exact wipe
surface before choosing the fix layer.

**Trigger to promote**: any live report of "git-push worked, then broke
after a CI build" on a self-target pair; or the first flow-eval that runs a
full actions-integration loop on a simple/self-target service.

## The defect chain (one root cause, three symptoms)

1. A GitHub-Actions build of a SELF-target service ships `-g` — but that is
   the RUNNER's fresh `actions/checkout@v4` clone `.git`, not the
   container's. The deploy replaces `/var/www/.git`, silently dropping the
   URL-scoped credential helper (`credential.https://<host>.helper`) and any
   seeded identity. Identity self-heals (set-if-absent runs on every direct
   deploy + setup), the credential helper does NOT — it is only ever written
   by git-push-setup's origin-sync.
2. `gitPushConfiguredRecall` checks only `test -d /var/www/.git`
   (existence), so a tokenless re-call reports "already-configured" on the
   wiped repo without reconstructing the wiring.
3. The launch push-proof (`BuildGitAuthedLsRemoteCommand`) carries `|| true`
   — a broken credential helper reads as EMPTY remote SHA, which the gate
   misclassifies as `head-not-pushed` ("push your code") instead of broken
   credential wiring.

## Sketch

Options, roughly in preference order: (a) recall stops trusting existence —
probe the helper (`git config --get credential.https://<host>.helper`) and
re-assert wiring when absent (origin-sync is idempotent, single owner
already exists); (b) launch push-proof drops the `|| true` masking and
classifies auth failure distinctly from empty-remote; (c) the emitted
Actions workflow re-asserts wiring post-deploy (worst — third writer).

**Refs**: `internal/ops/git_credential.go` (helper fragment + comment about
`-g` riding), `internal/tools/workflow_git_push_setup.go`
(`gitPushConfiguredRecall`), `internal/tools/launch_source_control_gate.go`
(push-proof read), `internal/tools/workflow_build_integration.go`
(actionsWorkflowYAML `-g` variant).
