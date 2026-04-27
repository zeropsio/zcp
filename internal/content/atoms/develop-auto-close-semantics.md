---
id: develop-auto-close-semantics
priority: 4
phases: [develop-active, develop-closed-auto]
title: "Work session auto-close semantics"
references-fields: [workflow.WorkSessionSummary.ClosedAt, workflow.WorkSessionSummary.CloseReason, workflow.StateEnvelope.Phase]
---

### Work session auto-close

Work sessions close automatically when either of two conditions hold:

- **`auto-complete`** — every service in scope has both a successful
  deploy and a passing verify. The envelope's `workSession.closedAt`
  becomes set, `closeReason: auto-complete`, and `phase` flips to
  `develop-closed-auto`.
- **`iteration-cap`** — the workflow's retry ceiling was hit. Same
  close-state shape; `closeReason: iteration-cap`.

Explicit `zerops_workflow action="close" workflow="develop"` emits
the same closed state manually and is rarely needed — starting a new
task with a different `intent` replaces the session.

Close scope follows the session topology: standard-mode pairs include
BOTH halves, so skipping the stage cross-deploy leaves the session
active. Dev-only or simple services close after their one successful
deploy + verify.

Close is cleanup, not commitment. Work itself is durable — code is
in git, infrastructure is on Zerops.
