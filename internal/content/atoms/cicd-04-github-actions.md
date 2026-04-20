---
id: cicd-04-github-actions
priority: 2
phases: [cicd-active, export-active]
title: "CI/CD — GitHub Actions setup"
---

## GitHub Actions — Setup

### 1. Get service ID

```
zerops_discover service="{targetHostname}"
```
Note the `serviceId`.

### 2. Create Zerops access token

Ask the user: "Use the existing ZCP API key, or create a scoped token at
https://app.zerops.io/settings/token-management?"

### 3. Set the `ZEROPS_TOKEN` secret

Run `gh auth status` first. If it succeeds, set the secret automatically:

```bash
gh secret set ZEROPS_TOKEN --repo {owner}/{repo} --body "{zeropsToken}"
```

If `gh` is not available / not authenticated, guide the user:

"Go to: GitHub repo → **Settings** → **Secrets and variables** → **Actions**
 → **New repository secret** → Name: `ZEROPS_TOKEN`, Value: the token.

 Also verify **Settings** → **Actions** → **General** → **Workflow
 permissions** is **Read and write**."

### 4. Write the workflow file

```bash
ssh {devHostname} "mkdir -p /var/www/.github/workflows"
```

`.github/workflows/deploy.yml`:

```yaml
name: Deploy to Zerops
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
          echo "$HOME/.local/bin" >> $GITHUB_PATH
      - name: Deploy to {targetHostname}
        run: zcli push --serviceId {serviceId} --setup {setup}
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
```

**Important:**
- `actions/checkout@v4` is required — `zcli push` sends the checked-out directory to Zerops.
- `--setup {setup}` selects the zerops.yaml entry (`prod` for stage, `dev` for dev-only).
- For multiple services, add one deploy step per target with its own `--serviceId` and `--setup`.

### 5. Commit and push

```bash
ssh {devHostname} "cd /var/www && git add -A && git commit -m 'ci: add deploy workflow'"
```
```
zerops_deploy targetService="{devHostname}" strategy="git-push"
```

First push to main triggers deploy.
