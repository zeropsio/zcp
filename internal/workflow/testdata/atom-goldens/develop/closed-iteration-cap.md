---
id: develop/closed-iteration-cap
atomIds: [develop-closed-auto, develop-auto-close-semantics]
description: "develop-closed-auto phase, close reason iteration-cap — workflow exhausted retry budget without success."
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
