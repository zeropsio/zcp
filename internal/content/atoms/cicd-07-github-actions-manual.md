---
id: cicd-07-github-actions-manual
priority: 2
phases: [cicd-active]
title: "CI/CD — GitHub Actions manual configuration"
---

## GitHub Actions — Manual Configuration

### 1. Get service ID for deploy target

From `zerops_discover`:
```
zerops_discover service="{targetHostname}"
```
The `serviceId` field is needed for the workflow file.

Or: Zerops dashboard → service → three-dot menu → **Copy Service ID**

### 2. Create Zerops access token

"Go to: https://app.zerops.io/settings/token-management → **Generate** a new token.
 Copy it — you'll need it in the next step."

### 3. Add GitHub secret

"Go to: GitHub repo → **Settings** → **Secrets and variables** → **Actions**
 → **New repository secret**
 → Name: `ZEROPS_TOKEN`
 → Value: the Zerops token from step 2

 Also verify: **Settings** → **Actions** → **General** → **Workflow permissions** is set to **Read and write permissions**."

### 4. Create workflow file

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

For multiple services, add one deploy step per target, each with its own `--serviceId` and `--setup`.

### 5. Commit and push

```bash
ssh {devHostname} "cd /var/www && git add -A && git commit -m 'ci: add deploy workflow'"
```
```
zerops_deploy targetService="{devHostname}" strategy="git-push"
```

First push to main triggers deploy.
