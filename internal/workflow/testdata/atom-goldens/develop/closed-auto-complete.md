---
id: develop/closed-auto-complete
atomIds: [develop-closed-auto, develop-auto-close-semantics]
description: "develop-closed-auto phase, close reason auto-complete (all services deployed and verified)."
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

---

### Work session auto-close

Auto-close fires only when EVERY in-scope service carries `closeDeployMode ∈ {auto, git-push}` AND has a successful deploy + a passing verify that ran AFTER that deploy (`closeReason: auto-complete`; or `iteration-cap` at the retry ceiling — same `ClosedAt`/`CloseReason` shape). Re-deploying re-opens verify: a deploy replaces the running app version, so a verify that passed before it no longer describes what is live — re-verify after the latest deploy. `unset` / `manual` services BLOCK it: the session stays open until you set a close-mode or call `action="close"` explicitly.

Scope follows session topology — standard pairs include both halves. For dev-only work pass `outOfScope=["<stage>"]` on develop start; the stage half drops to a non-blocking reminder and the session closes on the dev half alone.
