---
id: strategy-push-git-trigger-actions
priority: 3
phases: [strategy-setup]
strategies: [push-git]
triggers: [actions]
title: "push-git trigger — GitHub Actions workflow"
---

# Trigger — GitHub Actions

Action on push: the repo's own CI runs `zcli push` back to Zerops. More
flexible than webhook (monorepos, branch routing, matrix builds) but
requires a secret + workflow file in the repo.

## 1. Get a Zerops deploy token

Ask the user:

> "Use the existing ZCP API key as the deploy token, or create a scoped
> one at https://app.zerops.io/settings/token-management?"

A scoped per-project token is the safer default.

## 2. Store the token as `ZEROPS_TOKEN` secret

Preferred (`gh` CLI on the dev machine):

```
gh secret set ZEROPS_TOKEN --repo {owner}/{repoName} --body "{zeropsToken}"
```

If `gh` isn't available, UI fallback:

> GitHub repo → **Settings** → **Secrets and variables** → **Actions** →
> **New repository secret** → Name: `ZEROPS_TOKEN`, Value: the token.

## 3. Confirm Actions workflow permissions

> Repo → **Settings** → **Actions** → **General** → **Workflow
> permissions** → "Read and write permissions"

## 4. Resolve `serviceId` + `setup` name

```
zerops_discover service="{targetHostname}"
```

Setup names come from `zerops.yaml`:
- Container env: `ssh {targetHostname} "grep -E '^\s*- setup:' /var/www/zerops.yaml"`
- Local env: `grep -E '^\s*- setup:' <your-project-dir>/zerops.yaml`

Conventional mapping: dev/iteration target → `dev`, stage/prod target → `prod`.

## 5. Write `.github/workflows/deploy.yml`

In local env, create this file directly in your repo; in container env
use SSH with `cat > …`.

```yaml
name: Deploy to Zerops
on:
  push:
    branches: [{branchName}]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install zcli
        run: |
          curl -sSL https://zerops.io/zcli/install.sh | sh
          echo "$HOME/.local/bin" >> $GITHUB_PATH
      - name: Deploy to {targetHostname}
        run: zcli push --serviceId {serviceId} --setup {setup}
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
```

**Monorepo with multiple targets**: duplicate the final step for each
service, each with its own `--serviceId` and `--setup`.

## 6. Commit + first push

- Container env: follow `strategy-push-git-push-container` to commit the
  workflow file via SSH and `zerops_deploy strategy=git-push` it.
- Local env: commit locally and `zerops_deploy strategy=git-push` (or
  just `git push origin <branch>` if the user prefers their own git).

The first push also fires the Actions workflow. Two builds happen on
this push — Zerops's own (via `git-push`) and Actions's round-trip via
`zcli push`. Redundant the first time; verifies the CI path works.
Subsequent pushes only fire the Actions path.

## 7. Verify

```
zerops_events serviceHostname="{targetHostname}" limit=5
```

Look for `stack.build` in RUNNING / FINISHED. If missing:

| Problem | Fix |
|---|---|
| `ZEROPS_TOKEN` secret not found in Actions logs | Secret name must match exactly, Workflow permissions = Read + write |
| `zcli: command not found` | Install step missing `echo "$HOME/.local/bin" >> $GITHUB_PATH` |
| `Cannot find corresponding setup` | Fix `--setup` flag to match a `setup:` key in `zerops.yaml` |
| `[ci skip]` / `[skip ci]` in commit | Remove marker, push again |
| Actions not triggering | `.github/workflows/` must be on `{branchName}` in the pushed commit |
| 60-minute build timeout | Slow step — inspect `zerops_logs` |

## 8. Report

```
push-git + Actions configured for {targetHostname} → {repoUrl}.
Every push to {branchName} runs .github/workflows/deploy.yml which calls
zcli push for service {serviceId} (setup {setup}).

Iterate: push commits normally — Actions handles the deploy.
```
