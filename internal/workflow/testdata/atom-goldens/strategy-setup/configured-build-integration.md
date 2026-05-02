---
id: strategy-setup/configured-build-integration
atomIds: [setup-build-integration-actions, setup-build-integration-webhook]
description: "strategy-setup phase, GitPushState configured, BuildIntegration none — agent picks webhook vs actions."
---
<!-- UNREVIEWED -->

The Actions integration is one specific ZCP-managed CI shape: a GitHub Actions workflow runs `zcli push` from CI on every push that matches the workflow trigger. ZCP doesn't track or manage external workflows you may already have, so `build-integration=actions` is additive — independent CI/CD keeps running unchanged.

After you call `zerops_workflow action="build-integration" service="appdev" integration="actions"`, the response carries the workflow YAML body + prefilled `gh secret set` commands ready to paste. This atom is the human-readable companion that explains what each piece does and the recommended GitHub PAT shape.

## 1. Confirm git-push setup landed

This atom assumes `GitPushState=configured`. If you haven't run the setup yet, `action=build-integration` returns a `needsGitPushSetup` pointer; resolve that first.

## 2. Add the workflow file

Create `.github/workflows/zerops.yml` in the repo. The default workflow uses
`zcli` directly because it can pass `--setup`; this is required when
`zerops.yaml` has multiple setup blocks or when the setup name must be selected
explicitly.

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
      - name: Install zcli
        run: |
          curl -sSL https://zerops.io/zcli/install.sh | sh
          echo "$HOME/.local/bin" >> "$GITHUB_PATH"
      - name: Deploy to Zerops
        run: |
          zcli login "$ZEROPS_TOKEN"
          zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID }}" --setup appdev
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
```

Replace `appdev` with the setup name in your `zerops.yaml` if it differs from the runtime hostname.

If the repository has exactly one setup and you do not need explicit setup
selection, the compact wrapper action also works:

```yaml
      - uses: zeropsio/actions@v1.0.2
        with:
          access-token: ${{ secrets.ZEROPS_TOKEN }}
          service-id: ${{ secrets.ZEROPS_SERVICE_ID }}
```

## 3. Add the GitHub Actions secrets

**`ZEROPS_TOKEN` is the same Zerops PAT as `ZCP_API_KEY` — DON'T generate a new one.** ZCP already holds the value; reusing it as the GitHub secret keeps one credential, one rotation surface, one revocation path. Generating a separate PAT just for Actions doubles the long-lived credential count without any security gain.

**Recommended GitHub PAT shape**: a fine-grained PAT scoped ONLY to `{owner}/{repo}` with `Secrets: Read and write`. Single-repo blast radius — the agent can only manipulate this one repository. GitHub fine-grained PATs require an expiration; pick the longest you're comfortable with (max 1 year) and set a calendar reminder to regenerate + re-run `gh secret set` before it lapses.

Two `gh secret set` invocations wire both secrets in one shot. The exact form depends on where ZCP runs:

**Container** (ZCP runs inside a Zerops container; `ZCP_API_KEY` is in the container env):

```
gh secret set ZEROPS_TOKEN -b "$ZCP_API_KEY" -R {owner}/{repo}
gh secret set ZEROPS_SERVICE_ID -b "{serviceId}" -R {owner}/{repo}
```

**Local** (ZCP runs from the dev workstation; `ZCP_API_KEY` lives in `.mcp.json` alongside the MCP server config):

```
gh secret set ZEROPS_TOKEN -b "$(jq -r '.mcpServers.zcp.env.ZCP_API_KEY' .mcp.json)" -R {owner}/{repo}
gh secret set ZEROPS_SERVICE_ID -b "{serviceId}" -R {owner}/{repo}
```

In both cases the `ZCP_API_KEY` value substitutes in via the shell at expansion time — the literal token never crosses the MCP wire (so it never lands in chat logs / transcripts). The GitHub PAT used by `gh` itself only needs `Secrets: Read and write` on the one repo; it's the credential `gh` reads from its own keychain, not anything ZCP holds.

## 4. Mark the integration configured

```
zerops_workflow action="build-integration" service="appdev" \
  integration="actions"
```

This persists `BuildIntegration=actions` and returns the workflow YAML + secret commands prefilled with `serviceId` and `owner/repo` (parsed from the stamped `meta.RemoteURL`). The response is ready to paste — no extra lookups needed.

Note the orthogonality: Actions runs `zcli push` from CI, which mechanically pushes the build to the runtime via the same path `zerops_deploy` uses. The difference vs `webhook` is who owns the pull (GitHub Actions vs Zerops).

---

The webhook integration is one specific ZCP-managed CI shape: when a push lands on the remote, Zerops pulls the repo and runs the build pipeline. Independent CI/CD you may already have keeps working — ZCP doesn't track or manage external integrations, so `build-integration=webhook` is additive, not exclusive.

## 1. Confirm git-push setup landed

This atom assumes `GitPushState=configured`. If you haven't run the setup yet, `action=build-integration` returns a `needsGitPushSetup` pointer; resolve that first.

## 2. Wire the webhook in the Zerops dashboard

Open your Zerops project in the dashboard and navigate to the runtime service for `appdev`. Use the OAuth flow there to:

1. Connect to GitHub or GitLab (whichever hosts the repo).
2. Pick the repository the service should pull from.
3. Pick the branch (typically `main`).
4. Save.

The dashboard installs the webhook on the remote side with the right permissions. On GitHub fine-grained tokens you need at minimum `Contents: Read` on the repo and `Webhooks: Read and write` on the org/account; the OAuth flow surfaces the specific scope prompt.

## 3. Mark the integration configured

```
zerops_workflow action="build-integration" service="appdev" \
  integration="webhook"
```

This persists `BuildIntegration=webhook` so `zerops_deploy strategy=git-push` stops surfacing the missing-trigger warning. From now on, every push hitting the remote triggers a Zerops build automatically — including pushes from other contributors, not just yours.
