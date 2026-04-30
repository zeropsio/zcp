---
id: develop-strategy-review
priority: 2
phases: [develop-active]
deployStates: [deployed]
closeDeployModes: [unset]
multiService: aggregate
title: "Pick an ongoing close-mode"
---

### Pick an ongoing close-mode

The first deploy landed and verified. Before iterating, declare the develop session's delivery pattern. Close-mode does not change what `action="close"` does (close is always a session-teardown call) — it picks the per-mode atoms that guide every subsequent deploy and gates auto-close:

- `auto` — agent runs `zerops_deploy` directly via zcli. Auto-close fires when scope-services are green. Fast for tight iteration cycles.
- `git-push` — agent runs `zerops_deploy strategy="git-push"` to commit + push to a configured remote. Zerops or your CI picks the push up and builds. Requires `git-push-setup` first.
- `manual` — **you** drive every deploy. ZCP records evidence, never initiates a deploy, and the auto-close gate stays open until you call `action="close"` explicitly.

Pick a close-mode per service:

```
{services-list:zerops_workflow action="close-mode" closeMode={"{hostname}":"auto"}}
```

Replace `auto` with `git-push` or `manual` to match your workflow. Switching to `git-push` returns chained guidance pointing at `action="git-push-setup"` to provision GIT_TOKEN / .netrc / remote URL. The build integration (webhook / actions) is independent — wire it via `action="build-integration"` whenever git-push capability lands.

Pick explicitly before iterating; the default keeps working but committed close-modes drive the post-first-deploy auto-close gate.
