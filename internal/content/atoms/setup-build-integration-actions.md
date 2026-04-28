---
id: setup-build-integration-actions
priority: 2
phases: [strategy-setup]
gitPushStates: [configured]
buildIntegrations: [none]
title: "Wire the GitHub Actions integration"
---
The Actions integration is one specific ZCP-managed CI shape: a GitHub Actions workflow runs `zcli push` from CI on every push that matches the workflow trigger. ZCP doesn't track or manage external workflows you may already have, so `build-integration=actions` is additive — independent CI/CD keeps running unchanged.

## 1. Confirm git-push setup landed

This atom assumes `GitPushState=configured`. If you haven't run the setup yet, `action=build-integration` returns a `needsGitPushSetup` pointer; resolve that first.

## 2. Add the workflow file

Create `.github/workflows/zerops.yml` in the repo:

```yaml
name: Zerops deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: zeropsio/actions-setup-zcli@v1
      - run: zcli push --serviceId ${{ secrets.ZEROPS_SERVICE_ID }} --setup {hostname}
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
```

Replace `{hostname}` with the setup name in your `zerops.yaml` if it differs from the runtime hostname.

## 3. Add the GitHub Actions secrets

In the repo's GitHub settings → Secrets and variables → Actions, add:

| Secret | Source |
|---|---|
| `ZEROPS_TOKEN` | A Zerops personal access token with deploy scope. Generate one at the Zerops dashboard (Settings → Access tokens). |
| `ZEROPS_SERVICE_ID` | The numeric serviceId of the runtime — find it via `zerops_discover service="{hostname}"`. |

GitHub fine-grained PATs need `Secrets: Read and write` if you wire ZEROPS_TOKEN at the repo level. Org-level secrets work too — same scope on the org.

## 4. Mark the integration configured

```
zerops_workflow action="build-integration" service="{hostname}" \
  integration="actions"
```

This persists `BuildIntegration=actions`. Note the orthogonality: Actions runs `zcli push` from CI, which mechanically pushes the build to the runtime via the same path zerops_deploy uses. The difference vs `webhook` is who owns the pull (GitHub Actions vs Zerops).
