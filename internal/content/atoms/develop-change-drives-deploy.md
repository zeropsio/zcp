---
id: develop-change-drives-deploy
priority: 2
phases: [develop-active]
title: "Every code change must flow through the deploy strategy"
references-fields: [workflow.WorkSessionSummary.CloseReason]
references-atoms: [develop-auto-close-semantics]
---

### Every code change must flow through the deploy strategy

Editing files on the SSHFS mount (or locally in local mode) persists
only inside the current container — the next deploy rebuilds the
container from scratch, and anything not covered by `deployFiles` is
discarded. The rule is:

> **edit → deploy (via active strategy) → verify**

Auto-close semantics are described in `develop-auto-close-semantics`;
`closeReason` values you can observe are `auto-complete` (every
in-scope service passed) and `iteration-cap` (retry ceiling hit).
Explicit `zerops_workflow action="close" workflow="develop"` emits
the same closed state; it's rarely needed because starting a new task
with a different `intent` replaces the session. Session close is
cleanup, not commitment — close always succeeds.
