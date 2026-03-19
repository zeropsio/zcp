# CI/CD Setup: Connect Git Repository to Zerops

## Overview

Set up automated deployments from a git repository. Choose a provider, configure the connection, and verify with a test push.

---

<section name="cicd-choose">
## Choose CI/CD Provider

Select based on your hosting and requirements:

| Provider | When to use | How it works |
|----------|------------|--------------|
| **GitHub Actions** | Repo on GitHub | Workflow file triggers `zcli push` on push to branch |
| **GitLab CI** | Repo on GitLab | Pipeline job triggers `zcli push` on push to branch |
| **Zerops Webhook** | Any git host | Zerops listens for webhook, pulls and builds on push |
| **Generic zcli** | Any CI system | Add `zcli push` step to your existing CI pipeline |

**Recommendation:**
- GitHub/GitLab → use their native CI (Actions/CI) for full control
- Other git hosts (Bitbucket, Gitea, etc.) → Zerops webhook is simplest
- Already have CI → generic zcli integration

Choose and report: `zerops_workflow action="complete" step="choose" attestation="Provider: github for repo owner/name"`

Before completing, set the provider: the attestation should clearly state which provider was chosen.
</section>

<section name="cicd-configure-github">
## Configure GitHub Actions

### Prerequisites
- Zerops Personal Access Token (from Zerops dashboard → Access Token Management)
- GitHub repository with push access

### Steps

1. **Create Zerops token** — guide user to Zerops dashboard → Settings → Access Token Management → Generate token
2. **Add GitHub secret** — Settings → Secrets → Actions → New secret:
   - Name: `ZEROPS_TOKEN`
   - Value: the generated token
3. **Create workflow file** — `.github/workflows/deploy.yml`:

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
      - uses: zeropsio/zcli-action@v1
        with:
          token: ${{ secrets.ZEROPS_TOKEN }}
          project: "{projectName}"
          service: "{hostname}"
```

4. **Commit and push** the workflow file

For multiple services, add multiple `zcli-action` steps or use a matrix strategy.

Report: `zerops_workflow action="complete" step="configure" attestation="GitHub Actions configured for {hostname}, workflow file committed"`
</section>

<section name="cicd-configure-gitlab">
## Configure GitLab CI

### Prerequisites
- Zerops Personal Access Token
- GitLab repository with maintainer access

### Steps

1. **Create Zerops token** — Zerops dashboard → Settings → Access Token Management
2. **Add CI variable** — Settings → CI/CD → Variables:
   - Key: `ZEROPS_TOKEN`
   - Value: the generated token
   - Protected: Yes, Masked: Yes
3. **Create pipeline file** — `.gitlab-ci.yml`:

```yaml
deploy:
  image: zeropsio/zcli:latest
  stage: deploy
  only: [main]
  script:
    - zcli login --zeropsToken $ZEROPS_TOKEN
    - zcli push {hostname} --projectName "{projectName}"
```

4. **Commit and push** the pipeline file

Report: `zerops_workflow action="complete" step="configure" attestation="GitLab CI configured for {hostname}, pipeline file committed"`
</section>

<section name="cicd-configure-webhook">
## Configure Zerops Webhook

### Steps

1. **Connect repository in Zerops dashboard:**
   - Service detail → Build, Deploy, Run Pipeline Settings → Connect with GitHub/GitLab
   - Or use Zerops CLI: configure the build trigger
2. **Set trigger branch** — typically `main` or `production`
3. **Verify webhook** — push a commit and check Zerops dashboard for build activity

Webhook is the simplest option — no CI files needed. Zerops pulls code directly and runs the build pipeline defined in zerops.yml.

Report: `zerops_workflow action="complete" step="configure" attestation="Webhook configured for {hostname} on branch main"`
</section>

<section name="cicd-configure-generic">
## Configure Generic zcli Push

For any CI system (Jenkins, CircleCI, Travis, etc.):

### Steps

1. **Install zcli** in your CI environment:
   ```
   npm i -g @zerops/zcli
   # or download binary from https://github.com/zeropsio/zcli/releases
   ```
2. **Set environment variable** `ZEROPS_TOKEN` with your Zerops Personal Access Token
3. **Add deploy step** to your CI pipeline:
   ```
   zcli login --zeropsToken $ZEROPS_TOKEN
   zcli push {hostname} --projectName "{projectName}"
   ```
4. **Trigger branch** — configure to run on push to main/production

Report: `zerops_workflow action="complete" step="configure" attestation="zcli push configured in CI pipeline for {hostname}"`
</section>

<section name="cicd-verify">
## Verify CI/CD Setup

1. **Trigger a deploy** — push a small change (e.g., update a comment) to the configured branch
2. **Monitor** — `zerops_process` or `zerops_events` to see the build trigger
3. **Wait for completion** — build should appear and complete
4. **Verify** — `zerops_verify serviceHostname="{hostname}"` must return healthy

**If no build triggers:**
- Check webhook configuration in Zerops dashboard
- Verify the branch name matches
- Check CI logs for authentication errors (invalid token)
- Verify `zerops.yml` exists with correct `setup:` entry

**If build fails:**
- Check build logs in Zerops dashboard or via `zerops_logs`
- Common issues: wrong buildCommands, missing dependencies, incorrect deployFiles

Report: `zerops_workflow action="complete" step="verify" attestation="Test push triggered build, deploy completed, service healthy"`
</section>
