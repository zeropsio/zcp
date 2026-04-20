---
id: develop-push-dev-deploy
priority: 2
phases: [develop-active]
deployStates: [deployed]
strategies: [push-dev]
title: "Push-dev strategy — deploy via zerops_deploy"
---

### Push-Dev Deploy Strategy

The dev container uses SSH push — `zerops_deploy` uploads the working
tree straight into the service without a git remote.

- Self-deploy (single service): `zerops_deploy targetService="{hostname}"`
- Cross-deploy (dev → stage): `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"`
