**Surfaced**: 2026-05-16 — Karel's live launch-production test created prod project `zAho8ZKISe6PLFUFpERsyw` successfully (composer + import all OK), then the platform-side build process `SVjB0KASSJGutVdoYZREGg` for `appdev` failed in **0.32 seconds** with `appVersion.status: WAITING_TO_BUILD`, `build.pipelineStart: null` — builder container never started. Project logs show only the db service running; no `zbuilder@...` entries.

**Why deferred**: not a ZCP bug — Phase 2 composer + import flow worked end-to-end. The first-deploy failure is platform-side (builder queue / clone preflight / quota). ZCP's `first-deploy-failed` blocker is correct to surface it; what's missing is an actionable recovery hint.

**Trigger to promote**: another live launch hits the same "process FAILED 0.32s with no pipeline lifecycle markers" pattern, OR a user is left puzzled because the response says "first deploy did not complete cleanly" without telling them what to do next.

---

## What ZCP says today

```json
{
  "status": "failed",
  "blockers": [
    {
      "id": "first-deploy-failed",
      "severity": "block",
      "category": "other",
      "message": "Target project <id> created but first deploy did not complete cleanly: service appdev: process <pid> terminal status FAILED (FAILED)"
    }
  ]
}
```

The user doesn't know whether to:
- Retry the build (push a fresh commit / trigger redeploy)
- Wait for platform queue to drain
- Inspect platform-side logs
- Delete the project and re-launch

## Sketch

Promote the blocker to carry structured `Recovery` (same pattern as the verify-step recovery hints already shipped). Two recovery options to surface:

1. **Retry via push** — if buildFromGit is set, tell the user to push a new commit to the source repo: `git commit --allow-empty -m "retry build" && git push origin main`. Platform picks up the new ref and re-triggers build.
2. **Inspect platform logs** — point at `zerops_logs service=<runtime> source=build` for the failed process. Today the project's general log doesn't carry build events when the builder container never started; a dedicated `service=build` lookup may be needed.

Mechanical change:
- `internal/tools/workflow_launch_production.go` first-deploy-failed blocker emit site gains:
  ```go
  Recovery: &topology.Recovery{
      Tool:   "zerops_deploy",
      Action: "redeploy",
      Args:   map[string]any{"service": targetServiceHostname, "projectId": targetProjectID},
  }
  ```
  AND a structured `nextSteps[]` listing the push-to-retry option.

## Open question

Whether Phase 6b's "production launch terminal phase" (which already emits cicd handoff atoms) should subsume this — i.e. teach the launch terminal to re-fire on retry rather than treating it as a leaf failure. Less urgent than the explicit Recovery hint, but worth considering when the terminal phase is reshaped.

## Refs

- Karel live failure: process `SVjB0KASSJGutVdoYZREGg` on project `zAho8ZKISe6PLFUFpERsyw`, service `V3OpcEFLSQOHX6HbLBt7Tw` (appdev), buildFromGit `https://github.com/krls2020/eval2.git@main` commit `d9b561e1ab9de7047603ed21d9fc1d0f11f73b0d`. Same pattern earlier with `xBzcL6OpQqWiGPDDAOLTyA` on `YfiA4Q9IRd2IiEZnHD7d1g` (already deleted).
- AppVersion DTO at the failure: `id=XzSk2z2hRuu7Ybpsr8NNTg`, `status: WAITING_TO_BUILD`, every `build.*` timestamp `null`.
- Session transcript: `/tmp/karel-launch-prod.jsonl` (~263 events).
