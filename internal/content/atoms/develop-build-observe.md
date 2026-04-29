---
id: develop-build-observe
priority: 3
phases: [develop-active]
closeDeployModes: [git-push]
buildIntegrations: [webhook, actions]
deployStates: [deployed]
multiService: aggregate
title: "Observe an async build after a git push"
---
With `build-integration=webhook` or `build-integration=actions`, `zerops_deploy strategy=git-push` returns as soon as the push transmits — the build runs separately and asynchronously. You observe it via `zerops_events`, then bridge the result back into the local develop session with `record-deploy`.

## Watch the appVersion

```
{services-list:zerops_events serviceHostname="{hostname}"}
```

A new push triggers a build appVersion per service. Look for:

| `Status` | Meaning |
|---|---|
| `BUILDING` | The build pipeline is running. Re-call `zerops_events` to advance. |
| `ACTIVE` | Build completed; the runtime now serves the new code. Proceed to record-deploy + verify. |
| `FAILED` | Build failed. Read the latest event's `failureClass` + `description` for the cause; the recovery is whatever fixed the build (yaml, missing env var, code issue) plus a fresh push. |

The events tool is an envelope-aware lookup — pass `since=<duration>` to limit the window if the service has long history.

## Bridge the deploy back

Once `Status=ACTIVE`, run record-deploy per service:

```
{services-list:zerops_workflow action="record-deploy" targetService="{hostname}"}
```

This records each deploy so the next envelope render sees the service as deployed. From here the develop atoms gated on `deployStates: [deployed]` start firing, and a `zerops_verify` confirms the new code is healthy.

## When the build doesn't fire

`Warnings` on the push response calls out the case where `build-integration=none` left the push archived at the remote. If you intended to wire a build trigger, run `action=build-integration` now. If your team owns the CI side independently, ignore the warning — ZCP doesn't track external CI/CD by design.
