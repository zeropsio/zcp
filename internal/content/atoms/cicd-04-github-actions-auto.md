---
id: cicd-04-github-actions-auto
priority: 2
phases: [cicd-active]
title: "CI/CD — GitHub Actions automated setup"
---

## GitHub Actions — Automated Setup

**Prerequisites:** `gh` CLI installed and authenticated (`gh auth status` must succeed). All items from the requirements checklist above.

### 1. Get service ID

```
zerops_discover service="{targetHostname}"
```
Note the `serviceId` field.

### 2. Set GitHub secret

If not already done during prerequisites:
```bash
gh secret set ZEROPS_TOKEN --repo {owner}/{repo} --body "{zeropsToken}"
```

### 3. Write workflow file and push

Write `.github/workflows/deploy.yml` on the container:
```bash
ssh {devHostname} "mkdir -p /var/www/.github/workflows"
```

Content of `.github/workflows/deploy.yml`:
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
- `actions/checkout@v4` is required — `zcli push` sends the checked-out directory contents to Zerops
- `--setup {setup}` selects which zerops.yaml entry to use (e.g. `prod` for stage services, `dev` for dev-only)
- For multiple services, add one deploy step per target, each with its own `--serviceId` and `--setup`

Commit and push:
```bash
ssh {devHostname} "cd /var/www && git add -A && git commit -m 'ci: add deploy workflow'"
```
```
zerops_deploy targetService="{devHostname}" strategy="git-push"
```

First push to main triggers deploy.

### If `gh` is not available

Fall back to the manual GitHub Actions setup below.
