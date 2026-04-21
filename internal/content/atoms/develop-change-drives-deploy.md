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

The work session auto-closes once every service in scope has a successful
deploy **and** a passed verify — that is the objective "task done"
signal. Explicit `zerops_workflow action="close" workflow="develop"` is
the "I'm done here" signal; close always succeeds (it's session cleanup,
not commitment) and is rarely needed because starting a new task with a
different `intent` auto-closes the prior session.
