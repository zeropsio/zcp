---
id: bootstrap-resume
priority: 1
phases: [idle]
idleScenarios: [incomplete]
title: "Resume interrupted bootstrap"
references-fields: [workflow.StateEnvelope.IdleScenario, workflow.ServiceSnapshot.Resumable, workflow.BootstrapRouteOption.ResumeSession, workflow.BootstrapRouteOption.ResumeServices]
---

### Interrupted bootstrap detected

The envelope reports `idleScenario: incomplete` — the project has at
least one runtime service whose snapshot carries `resumable: true`,
meaning a prior bootstrap session wrote partial state and died
before close. **Do not classic-bootstrap over these services** — a
new session will clash with the existing partial records.

**Options, in priority order:**

1. **Resume the session** — the preferred path. Call discovery first:
   ```
   zerops_workflow action="start" workflow="bootstrap" intent="<anything>"
   ```
   Read `routeOptions[]` — the `resume` entry carries `resumeSession`
   (the session ID to pick up) and `resumeServices` (the hostnames
   that will be reclaimed). Dispatch with:
   ```
   zerops_workflow action="start" workflow="bootstrap" route="resume" sessionId="<resumeSession>"
   ```
   Resume picks up at the step that was in flight when the earlier
   session ended.

2. **Abandon and restart** — if the partial state is stale (the
   original bootstrap was abandoned deliberately, or the services are
   wrong for the current task), delete the orphan files under
   `.zcp/state/services/<hostname>.json`. The services then become
   adoptable in the normal flow.

Either way, **never** use `route="classic"` on a project with
`resumable: true` snapshots. Classic ignores the lock and the new
plan's hostnames will collide with the orphan records at provision
time.
