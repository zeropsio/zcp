---
id: develop-push-dev-deploy-container
priority: 2
phases: [develop-active]
deployStates: [deployed]
strategies: [push-dev]
environments: [container]
title: "Push-dev strategy — deploy via zerops_deploy (container)"
---

### Push-Dev Deploy Strategy — container

The dev container uses SSH push — `zerops_deploy` uploads the working
tree from `/var/www/{hostname}/` straight into the service without a
git remote. No credentials involved on your side: the tool SSHes using
ZCP's container-internal key.

- Self-deploy (single service): `zerops_deploy targetService="{hostname}"`
- Cross-deploy (dev → stage): `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"`

`deployFiles` discipline differs per class — self-deploy needs `[.]`,
cross-deploy cherry-picks build output. See `develop-deploy-modes`.
