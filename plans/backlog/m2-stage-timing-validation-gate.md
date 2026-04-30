# Stage-entry timing validation gate (M2)

**Surfaced**: 2026-04-29 — `docs/audit-prerelease-internal-testing-2026-04-29.md`
finding M2. `internal/ops/deploy_validate.go` (lines around the per-role
shape warnings — `:110+116+121` per audit) validates per-role shape but
doesn't enforce the "stage entry written AFTER dev verified" sequence.
If the agent writes both upfront, deploy succeeds and the spec's safety
story ("dev verified before stage exists") is broken without alarm.

**Why deferred**: structural fix — needs session-scoped "have we seen a
successful dev verify yet?" gate. Doesn't surface unless the agent skips
the prescribed sequence; the dev→stage promote-stage atom guides the
correct path. The audit categorized this MEDIUM specifically because it's
"guidance only, no enforcement" — internal testing will surface whether
agents actually skip.

**Trigger to promote**: a live-agent run where the agent wrote both setup
blocks upfront AND the resulting deploy missed an issue the staged
sequence would have caught (e.g. dev verify revealed a config issue the
agent missed because stage hid it). Or a recipe that legitimately needs
both blocks at bootstrap and runs into the gate.

## Sketch

Session-scoped check at `ValidateZeropsYml` time:

1. When `target.role == DeployRoleStage`, look up the dev half via
   `meta.PairFor(target)` / `FindServiceMeta`.
2. Read the work session: has the dev half logged a successful verify
   attempt (`HasSuccessfulVerifyFor(ws, devHostname)`)?
3. If no: refuse with `ErrPrerequisiteMissing`, suggestion =
   "Verify {devHostname} successfully before deploying {stageHostname};
   run zerops_verify serviceHostname=\"{devHostname}\" first."
4. Skip the gate when WorkSession is nil (workflow-less zerops_deploy
   call — no session-state to consult).

## Risks

- Recipe-bootstrap path writes both setups during recipe-import; the
  gate would need to skip during recipe-time deploys (use phase /
  workflow detection).
- Adopted services where the user has a long deploy history outside
  ZCP — they may legitimately deploy stage without a session-tracked
  dev verify. The `HasSuccessfulVerifyFor` predicate would need to
  also accept "dev half is currently `Status=ACTIVE`" as a proxy.
- Doubles the value of meta-pair-keying (E8 invariant). Confirm no
  edge cases where dev and stage have separate meta files.

## Refs

- Audit M2 verified at HEAD `9669ebb5`:
  `internal/ops/deploy_validate.go:110+116+121`.
- Spec D2d (standard-mode first-deploy promote-stage atom) covers the
  guidance side but doesn't enforce sequencing.
