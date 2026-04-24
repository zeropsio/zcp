---
id: idle-develop-entry
priority: 1
phases: [idle]
idleScenarios: [bootstrapped]
title: "Develop entry"
references-fields: [workflow.StateEnvelope.Phase, workflow.WorkSessionSummary.Deploys, workflow.WorkSessionSummary.Verifies]
---

The project has at least one bootstrapped service ready to receive
code. Start a develop session:

```
zerops_workflow action="start" workflow="develop" intent="{task-description}" scope=["{hostname}",…]
```

The envelope will flip to `phase: develop-active`; subsequent status
calls show `workSession.deploys[]` and `workSession.verifies[]` as
you iterate. Auto-close semantics: `develop-auto-close-semantics`.
