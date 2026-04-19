---
id: develop-push-dev-deploy
priority: 2
phases: [develop-active]
strategies: [push-dev]
title: "Push-dev strategy — deploy via zerops_deploy"
---

### Push-Dev Deploy Strategy

For services bootstrapped with dev+stage pattern using SSH push deployment.
Follow the dev+stage pattern above.
Key commands: zerops_deploy targetService="{hostname}" (self-deploy),
zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}" (cross-deploy).
