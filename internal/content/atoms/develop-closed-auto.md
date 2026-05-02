---
id: develop-closed-auto
priority: 1
phases: [develop-closed-auto]
title: "Develop auto-closed — next step"
references-fields: [workflow.StateEnvelope.Phase, workflow.WorkSessionSummary.ClosedAt, workflow.WorkSessionSummary.CloseReason]
references-atoms: [develop-auto-close-semantics]
---

The envelope's `phase: develop-closed-auto` is set. The session was closed automatically by one of two close mechanisms — read `workSession.closeReason` from the envelope to know which: `auto-complete` (every in-scope service deployed and verified) OR `iteration-cap` (workflow exhausted its retry budget).

`auto-complete` is the success path: work landed cleanly. Pick a new task and start the next session.

`iteration-cap` is the give-up path: the same fix kept failing. Before starting a new session, **inspect `workSession.deploys[].reason`** for the recurring failure — repeating the same approach with the same intent re-hits the cap. If multiple iterations failed for the same reason (build base mismatch, env-var name drift, port mismatch), fix the root cause first; if iterations failed for *different* reasons, the task may be too broad — split it.

Either way, work is durable: code is in git, infrastructure is on Zerops.

Next actions:

```
zerops_workflow action="start" workflow="develop" intent="{next-task}"
zerops_workflow action="close" workflow="develop"
```
