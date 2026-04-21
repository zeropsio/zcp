---
id: strategy-push-git
priority: 2
phases: [strategy-setup]
title: "Configure push-git strategy — tokens, optional CI/CD, first push"
---

# Configure push-git strategy

You are setting the `{targetHostname}` service to deploy via git push to an
external repo (GitHub/GitLab). Once configured, every `zerops_deploy
strategy="git-push"` pushes the current `/var/www` contents to the remote.

**Convert the task list below into your task tracker. Execute in order.**

## Tasks

1. Confirm repo URL
2. Choose sub-mode (push-only or full CI/CD)
3. Get + set GIT_TOKEN
4. [push-only] Commit + first push. Done.
5. [CI/CD Actions] Get Zerops deploy token
6. [CI/CD Actions] Set ZEROPS_TOKEN as GitHub secret
7. [CI/CD Actions] Verify Actions Workflow permissions = Read and write
8. [CI/CD Actions] Write `.github/workflows/deploy.yml` to container
9. [CI/CD Actions] Commit + push (triggers first deploy)
10. [CI/CD Webhook] GUI walkthrough in Zerops dashboard
11. Verify (for CI/CD paths)
12. Report to user

---

## 1. Confirm repo URL

```
ssh {targetHostname} "cd /var/www && git remote get-url origin 2>/dev/null"
```

Empty → ask user: "Repo URL? (e.g. `https://github.com/user/repo`)". Store
the URL for later tasks. If the repo does not yet exist, tell user to
create it (empty, no README, no .gitignore — you populate from the
container).

## 2. Choose sub-mode

Ask the user:

> "Do you want to just push code to the remote, or set up full CI/CD
> (push on {branchName} → automatic deploy)?"

| Answer | Tasks to run |
|---|---|
| Push only | 3 → 4. Done. |
| Full CI/CD via GitHub Actions | 3 → 5 → 6 → 7 → 8 → 9 → 11 → 12 |
| Full CI/CD via Webhook (GitHub or GitLab) | 3 → 10 → 11 → 12 |
| Both (push first, then add CI/CD) | 3 → 4, then 5–11 |

## 3. Get + set GIT_TOKEN

Required token permissions:

| Sub-mode | GitHub fine-grained token | GitLab access token |
|---|---|---|
| Push only | Contents: Read and write | `write_repository` |
| CI/CD Actions | Contents + Secrets + Workflows, all Read and write | `write_repository` (Webhook only) |
| CI/CD Webhook | Same as push-only | `api` (+ `write_repository`) |

Ask the user for the token. Store:

```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

## 4. [push-only] Commit + first push

```
ssh {targetHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"
zerops_deploy targetService="{targetHostname}" strategy="git-push" remoteUrl="{repoUrl}" branch="main"
```

The tool handles `.netrc`, `git init`, `git config`, `git remote add`
automatically. Do NOT run those yourself.

## 5. [CI/CD Actions] Get Zerops deploy token

Ask the user:

> "Use the existing ZCP API key as the deploy token, or create a scoped
> token at https://app.zerops.io/settings/token-management?"

Store the chosen token value for task 6.

## 6. [CI/CD Actions] Set ZEROPS_TOKEN as GitHub secret

Preferred (`gh` CLI on your dev machine):

```
gh secret set ZEROPS_TOKEN --repo {owner}/{repoName} --body "{zeropsToken}"
```

If `gh` is unavailable / not authenticated, guide the user via UI:

> GitHub repo → **Settings** → **Secrets and variables** → **Actions** →
> **New repository secret** → Name: `ZEROPS_TOKEN`, Value: the token.

## 7. [CI/CD Actions] Verify Workflow permissions

Ask the user to verify (or set):

> Repo → **Settings** → **Actions** → **General** → **Workflow permissions**
> → "Read and write permissions"

## 8. [CI/CD Actions] Write the workflow file

Find the service ID and setup name you need:

```
zerops_discover service="{targetHostname}"                            # serviceId
ssh {targetHostname} "grep -E '^\s*- setup:' /var/www/zerops.yaml"    # setup names
```

Setup mapping: dev/iteration target → `dev`, stage/prod target → `prod`.

Write the workflow file (replace `{serviceId}`, `{setup}`, `{branchName}`):

```
ssh {targetHostname} "mkdir -p /var/www/.github/workflows"
ssh {targetHostname} "cat > /var/www/.github/workflows/deploy.yml" <<'YAML'
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
YAML
```

Multiple deploy targets? Add one deploy step per target with its own
`--serviceId` and `--setup`.

## 9. [CI/CD Actions] Commit + push

```
ssh {targetHostname} "cd /var/www && git add -A && git commit -m 'ci: add deploy workflow'"
zerops_deploy targetService="{targetHostname}" strategy="git-push" remoteUrl="{repoUrl}" branch="main"
```

First push triggers both the Zerops build (via git-push) AND GitHub
Actions job (pushes back to Zerops via `zcli push` — redundant but
harmless first time; verifies CI path works).

## 10. [CI/CD Webhook] GUI walkthrough

Guide the user through the Zerops GUI:

> 1. Open **https://app.zerops.io/service-stack/{serviceId}/deploy**
>    (or: dashboard → project → service **{targetHostname}** → **Deploy** tab)
> 2. Click **"Connect with a GitHub repository"** (or GitLab).
> 3. OAuth popup → log in, grant access.
>    **ADMIN rights on the repo required** — Zerops creates a webhook.
> 4. Select repository **{owner}/{repoName}**.
> 5. Configure build trigger: **Push to branch** → **{branchName}**
>    (or: **New tag** with optional regex filter like `v*`).
> 6. Check **"Trigger automatic builds"**.
> 7. Click **Save**.

Tell the user to confirm when done — you'll verify via `zerops_events`.

## 11. Verify

```
zerops_events serviceHostname="{targetHostname}" limit=5
```

Look for `stack.build` in RUNNING or FINISHED state. If not present:

| Problem | Fix |
|---|---|
| `[ci skip]` / `[skip ci]` in commit | Remove marker, push again |
| Actions not triggering | `.github/workflows/` must be on {branchName}; check repo → Actions tab |
| Webhook not triggering | Zerops dashboard → Deploy tab → integration status; re-authorize if needed |
| `ZEROPS_TOKEN` not found in Actions | Secret name must match exactly; Workflow permissions must be Read+write |
| `zcli: command not found` in CI | Install step missing `echo "$HOME/.local/bin" >> $GITHUB_PATH` |
| `Cannot find corresponding setup` | Add/fix `--setup` flag in `zcli push` command |
| Build timeout | 60-minute hard limit; inspect `zerops_logs` for slow steps |

## 12. Report

Tell the user:

```
push-git configured for {targetHostname} → {repoUrl} (branch {branchName}).

[push-only]: commit + `zerops_deploy targetService="{targetHostname}" strategy="git-push"` to push.
[CI/CD]: any commit to {branchName} triggers automatic build + deploy.

Iterate:
  ssh {targetHostname} 'cd /var/www && ...'            # edit
  zerops_deploy targetService="{targetHostname}" strategy="git-push"  # manual push (CI picks it up too)
```
