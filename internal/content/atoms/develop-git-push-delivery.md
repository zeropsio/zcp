---
id: develop-git-push-delivery
priority: 2
phases: [develop-active]
gitPushStates: [configured]
closeDeployModes: [auto, unset]
modes: [standard, simple, local-stage, local-only]
deployStates: [deployed]
multiService: aggregate
title: "Delivery = commit + git push — the repo is the source of truth"
references-fields: [ops.GitPushResult.Status, ops.GitPushResult.BuildTarget, ops.GitPushResult.BuildStatus, ops.GitPushResult.AutoRecorded]
references-atoms: [develop-git-push-broken]
---
Git push is configured for this service (`gitPush=configured`), so the repo is the source of truth and delivery happens by pushing to it. Development work ends with a push, not a redeploy — code that reached the remote persists across container replacement, which is what made redeploy-for-persistence necessary in the first place. Direct self/cross `zerops_deploy` calls on this pair answer `push-delivery-required` with the recommended push call instead of deploying.

**Push source vs build target.** For a standard pair, the push originates from the DEV half (push source) and the build lands on the STAGE half (build target). For simple / single-runtime modes, push source equals build target. <!-- axis-k-keep: signal #1 names topology roles (push source vs build target) load-bearing for the iteration cycle -->
<!-- axis-l-keep -->
<!-- axis-m-keep -->
<!-- axis-n-keep -->

## Ship the work

```
{services-list:zerops_deploy targetService="{hostname}" setup="<source-setup>" strategy="git-push"}
```

`targetService` is the PUSH SOURCE hostname (dev half of a standard pair, or the service itself for simple modes). The call refuses an empty tree or uncommitted changes — commit first (`ssh <host> "cd /var/www && git add -A && git commit -m '<msg>'"` for the runtime container, `git -C <workingDir> add -A && git commit -m '<msg>'` on a dev machine). It pushes HEAD to the configured remote and then follows the integration build on the build target until it settles:

| Response `status` | Meaning |
|---|---|
| `"DELIVERED"` | Push landed AND the integration build reached an active app version. The response carries `buildTarget` + `buildStatus`, and `autoRecorded: true` means the session already counts this as the build target's deploy evidence — no follow-up call needed. |
| `"PUSHED"` + failed `buildStatus` | The push landed but the build failed. The same response carries `failureClassification` (category + cause + suggested action) and `buildLogs` — read the classification first, fix, commit, push again. |
| `"PUSHED"` + build not observed | No integration build appeared inside the watch window. Either `buildIntegration=none` (see below) or the CI is slow — check `zerops_events serviceHostname="<build-target>"` later. |

**Setup parameter:** `setup=<name>` MUST match a `setup:` block name in the project's `zerops.yaml` (the push pre-flight validates the SOURCE setup; the remote build consumes its own setup name — for standard pairs typically `prod`). Read the project's `zerops.yaml` first and pass the matching name explicitly; an `INVALID_ZEROPS_YML` error response carries `attemptedSetup` + `availableSetups` for a one-round-trip recovery.

## What runs the build depends on `buildIntegration`

| `buildIntegration` | What happens after the push |
|---|---|
| `webhook` | Zerops pulls the repo and runs the build pipeline on the build target. |
| `actions` | Your GitHub Actions workflow runs `zcli push` from CI; the build lands on the build target. |
| `none` | The push is archived at the remote; no watched build fires. The push response offers the choice: wire an integration via `zerops_workflow action="build-integration"`, keep your independent CI, or stay archive-only. |

## After "DELIVERED"

Verify the build target: `zerops_verify serviceHostname="<build-target>"`. Deploy evidence for the session is already in place when the response said `autoRecorded: true`; `zerops_workflow action="record-deploy" targetService="<build-target>"` is only the recovery call for a build that landed OUTSIDE the watch window (confirm the app version via `zerops_events` first, then record).

If the push fails with a credential cause, the token was rotated or revoked upstream — ask the user for a fresh token and re-run `zerops_workflow action="git-push-setup" service="{hostname}" remoteUrl="..." gitToken="<fresh PAT>"`. Never invent or reuse a token the user didn't supply.
