---
id: develop-close-mode-manual
priority: 2
phases: [develop-active]
closeDeployModes: [manual]
multiService: aggregate
title: "Manual close-mode = ZCP yields, your tools own the close"
coverageExempt: "manual close-mode is the rare external-orchestration path — 30 canonical scenarios cover auto + git-push (the common cases); manual is <1% of agent sessions per Phase 4 heuristic"
---
This service is on `closeDeployMode=manual`. ZCP records deploy and verify attempts when you call its tools, but the implicit auto-close at end of work session is gated off — workflows on manual close stay open until you explicitly close them.

## Why pick manual

Manual is the extension slot. Pick it when an external loop (your own slash command, a hook, a CI step, custom orchestration) owns the deploy/verify/close decisions. ZCP becomes a recording surface: every `zerops_*` tool you call still updates state, but ZCP never decides the workflow is "done" on its own.

## What still works

- All deploy tools remain callable; `zerops_deploy` records to the session as usual.
- `zerops_verify` records to the session and surfaces results on `action=status`.
- `zerops_workflow action=status` returns the lifecycle envelope unchanged — you see exactly which services have a successful deploy and a passed verify.
- The `workSessionState` block on side-effect responses carries `status` (open / auto-closed / none) plus the per-service progress so callers observe whether auto-close gating fires; on a manual-close pair the gate stays open until you call `action="close"` explicitly.

## What stops working

The implicit auto-close on deploy/verify success. Even when every service in scope has a successful deploy + passed verify, the workflow stays open until you call:

```
zerops_workflow action="close"
```

That's the explicit close. The recovery path in `action=status` references it.

## Switching back

To rejoin the auto-close gate, swap close-mode per service:

```
{services-list:zerops_workflow action="close-mode" closeMode={"{hostname}":"auto"}}
```

(Or `"git-push"` instead of `"auto"`.) The close-mode write succeeds standalone — for git-push, follow the chained `nextSteps` pointer at `action=git-push-setup` to provision the capability.
