---
id: develop-close-mode-auto-deploy-container
priority: 2
phases: [develop-active]
deployStates: [deployed]
closeDeployModes: [auto]
environments: [container]
title: "close-mode=auto — deploy via zerops_deploy"
references-fields: [ops.DeployResult.Mode, ops.DeployResult.SourceService, ops.DeployResult.TargetService]
references-atoms: [develop-deploy-modes, develop-deploy-files-self-deploy, develop-platform-rules-container]
---

### close-mode=auto Deploy

The dev container uses SSH push — `zerops_deploy` uploads the
working tree from `/var/www/{hostname}/` straight into the service
without a git remote. No credentials on your side: `zerops_deploy` SSHes
using ZCP's runtime container internal key. The response's `mode` is `ssh`;
`sourceService` and `targetService` identify the deploy class.

- Self-deploy (single service): `zerops_deploy targetService="{hostname}"` — `sourceService == targetService`, class is self.
- Cross-deploy (dev → stage): `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"` — class is cross.

`deployFiles` discipline differs per class: self-deploy needs `[.]`
(narrower patterns destroy the target's source); cross-deploy
cherry-picks build output. See `develop-deploy-modes` for the full
rule and `develop-deploy-files-self-deploy` for the self-deploy
invariant.
