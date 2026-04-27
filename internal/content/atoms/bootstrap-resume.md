---
id: bootstrap-resume
priority: 1
phases: [idle]
idleScenarios: [incomplete]
title: "Resume interrupted bootstrap"
references-fields: [workflow.StateEnvelope.IdleScenario, workflow.ServiceSnapshot.Resumable, workflow.BootstrapRouteOption.ResumeSession, workflow.BootstrapRouteOption.ResumeServices]
---

### Interrupted bootstrap detected

Envelope has `idleScenario: incomplete`: at least one runtime snapshot
has `resumable: true`, meaning a prior bootstrap wrote partial state
and died before close. **Do not classic-bootstrap over these services**
— a new session collides with the partial records.

**Decision path:**

1. **Resume first.** Call discovery:
   ```
   zerops_workflow action="start" workflow="bootstrap" intent="<anything>"
   ```
   Read `routeOptions[]`; the `resume` entry carries `resumeSession`
   and `resumeServices`. Dispatch:
   ```
   zerops_workflow action="start" workflow="bootstrap" route="resume" sessionId="<resumeSession>"
   ```
   Resume continues at the interrupted step.

2. **Abandon only when stale.** If the old bootstrap was deliberately
   abandoned or the services are wrong, delete orphan files under
   `.zcp/state/services/<hostname>.json`; the services become adoptable.

Either way, **never** use `route="classic"` with `resumable: true`
snapshots. Classic ignores the lock and new hostnames collide with
orphan records at provision.
