---
id: strategy-setup/configured-build-integration
atomIds: [setup-build-integration-actions, setup-build-integration-webhook]
description: "strategy-setup phase, GitPushState configured, BuildIntegration none — agent picks webhook vs actions."
---
The Actions integration is the recommended ZCP-managed CI shape for GitHub remotes — a GitHub Actions workflow runs `zcli push` from CI on every push to the configured branch. With a permissive PAT and `gh` CLI, both the workflow file and the two repository secrets are written from the terminal, so the setup completes without any manual step in the Zerops dashboard. (Webhook is the fallback for GitLab / policy-constrained repos — see the webhook atom.)

ZCP doesn't track or manage external workflows you may already have, so `build-integration=actions` is additive — independent CI/CD keeps running unchanged.

After you call `zerops_workflow action="build-integration" service="appdev" integration="actions"`, the response carries the workflow YAML body + prefilled `gh secret set` commands ready to paste, plus `buildTarget` and `buildSetup` so the workflow's `zcli push --service-id ... --setup ...` targets the right runtime (stage half for standard pairs, the service itself for simple modes). This atom is the human-readable companion.

## 1. Confirm git-push setup landed AND `gh` is authenticated

Two preconditions live outside this integration — surface them before calling:

- **`GitPushState=configured`** — if setup hasn't run yet, `action=build-integration` returns a `needsGitPushSetup` pointer.
- **No `gh auth login` step** — the `gh secret set` commands convey the token per invocation; the confirm response's `ghTokenConveyance` block carries the exact mechanism resolved for the current environment. There is no stored gh credential and no login precondition — the command always acts with the current git-push-setup PAT. That PAT needs `Secrets: Read and write` on the repo (the recommended actions-track scope already covers it).

`BuildIntegration=actions` records the choice and the handoff shape; the workflow YAML commit, the secret-set, and the `gh` auth steps happen outside this integration's reach.

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
          zcli push --service-id "${{ secrets.ZEROPS_SERVICE_ID }}" --setup <build-setup>
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
```

The `build-integration=actions` confirm response carries `buildSetup` already resolved for the target topology (recipe-style standard pair: `prod` on stage; single-runtime adoption: the runtime's own setup name). Paste that value in place of `<build-setup>` before committing the workflow file. The `workflowFile.content` field of the confirm response has it already substituted — copy that body verbatim.

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

**Recommended GitHub PAT shape**: a fine-grained PAT scoped ONLY to `{owner}/{repo}` with `Contents: Read and write` (commit/push the workflow file in step 2) + `Workflows: Read and write` (REQUIRED — `.github/workflows/` pushes are rejected without it) + `Secrets: Read and write` (the `gh secret set` in step 3) + `Actions: Read` and `Checks: Read` (watch the run with `gh run list` / `gh run watch`). Create or edit it at <https://github.com/settings/personal-access-tokens>. Single-repo blast radius — the agent can only manipulate this one repository. GitHub fine-grained PATs require an expiration; pick the longest you're comfortable with (max 1 year) and set a calendar reminder to regenerate before it lapses.

Two `gh secret set` invocations wire both secrets in one shot. The exact form depends on where ZCP runs:

**Container** (ZCP runs inside a Zerops container; `ZCP_API_KEY` is in the container env):

```
gh secret set ZEROPS_TOKEN -b "$ZCP_API_KEY" -R {owner}/{repo}
gh secret set ZEROPS_SERVICE_ID -b "{serviceId}" -R {owner}/{repo}
```

**Local** (ZCP runs from the dev workstation; `ZCP_API_KEY` lives in `.mcp.json` alongside the MCP server config):

```
gh secret set ZEROPS_TOKEN -b "$(jq -r '.mcpServers.zerops.env.ZCP_API_KEY' .mcp.json)" -R {owner}/{repo}
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
zerops_workflow action="build-integration" service="appdev" \
  integration="webhook"
```

This persists `BuildIntegration=webhook` so `zerops_deploy strategy=git-push` stops surfacing the missing-trigger warning. From now on, every push hitting the remote triggers a Zerops build automatically — including pushes from other contributors, not just yours.
