---
id: develop-close-mode-auto
priority: 3
phases: [develop-active]
closeDeployModes: [auto]
deployStates: [deployed]
multiService: aggregate
title: "Delivery pattern = direct deploy via zerops_deploy"
---
This service is on `closeDeployMode=auto`. Your delivery pattern is direct `zerops_deploy` calls via zcli — fast, synchronous, the canonical default for tight iteration cycles. `action="close"` itself is a session-teardown call regardless of close-mode; auto-close fires when the deploys you ran during iterations satisfy the green-scope gate.

## How auto-close fires

When auto-close conditions land (every service in scope has a successful deploy + passed verify), ZCP closes the develop session automatically. The deploys that landed during develop iterations ARE the close deploys — there's no separate close-time push, and no special call from the close handler.

The env-specific mechanics (SSH push from `/var/www` for container, `zcli push` from CWD for local) live in the env-scoped deploy guidance fired alongside this atom.

## When you might switch

`auto` is great for "make a change, see it live, repeat." If the workflow grows — multiple contributors landing changes, CI pipelines that should run before deploy, release branches — switch:

- `git-push` if pushing to a git remote should trigger the build (Zerops webhook or GitHub Actions). After the close-mode flip, `action=git-push-setup` provisions the capability.
- `manual` if external orchestration owns close decisions. ZCP still records every deploy/verify; auto-close just doesn't fire.

Switch close-mode per service:

```
{services-list:zerops_workflow action="close-mode" closeMode={"{hostname}":"git-push"}}
```

(Replace `git-push` with `manual` to yield to user orchestration.) The default stays auto until you explicitly switch.
