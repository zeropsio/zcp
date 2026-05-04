# `nextActions: zerops_logs severity=ERROR` after a successful deploy

**Surfaced:** 2026-05-04, eval suite `20260504-065807` `classic-static-nginx-simple`
retro. Same finding earlier in `classic-go-simple` 211240 retro.

**Why deferred:** noted during the four-phase fix planning but explicitly
out of scope (Phase 1 was probe-skip only; this is a sibling field that
also misdirects post-deploy).

## What

After a successful `zerops_deploy`, the response's `nextActions` field
unconditionally suggests `zerops_logs severity=ERROR since=5m`. On a
clean deploy that's misdirection — the right next move is `zerops_verify`
(or `zerops_dev_server start` for dev-mode dynamic).

Agent quote (static-nginx 20260504): "That's misleading on a clean
deploy — the right next action is `zerops_verify`, not log inspection."

## Trigger to promote

Promote when the journey-shape work begins, OR if the next eval still
flags this. Cheap surgical fix on its own — could ship between phases.

## Sketch

Source: `internal/tools/next_actions.go` (or wherever `NextActions` is
composed for `DeployResult`). Make the suggestion outcome-aware:

- `result.Status == DEPLOYED` AND no failureClassification AND no
  warnings → suggest `zerops_verify` (or `zerops_dev_server start` for
  dev-mode dynamic, mirroring the deferred-start predicate from Phase 1
  `topology.IsDeferredStart`).
- Failure paths → keep the log-inspection suggestion (it's correct
  there).

The Phase 1 `IsDeferredStart` predicate is already the right gate for
the dev-server-vs-verify split.

## Risks

- Need to make sure `next_actions.go` has access to (mode, runtimeClass)
  at composition time. If not, requires the same plumbing the Phase 1
  fix does in `deploy_subdomain.go`.
