---
id: develop-strategy-review
priority: 1
phases: [develop-active]
closeDeployModes: [unset]
multiService: aggregate
title: "Pick an ongoing close-mode"
---

### DECISION — pick a close-mode now (auto-close stays BLOCKED until set)

Close-mode is `unset` on the listed services — auto-close stays blocked no matter how much you deploy + verify. Set it per in-scope service; it can precede the first deploy. This is the one call that unblocks auto-close:

```
{services-list:zerops_workflow action="close-mode" closeMode={"{hostname}":"auto"}}
```

Swap `auto` for the close-mode you want:

- `auto` — agent runs `zerops_deploy` directly via zcli; auto-close fires once scope-services are green. Fast for tight iteration.
- `manual` — **you** drive every deploy; ZCP records evidence, never deploys, auto-close stays open until you call `action="close"`.

Delivery is a SEPARATE dimension from close-mode: to deliver via git push, run `action="git-push-setup"` (then `zerops_deploy strategy="git-push"`); CI wiring is `action="build-integration"`. Both work under either close-mode — close-mode only owns the auto-close gate + iteration cadence, not how you deliver.

close-mode does NOT change what `action="close"` does (always session-teardown) — it selects the per-mode iteration guidance and drives the auto-close gate.
