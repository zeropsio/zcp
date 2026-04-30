# Propagate FailureClass into TimelineEvent (async build path)

**Surfaced**: 2026-04-29 — `docs/audit-prerelease-internal-testing-2026-04-29.md`
finding C3. Atoms (`develop-build-observe`, `events.go:94` BUILD_FAILED hint)
tell the agent to read `failureClass` + `description` on `TimelineEvent`, but
the field doesn't exist — `TimelineEvent` (`internal/ops/events.go`) carries
only `FailReason string` + `Hint string`. The classifier (`topology.FailureClass`)
DOES exist and is wired into synchronous deploy responses
(`tools/deploy_poll.go`, `tools/deploy_local.go`, `tools/deploy_git_push.go`,
plus the deploy_failure.go classifier infrastructure), but the async-build
events path was never wired. Surfaces during `build-integration={webhook,actions}`
flows where the agent watches `zerops_events` for build outcomes.

**Why deferred**: closing C2 (audit P4 — git-push lifecycle separation +
record-deploy bridge) addressed the *primary* failure mode (premature
Deployed=true). C3 is about diagnostic depth on the async path. Agent's
fall-back today: read `FailReason` text. Works but loses the structured
suggestedAction the classifier provides on sync deploys.

**Trigger to promote**: live-agent feedback during internal testing where
async build failures are common and the agent's recovery loop misroutes
because `FailReason` text wasn't classifier-quality. Or once a parallel
async-event consumer (e.g. a future `zerops_events action=watch` polling
mode) needs structured failure data.

## Sketch

- Extend `internal/ops/events.go::TimelineEvent` with `FailureClass topology.FailureClass`
  + `FailureClassification *topology.DeployFailureClassification`.
- Where the platform emits build events, run the same
  `classifyDeployFailure` logic over the BUILD_FAILED event's
  `failReason`/`description` payload. Reuse `internal/ops/deploy_failure.go`
  + `deploy_failure_signals.go` so sync and async classifications come from
  ONE pattern library.
- Update the BUILD_FAILED hint at `events.go:94` to drop the "read
  failureClass" instruction unless / until the field actually exists.
- Update `develop-build-observe.md:24` accordingly.

## Risks

- Event-stream schema change — consumers that parse JSON literally need to
  handle the new field (additive, but `omitempty`-able to soften).
- Pattern library was tuned for sync deploy errors; some build-side
  failures may not match cleanly and would surface as
  `FailureClassOther`. That's still better than no classification.

## Refs

- Audit C3 verified at HEAD `9669ebb5`:
  `internal/ops/events.go:29-41` shape + `:94` hint;
  `internal/content/atoms/develop-build-observe.md:24` reference.
- Sync classifier:
  `internal/ops/deploy_failure.go` + `deploy_failure_signals.go`.
- Spec invariant: ticket E2 in
  `plans/engine-atom-rendering-improvements-2026-04-27.md` set the
  classifier-first contract for sync; async closure is the matching
  follow-up.
