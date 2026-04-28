---
id: setup-build-integration-webhook
priority: 2
phases: [strategy-setup]
gitPushStates: [configured]
buildIntegrations: [none]
title: "Wire the Zerops dashboard webhook integration"
---
The webhook integration is one specific ZCP-managed CI shape: when a push lands on the remote, Zerops pulls the repo and runs the build pipeline. Independent CI/CD you may already have keeps working — ZCP doesn't track or manage external integrations, so `build-integration=webhook` is additive, not exclusive.

## 1. Confirm git-push setup landed

This atom assumes `GitPushState=configured`. If you haven't run the setup yet, `action=build-integration` returns a `needsGitPushSetup` pointer; resolve that first.

## 2. Wire the webhook in the Zerops dashboard

Open your Zerops project in the dashboard and navigate to the runtime service for `{hostname}`. Use the OAuth flow there to:

1. Connect to GitHub or GitLab (whichever hosts the repo).
2. Pick the repository the service should pull from.
3. Pick the branch (typically `main`).
4. Save.

The dashboard installs the webhook on the remote side with the right permissions. On GitHub fine-grained tokens you need at minimum `Contents: Read` on the repo and `Webhooks: Read and write` on the org/account; the OAuth flow surfaces the specific scope prompt.

## 3. Mark the integration configured

```
zerops_workflow action="build-integration" service="{hostname}" \
  integration="webhook"
```

This persists `BuildIntegration=webhook` so `zerops_deploy strategy=git-push` stops surfacing the missing-trigger warning. From now on, every push hitting the remote triggers a Zerops build automatically — including pushes from other contributors, not just yours.
