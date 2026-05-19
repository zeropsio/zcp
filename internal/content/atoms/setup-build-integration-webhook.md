---
id: setup-build-integration-webhook
priority: 3
phases: [strategy-setup]
gitPushStates: [configured]
buildIntegrations: [none]
title: "Alternative (GitLab / policy-constrained repos): Zerops dashboard webhook"
---
Webhook is the fallback CI shape when Actions doesn't fit — GitLab remotes, repos where workflow files / secrets can't be written from the terminal, or policies that block GitHub Actions. Zerops OAuths the repository in the dashboard and rebuilds the runtime on every push. **Requires one manual step in the Zerops dashboard** (the OAuth flow can't run from MCP). For GitHub remotes with a permissive PAT the Actions integration is the shorter happy path — see the actions atom.

ZCP doesn't track external CI/CD you may already have — `build-integration=webhook` is additive.

## 1. Confirm git-push setup landed

This atom assumes `GitPushState=configured`. If you haven't run the setup yet, `action=build-integration` returns a `needsGitPushSetup` pointer; resolve that first.

## 2. Wire the webhook in the Zerops dashboard

The confirm response carries `dashboardUrl` (deep-link to the BUILD TARGET service's Deploy panel — stage half for standard pairs, the service itself for simple modes) and `setupFieldMandatory` (true when buildSetup ≠ hostname). One-time account-level setup applies: the Zerops dashboard OAuth bonds GitHub on the user's ACCOUNT, not per-repo or per-service — once bonded, every project and service-stack on that account can wire builds without re-authorizing.

1. Open `dashboardUrl` — the build-target service's Deploy tab.
2. Click the GitHub/GitLab integration button. If this is the first time on this account, authorize Zerops (one-time per account; the bond covers every future project + service-stack).
3. Pick the repository. Choose trigger type: Push to Branch (typical) or New Tag (production-grade CD with tag regex).
4. For Push to Branch: select the branch (default `main` auto-discovered from the repo).
5. **`setup` field** — the UI labels it optional, but the dashboard maps service hostname → setup block in `zerops.yaml` with NO fallback. Type the `buildSetup` value the confirm response carried whenever it differs from the hostname (recipe pairs: `prod` on the stage half; single-runtime: matches the runtime's setup name). Leaving blank surfaces "The setup was not found" when the hostname has no matching setup block.
6. Optional: tick "Trigger once after the activation?" to run an immediate build of the current branch state right after save.
7. Click Activate pipeline trigger.

## 3. Mark the integration configured

```
zerops_workflow action="build-integration" service="{hostname}" \
  integration="webhook"
```

This persists `BuildIntegration=webhook` so `zerops_deploy strategy=git-push` stops surfacing the missing-trigger warning. From now on, every push hitting the remote triggers a Zerops build automatically — including pushes from other contributors, not just yours.
