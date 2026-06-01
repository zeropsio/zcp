---
id: develop-auto-close-semantics
priority: 4
phases: [develop-active, develop-closed-auto]
title: "Work session auto-close semantics"
references-fields: [workflow.WorkSessionSummary.ClosedAt, workflow.WorkSessionSummary.CloseReason, workflow.StateEnvelope.Phase, workflow.ServiceSnapshot.CloseDeployMode]
---

### Work session auto-close

Auto-close is gated on every in-scope service carrying `closeDeployMode ∈ {auto, git-push}`. Services with `closeDeployMode=unset` or `closeDeployMode=manual` BLOCK the auto-close trigger — the session stays open until you either pick a close-mode for those services or call `action="close"` explicitly.

When the gate is open, the session closes automatically when either:

- **`auto-complete`** — every service in scope has both a successful
  deploy and a passing verify; `closeReason: auto-complete`.
- **`iteration-cap`** — the workflow's retry ceiling was hit; same
  close-state shape, `closeReason: iteration-cap`.

Explicit `zerops_workflow action="close" workflow="develop"` emits
the same closed state manually and is rarely needed — starting a new
task with a different `intent` replaces the session.

Close scope follows the session topology: standard-mode pairs include
BOTH halves by default. For dev-only work ("leave staging as it is"),
pass `outOfScope=["<stage>"]` on develop start — the stage half drops to
a non-blocking reminder and the session closes on the dev half alone.
Dev-only or simple services close after one successful deploy + verify.

Close is cleanup, not commitment — work is durable in git + on Zerops.
