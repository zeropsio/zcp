---
id: develop-closed-auto
priority: 1
phases: [develop-closed-auto]
title: "Develop auto-closed — next step"
references-fields: [workflow.StateEnvelope.Phase, workflow.WorkSessionSummary.ClosedAt, workflow.WorkSessionSummary.CloseReason]
references-atoms: [develop-auto-close-semantics]
---

The envelope's `phase: develop-closed-auto` and `closeReason:
auto-complete` are set; full close criteria are in
`develop-auto-close-semantics`. Work is durable — code is in git,
infrastructure on Zerops.

Next actions:

```
zerops_workflow action="start" workflow="develop" intent="{next-task}"
zerops_workflow action="close" workflow="develop"
```

Full auto-close and explicit-close semantics:
`develop-auto-close-semantics`.
