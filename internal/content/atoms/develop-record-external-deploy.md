---
id: develop-record-external-deploy
title: Record an external deploy back into ZCP state
priority: 4
phases: [develop-active]
deployStates: [never-deployed]
buildIntegrations: [webhook, actions]
coverageExempt: "fires only on never-deployed runtimes with buildIntegrations:[webhook, actions] — narrow intersection; the develop/git-push-configured-webhook scenario uses deployed=true so this atom doesn't fire there. The intersection (never-deployed + webhook/actions) is rare in practice (<1% session frequency)"
---
A deploy that happens outside the synchronous ZCP push path — `zerops_deploy strategy="git-push"` (Zerops builds async after the push lands), a webhook firing on a remote push, GitHub Actions running zcli push, or a teammate running zcli push directly — does not record the deploy in local state on its own. Without that record the service stays at `deployState=never-deployed` here. ZCP-managed BuildIntegration `webhook` / `actions` services rely on this bridge.

`zerops_workflow action="record-deploy" targetService="{hostname}"` is the canonical bridge: it flips `deployState` from `never-deployed` to `deployed` so the next envelope render sees the service as deployed. The call is idempotent — re-running on an already-recorded service is a no-op. When the service's mode is in the auto-enable allow-list (dev/stage/simple/standard/local-stage), the call also attempts to auto-enable the L7 subdomain route; non-HTTP-shaped stacks (workers, deferred dev-server starts) are detected by the `serviceStackIsNotHttp` response and skipped silently.

Run it after `zerops_events serviceHostname="{hostname}"` confirms the appVersion landed (`Status: ACTIVE`). Skip it when you triggered the deploy through the synchronous `zerops_deploy` path (default zcli push) — that path records the deploy automatically because the operation completes synchronously.

```
zerops_workflow action="record-deploy" targetService="{hostname}"
```

The response carries `subdomainAccessEnabled` + `subdomainUrl` when the auto-enable side effect lands, plus `firstDeployedAt` so you can confirm the stamp.
