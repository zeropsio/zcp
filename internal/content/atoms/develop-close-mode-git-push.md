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
This service is on `closeDeployMode=git-push`. Your delivery pattern is `zerops_deploy strategy="git-push"` — commits whatever is live in the workspace and pushes to the configured remote, then Zerops (or your CI) sees the push and runs the build separately. `action="close"` itself is a session-teardown call regardless of close-mode; run the push call below before invoking close.

**Push source vs build target.** For a standard pair, push originates from the DEV half (push source), and Zerops rebuilds the STAGE half (build target) from the remote. No local dev-to-stage cross-deploy is required — the remote push replaces it. For simple / single-runtime modes, push source equals build target. <!-- axis-k-keep: signal #1 names topology roles (push source vs build target) load-bearing for the iteration cycle -->
<!-- axis-l-keep -->
<!-- axis-m-keep -->
<!-- axis-n-keep -->

## Push the build commit

```
{services-list:zerops_deploy targetService="{hostname}" setup="<source-setup>" strategy="git-push"}
```

`targetService` is the PUSH SOURCE hostname (dev half of a standard pair, or the service itself for simple modes). The deploy tool reads the working tree from `/var/www` (container) or the local workspace, ensures there's a fresh commit, and pushes to the configured remote on the configured branch. If `failureClassification.category=credential` surfaces (GIT_TOKEN or local credentials missing), re-run `zerops_workflow action="git-push-setup" service="{hostname}"` to refresh the capability.

**Setup parameter:** `setup=<name>` MUST match a `setup:` block name in the project's `zerops.yaml`. For the local push pre-flight use the SOURCE setup (recipe-derived: `dev`; single-runtime: `<hostname>`). The remote build (webhook / actions) consumes a separate setup name — for standard pairs that's `prod`, and the build-integration confirm response carries `buildSetup` already populated. The deploy tool defaults the source setup to the target hostname when omitted — that works only when the setup name happens to equal the hostname. Read the project's `zerops.yaml` first and pass the matching name explicitly. An `INVALID_ZEROPS_YML` error response carries `attemptedSetup` + `availableSetups` so a missed first call is recoverable in one round-trip.

**Self-deploy / cross-deploy under git-push:** the develop iteration cycle uses `zerops_deploy strategy="git-push"` only — `zerops_deploy targetService="{hostname}" setup=dev` (self-deploy into dev) and `zerops_deploy sourceService=<dev> targetService=<stage> setup=prod` (local cross-deploy) are NOT part of the git-push delivery pattern. Persistence is git; the build is async on the build target.

## What runs the build depends on `BuildIntegration`

| `buildIntegration` | What happens after the push |
|---|---|
| `webhook` | Zerops dashboard pulls the repo and runs the build pipeline. Watch via `zerops_events serviceHostname="<hostname>"`. |
| `actions` | Your GitHub Actions workflow runs `zcli push` from CI; the build lands on the runtime. Same observation path. |
| `none` | The push is archived at the remote. No ZCP-managed build fires; if you have independent CI/CD, that may pick it up — ZCP doesn't track external CI. |

`build-integration=none` is a valid steady state if your team has independent CI/CD. The `Warnings` array surfaces a soft note when the deploy tool detects this combination — informational, not a blocker.

## When the build lands, ack it

For webhook + actions integrations, the build is async — `zerops_deploy strategy=git-push` returns as soon as the push transmits. After `zerops_events` confirms the build's appVersion went `Status: ACTIVE` on the BUILD TARGET (stage half for standard pairs), run record-deploy on that build target:

```
zerops_workflow action="record-deploy" targetService="<build-target>"
```

This records the deploy so the develop session sees the build target as deployed and auto-close becomes eligible (the close-mode gate stays open under git-push). If the push source is passed by mistake (dev half), the call routes to the build target and the response carries a warning — pass the build target directly next time. Verify runs against the build target the same way: `zerops_verify serviceHostname="<build-target>"`.
