---
id: develop-close-mode-auto
priority: 3
phases: [develop-active]
closeDeployModes: [auto]
deployStates: [deployed]
multiService: aggregate
title: "Close = direct deploy via zerops_deploy"
---
This pair is on `closeDeployMode=auto`. The develop close action runs `zerops_deploy` directly from ZCP — fast, synchronous, and the canonical default for tight iteration cycles.

## How close fires

When auto-close conditions land (every service in scope has a successful deploy + passed verify), ZCP closes the develop session automatically. The deploy that landed during develop iterations is the close deploy — there's no separate close-time push.

The mechanics underneath:

| environment | what `zerops_deploy` does |
|---|---|
| container | SSH to the dev hostname → `zcli push` from `/var/www` to the runtime. Synchronous: deploy result lands when the build pipeline completes. |
| local | `zcli push` from the local workspace to the linked Zerops stage. Same shape, different source. |

## When you might switch

`auto` is great for "make a change, see it live, repeat." If the workflow grows — multiple contributors landing changes, CI pipelines that should run before deploy, release branches — switch:

- `git-push` if pushing to a git remote should trigger the build (Zerops webhook or GitHub Actions). After the close-mode flip, `action=git-push-setup` provisions the capability.
- `manual` if external orchestration owns close decisions. ZCP still records every deploy/verify; auto-close just doesn't fire.

Switch close-mode per service:

```
{services-list:zerops_workflow action="close-mode" closeMode={"{hostname}":"git-push"}}
```

(Replace `git-push` with `manual` to yield to user orchestration.) The default stays auto until you explicitly switch.
