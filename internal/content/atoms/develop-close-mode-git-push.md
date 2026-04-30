---
id: develop-close-mode-git-push
priority: 2
phases: [develop-active]
closeDeployModes: [git-push]
gitPushStates: [configured]
modes: [standard, simple, local-stage, local-only]
deployStates: [deployed]
multiService: aggregate
title: "Delivery pattern = commit + git push to the remote"
references-fields: [ops.GitPushResult.Status, ops.GitPushResult.RemoteURL, ops.GitPushResult.Branch]
references-atoms: [develop-close-mode-git-push-needs-setup]
---
This pair is on `closeDeployMode=git-push`. Your delivery pattern is `zerops_deploy strategy="git-push"` — commits whatever is live in the workspace and pushes to the configured remote, then Zerops (or your CI) sees the push and runs the build separately. `action="close"` itself is a session-teardown call regardless of close-mode; run the push call below before invoking close.

## Push the build commit

```
{services-list:zerops_deploy targetService="{hostname}" strategy="git-push"}
```

The deploy tool fetches the working tree from `/var/www` (container) or the local workspace, ensures there's a fresh commit, and pushes to the configured remote on the configured branch. Use `setup-git-push-container` (or `setup-git-push-local`) if `failureClassification.category=credential` surfaces — that means GIT_TOKEN or local credentials are missing.

## What runs the build depends on `BuildIntegration`

| `buildIntegration` | What happens after the push |
|---|---|
| `webhook` | Zerops dashboard pulls the repo and runs the build pipeline. Watch via `zerops_events serviceHostname="<hostname>"`. |
| `actions` | Your GitHub Actions workflow runs `zcli push` from CI; the build lands on the runtime. Same observation path. |
| `none` | The push is archived at the remote. No ZCP-managed build fires; if you have independent CI/CD, that may pick it up — ZCP doesn't track external CI. |

`build-integration=none` is a valid steady state if your team has independent CI/CD. The `Warnings` array surfaces a soft note when the deploy tool detects this combination — informational, not a blocker.

## When the build lands, ack it

For webhook + actions integrations, the build is async — `zerops_deploy strategy=git-push` returns as soon as the push transmits. After `zerops_events` confirms the build's appVersion went `Status: ACTIVE`, run record-deploy per service:

```
{services-list:zerops_workflow action="record-deploy" targetService="{hostname}"}
```

This records the deploy so the develop session sees the service as deployed and auto-close becomes eligible (the close-mode gate stays open under git-push).
