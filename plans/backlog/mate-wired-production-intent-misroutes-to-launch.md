# A wired Mate's launch-production refusal — what is still open

**Surfaced**: 2026-09-23 — in group `fsadfdasfsa` "muzes to dat na produkci ?" started
launch-production while `055a7e8 Mate: weatherdev (#4)` was merged and waiting for _Release_. The
fix is described in `spec-mate.md` §10.10 (_A wired Mate's production is the group's_) and
MB-31: `handleLaunchProduction` refuses in a wired Mate with
`wired_mate_production_is_the_groups` and a freshened pull-request next step, and
`idle-launch-entry` branches on the wired state. This entry keeps only what did not land.

**Trigger to promote**: the next change to launch-production or to the envelope's atom axes.

## Not done
- **No wired-idle golden.** `StateEnvelope` carries no field for the Gitea wiring, so the golden
  harness has no axis to drive a scenario that differs by it; the atom's wired branch is prose
  inside `idle/bootstrapped-with-managed`. A dedicated scenario needs a wiring field on the
  envelope, computed in `compute_envelope.go`.

## Open
- **The release switch (D8) gates nothing an agent can reach.** No zcp tool tags the group repo:
  `action="release"` (`workflow_release.go`) pushes a `v*` tag from the checkout to the pair's own
  remote (`meta.RemoteURL`) and has no wired branch, and the group-repo writers
  (`internal/ops/gitea_group.go`, `gitea_recipe_reconcile.go`) publish recipe content only. Decide
  whether a wired Mate gets a group-release action behind D8, or D8 is broker-side only. Until
  then the atom does not mention it.
