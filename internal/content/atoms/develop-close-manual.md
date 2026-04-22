---
id: develop-close-manual
priority: 7
phases: [develop-active]
deployStates: [deployed]
strategies: [manual]
title: "Close task — manual strategy"
---

### Closing the task — manual strategy

**ZCP stays out of the deploy loop on manual strategy.** The user
declared they orchestrate deploys themselves (with their own CI, their
own scripts, their own discretion) — that's the entire point of
selecting manual. Don't suggest `zerops_deploy` or any other tool-
driven deploy from the close step.

Tell the user that code changes are complete and summarize what was
done. They'll handle rollout on their own schedule.

If the user later decides they want ZCP automation, they can switch:

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}
# or
zerops_workflow action="strategy" strategies={"{hostname}":"push-git"}
```

Until then, treat the task as closed the moment the code is committed
and report status to the user.
