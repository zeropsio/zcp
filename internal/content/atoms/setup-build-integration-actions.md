---
id: setup-build-integration-actions
priority: 1
phases: [strategy-setup]
gitPushStates: [configured]
buildIntegrations: [none]
title: "Recommended for GitHub: GitHub Actions integration (zero manual dashboard step)"
---
The Actions integration is the recommended ZCP-managed CI shape for GitHub remotes — a GitHub Actions workflow runs `zcli push` from CI on every push to the configured branch. With a permissive PAT and `gh` CLI, both the workflow file and the two repository secrets are written from the terminal, so the setup completes without any manual step in the Zerops dashboard. (Webhook is the fallback for GitLab / policy-constrained repos — see the webhook atom.)

ZCP doesn't track or manage external workflows you may already have, so `build-integration=actions` is additive — independent CI/CD keeps running unchanged.

After you call `zerops_workflow action="build-integration" service="{hostname}" integration="actions"`, the response carries the workflow YAML body + prefilled `gh secret set` commands ready to paste, plus `buildTarget` and `buildSetup` so the workflow's `zcli push --service-id ... --setup ...` targets the right runtime (stage half for standard pairs, the service itself for simple modes). This atom is the human-readable companion.

## 1. Confirm git-push setup landed AND `gh` is authenticated

Two preconditions live outside this integration — surface them before calling:

- **`GitPushState=configured`** — if setup hasn't run yet, `action=build-integration` returns a `needsGitPushSetup` pointer.
- **`gh auth status`** — the `gh secret set` commands below assume an authenticated GitHub CLI session. The confirm response carries the `ghAuthPrecondition` block with the exact authentication command to run FIRST, resolved for the current environment; do it or the first `gh secret set` invocation fails. A PAT with `Secrets: Read and write` on the repo works (the git-push-setup PAT already covers this when scoped Secrets+Workflows).

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

**Recommended GitHub PAT shape**: a fine-grained PAT scoped ONLY to `{owner}/{repo}` with `Secrets: Read and write`. Single-repo blast radius — the agent can only manipulate this one repository. GitHub fine-grained PATs require an expiration; pick the longest you're comfortable with (max 1 year) and set a calendar reminder to regenerate + re-run `gh secret set` before it lapses. <!-- axis-m-keep -->

Two `gh secret set` invocations wire both secrets in one shot. The exact form depends on where ZCP runs:

<!-- axis-n-keep -->
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
zerops_workflow action="build-integration" service="{hostname}" \
  integration="actions"
```

This persists `BuildIntegration=actions` and returns the workflow YAML + secret commands prefilled with `serviceId` and `owner/repo` (parsed from the stamped `meta.RemoteURL`). The response is ready to paste — no extra lookups needed.

Note the orthogonality: Actions runs `zcli push` from CI, which mechanically pushes the build to the runtime via the same path `zerops_deploy` uses. The difference vs `webhook` is who owns the pull (GitHub Actions vs Zerops).
