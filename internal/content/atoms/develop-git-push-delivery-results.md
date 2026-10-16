---
id: develop-git-push-delivery-results
priority: 2
phases: [develop-active]
gitPushStates: [configured]
closeDeployModes: [auto, unset]
modes: [standard, simple, local-stage, local-only]
stageScope: [in-scope, none]
deployStates: [deployed]
multiService: aggregate
reference: true
title: "Git-push results — response status, what runs the build, after DELIVERED"
references-fields: [ops.GitPushResult.Status, ops.GitPushResult.BuildTarget, ops.GitPushResult.BuildStatus, ops.GitPushResult.AutoRecorded]
---

A `zerops_deploy strategy="git-push"` call pushes HEAD to the configured remote and then follows the integration build on the build target until it settles:

| Response `status` | Meaning |
|---|---|
| `"DELIVERED"` | Push landed AND the integration build reached an active app version. The response carries `buildTarget` + `buildStatus`, and `autoRecorded: true` means the session already counts this as the build target's deploy evidence — no follow-up call needed. |
| `"PUSHED"` + failed `buildStatus` | The push landed but the build failed. The same response carries `failureClassification` (category + cause + suggested action) and `buildLogs` — read the classification first, fix, commit, push again. |
| `"PUSHED"` + build not observed | No integration build appeared inside the watch window. Either `buildIntegration=none` (see below) or the CI is slow — check `zerops_events serviceHostname="<build-target>"` later. |

## What runs the build depends on `buildIntegration`

| `buildIntegration` | What happens after the push |
|---|---|
| `webhook` | Zerops pulls the repo and runs the build pipeline on the build target. |
| `actions` | A workflow in the repository runs the build from CI. On GitHub that is `.github/workflows/`, running `zcli push` with a CI token; the build lands on the build target. |
| `none` | The push is archived at the remote; no watched build fires. The push response offers the choice: wire an integration via `zerops_workflow action="build-integration"`, keep your independent CI, or stay archive-only. Until one is wired, a direct `zerops_deploy` is what puts code on the service — including the dev→stage promotion of a standard pair — and the push still matters as the durable copy. |

## After "DELIVERED"

Verify the build target: `zerops_verify serviceHostname="<build-target>"`. Deploy evidence for the session is already in place when the response said `autoRecorded: true`; `zerops_workflow action="record-deploy" targetService="<build-target>"` is only the recovery call for a build that landed OUTSIDE the watch window (confirm the app version via `zerops_events` first, then record).
