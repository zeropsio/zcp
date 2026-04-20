---
id: bootstrap-resume
priority: 1
phases: [idle]
idleScenarios: [incomplete]
title: "Resume interrupted bootstrap"
---

### Interrupted bootstrap detected

The project has at least one runtime service whose `ServiceMeta` is
tagged with a `BootstrapSession` but carries no `BootstrappedAt`. That
means a bootstrap session started, wrote partial metadata, then died
before reaching the close step. ZCP owns those service slots via the
recorded session — **do not classic-bootstrap over them**, or a new
session will clash with the orphaned records.

**Options, in priority order:**

1. **Resume the session** — the preferred path. Call discovery first:
   ```
   zerops_workflow action="start" workflow="bootstrap" intent="<anything>"
   ```
   The response includes a `resume` option with `resumeSession` (the
   session ID to pick up) and `resumeServices` (the hostnames that
   will be reclaimed). Dispatch with:
   ```
   zerops_workflow action="start" workflow="bootstrap" route="resume" sessionId="<resumeSession>"
   ```
   The engine claims the session from the dead PID and hands you back
   at the step that was in flight when it died.

2. **Reset and restart** — if the incomplete metadata is stale (the
   original bootstrap was abandoned deliberately, or the services are
   wrong for the current task), delete the orphan metas first and
   then run a fresh discovery. The metas live under
   `.zcp/state/services/<hostname>.json` — removing them clears the
   `BootstrapSession` tag, after which the services become adoptable
   in the normal flow.

Either way, **never** use `route="classic"` on a project with incomplete
metadata. Classic ignores the lock and the new plan's hostnames will
collide with the orphan metas at provision time.
