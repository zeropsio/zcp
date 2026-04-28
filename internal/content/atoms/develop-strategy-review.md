---
id: develop-strategy-review
priority: 2
phases: [develop-active]
deployStates: [deployed]
closeDeployModes: [unset]
title: "Pick an ongoing close-mode"
---

### Pick an ongoing close-mode

The first deploy landed and verified. Before iterating, confirm how the develop workflow should close future tasks:

- `auto` — ZCP runs `zerops_deploy` directly at close (current default). Fast for tight iteration cycles.
- `git-push` — close commits + pushes to a configured git remote. Zerops or your CI picks the push up and builds. Requires `git-push-setup` first.
- `manual` — **you** orchestrate close. ZCP yields; your slash commands / hooks / external loop own the deploy/verify/close decisions.

```
zerops_workflow action="close-mode" closeMode={"{hostname}":"auto"}
```

Replace `auto` with `git-push` or `manual` to match your workflow. Switching to `git-push` returns chained guidance pointing at `action="git-push-setup"` to provision GIT_TOKEN / .netrc / remote URL. The build integration (webhook / actions) is independent — wire it via `action="build-integration"` whenever git-push capability lands.

Pick explicitly before iterating; the default keeps working but committed close-modes drive the post-first-deploy auto-close gate.
