---
id: develop-change-drives-deploy
priority: 2
phases: [develop-active]
title: "Every code change must flow through the deploy strategy"
references-fields: [workflow.WorkSessionSummary.CloseReason]
references-atoms: [develop-auto-close-semantics, develop-platform-rules-common]
---

### Every code change must flow through the deploy strategy

Editing files on the SSHFS mount (or locally in local mode) only
persists across deploys when covered by `deployFiles` (see
`develop-platform-rules-common` for the deploy-replaces-container
invariant). The rule is:

> **edit → deploy (via active strategy) → verify**

Auto-close semantics are described in `develop-auto-close-semantics`;
`closeReason` values you can observe are `auto-complete` (every
in-scope service passed) and `iteration-cap` (retry ceiling hit).
Explicit `zerops_workflow action="close" workflow="develop"` emits
the same closed state; it's rarely needed because starting a new task
with a different `intent` replaces the session. Session close is
cleanup, not commitment — close always succeeds.
