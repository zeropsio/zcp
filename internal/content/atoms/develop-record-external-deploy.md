---
id: develop-record-external-deploy
title: Record an external deploy back into ZCP state
priority: 4
phases: [develop-active]
deployStates: [deployed]
buildIntegrations: [webhook, actions]
---
A deploy that happened outside ZCP — webhook fired on a remote push, GitHub Actions ran zcli push, or a teammate ran zcli push directly — does not record the deploy in local state. Without that record the service stays at `deployState=never-deployed` here and develop atoms gated on `deployStates: [deployed]` don't fire even after Zerops has run the new build.

`zerops_workflow action="record-deploy" targetService="{hostname}"` is the canonical bridge: it records the deploy so the next envelope render sees the service as deployed. The call is idempotent — re-running on an already-recorded service is a no-op. When the service's mode is eligible (dev/stage/simple/standard/local-stage), the call also auto-enables the L7 subdomain route, mirroring how zerops_deploy handles first deploy.

Run it after `zerops_events serviceHostname="{hostname}"` confirms the appVersion landed (`Status: ACTIVE`). Skip it when you triggered the deploy through `zerops_deploy` — that path stamps automatically.

```
zerops_workflow action="record-deploy" targetService="{hostname}"
```

The response carries `subdomainAccessEnabled` + `subdomainUrl` when the auto-enable side effect lands, plus `firstDeployedAt` so you can confirm the stamp.
