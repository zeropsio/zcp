---
id: develop-change-drives-deploy
priority: 2
phases: [develop-active]
title: "Every code change must flow through the deploy strategy"
---

### Every code change must flow through the deploy strategy

Editing files on the SSHFS mount (or locally, for local mode) only
persists **inside the current container**. A deploy rebuilds the
container from scratch — anything not covered by `deployFiles` is
discarded. Rule:

> **edit → deploy (via active strategy) → verify**

If you made code changes, run the strategy-specific `zerops_deploy`
before closing the work session. `zerops_workflow action="close"`
will warn when it sees no successful deploy recorded in the session —
pass `force=true` only for non-code tasks (investigation, `zerops_env`
tweaks, abandoned experiments).

The work session auto-closes once every service in scope has a
successful deploy **and** a passed verify. That is the normal
task-complete signal; explicit close is only needed when skipping
deploy intentionally or when auto-close didn't fire (e.g. partial
coverage).
