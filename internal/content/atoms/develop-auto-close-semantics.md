---
id: develop-auto-close-semantics
priority: 4
phases: [develop-active, develop-closed-auto]
title: "Work session auto-close semantics"
references-fields: [workflow.WorkSessionSummary.ClosedAt, workflow.WorkSessionSummary.CloseReason, workflow.StateEnvelope.Phase, workflow.ServiceSnapshot.CloseDeployMode]
---

### Work session auto-close

Auto-close fires only when EVERY in-scope service carries `closeDeployMode ∈ {auto, git-push}` AND has a successful deploy + a passing verify that ran AFTER that deploy (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). Re-deploying re-opens verify: a deploy replaces the running app version, so a verify that passed before it no longer describes what is live — re-verify after the latest deploy. `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

Scope follows session topology — standard pairs include both halves. For dev-only work pass `outOfScope=["<stage>"]` on develop start; the stage half drops to a non-blocking reminder and the session closes on the dev half alone.
