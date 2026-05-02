---
id: develop/closed-auto-complete
atomIds: [develop-closed-auto, develop-auto-close-semantics]
description: "develop-closed-auto phase, close reason auto-complete (all services deployed and verified)."
---
<!-- UNREVIEWED -->

The envelope's `phase: develop-closed-auto` is set. The session was closed automatically by one of two close mechanisms — read `workSession.closeReason` from the envelope to know which: `auto-complete` (every in-scope service deployed and verified) OR `iteration-cap` (workflow exhausted its retry budget).

`auto-complete` is the success path: work landed cleanly. Pick a new task and start the next session.

`iteration-cap` is the give-up path: the same fix kept failing. Before starting a new session, **inspect `workSession.deploys[].reason`** for the recurring failure — repeating the same approach with the same intent re-hits the cap. If multiple iterations failed for the same reason (build base mismatch, env-var name drift, port mismatch), fix the root cause first; if iterations failed for *different* reasons, the task may be too broad — split it.

Either way, work is durable: code is in git, infrastructure is on Zerops.

Next actions:

```
zerops_workflow action="start" workflow="develop" intent="{next-task}"
zerops_workflow action="close" workflow="develop"
```

Full auto-close and explicit-close semantics: `develop-auto-close-semantics`.

---

### Work session auto-close

Auto-close is gated on every in-scope service carrying `closeDeployMode ∈ {auto, git-push}`. Services with `closeDeployMode=unset` or `closeDeployMode=manual` BLOCK the auto-close trigger — the session stays open until you either pick a close-mode for those services or call `action="close"` explicitly. (Verified by `internal/workflow/work_session_test.go::TestEvaluateAutoClose` — `unset_blocks` and `manual_blocks` both return `want: false`.)

When the gate is open (every in-scope service is `auto` or `git-push`), the session closes automatically under either of two conditions:

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
