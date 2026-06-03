---
id: develop-strategy-review
priority: 1
phases: [develop-active]
deployStates: [deployed]
closeDeployModes: [unset]
multiService: aggregate
title: "Pick an ongoing close-mode"
---

### DECISION — pick a close-mode now (auto-close stays BLOCKED until set)

First deploy is on record (`deployed: true`) but close-mode is `unset`. Set it per in-scope service before iterating — this is the one call that unblocks auto-close:

```
{services-list:zerops_workflow action="close-mode" closeMode={"{hostname}":"auto"}}
```

Swap `auto` for the delivery pattern you want:

- `auto` — agent runs `zerops_deploy` directly via zcli; auto-close fires once scope-services are green. Fast for tight iteration.
- `git-push` — `zerops_deploy strategy="git-push"` commits + pushes to a configured remote; Zerops/CI builds. Returns chained guidance to `action="git-push-setup"` first. Build integration (webhook/actions) is independent — `action="build-integration"`.
- `manual` — **you** drive every deploy; ZCP records evidence, never deploys, auto-close stays open until you call `action="close"`.

close-mode does NOT change what `action="close"` does (always session-teardown) — it selects the per-mode iteration guidance and drives the auto-close gate.
