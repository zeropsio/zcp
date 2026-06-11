# mode-unsupported-api recovery hint misdirects to adopt (which can't create the stage half)

**Surfaced**: 2026-06-11, flow-eval `launch-production-dev-only` (suite
20260611-183954) — launch-production rejected the dev-mode runtime as a push
source via the `mode-unsupported-api` blocker, whose recovery text told the
agent to "re-run bootstrap with route=adopt and a plan target carrying
`isExisting=true` + `bootstrapMode=standard` + an explicit `stageHostname`".
The adopt route then refused (`stage runtime apistage not found in project`)
because adopt only validates against LIVE services — it cannot CREATE the
missing stage half. The agent burned a round-trip, reset the session, and
restarted with route=classic, which is the correct path for expanding a
dev-only runtime to a standard pair.

**Why deferred**: out of scope of the single-token launch lifecycle ship; the
fix lives in the blocker's recovery text (and possibly the adopt-route plan
validation diagnostic, which could itself point at classic when a plan names a
non-live stageHostname). Needs a quick check of parallel emitters (the same
adopt-with-new-stage advice may exist in the develop-mode-expansion atom and
the launch source-control chain).

**Trigger to promote**: next launch/bootstrap eval round-trip, or any repeat
of the same misdirect in an eval retrospective.

**Sketch**: recovery hint for mode-unsupported push sources should chain
route=classic (creates the stage service) — or the adopt diagnostic for a
missing stageHostname should carry the "switch to classic" fork explicitly.
Self-review with full friction narrative:
`eval/behavioral/runs/20260611-183954/launch-production-dev-only/self-review.md`.
