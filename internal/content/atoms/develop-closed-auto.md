---
id: develop-closed-auto
priority: 1
phases: [develop-closed-auto]
title: "Develop auto-closed — next step"
references-fields: [workflow.StateEnvelope.Phase, workflow.WorkSessionSummary.ClosedAt, workflow.WorkSessionSummary.CloseReason]
references-atoms: [develop-auto-close-semantics]
---

The envelope's `phase: develop-closed-auto` is set because every
in-scope service has a successful deploy and a passing verify, and
the session's `closeReason` is `auto-complete`. Work is durable —
code is in git, infrastructure on Zerops.

Start the next task or explicitly close:

```
zerops_workflow action="start" workflow="develop" intent="{next-task}"
zerops_workflow action="close" workflow="develop"
```

Starting a new task replaces this session. Until one of those
actions happens, further deploy attempts attach to this
already-completed session. Full auto-close semantics:
`develop-auto-close-semantics`.
