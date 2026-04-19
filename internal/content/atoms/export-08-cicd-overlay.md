---
id: export-08-cicd-overlay
priority: 2
phases: [export-active]
title: "Export — CI/CD setup overlay (intent A or C)"
---

## CI/CD Setup (Intent A or C)

Skip this section if intent is B (buildFromGit only).

### Choose Approach

| Approach | When to use | How it works |
|----------|------------|--------------|
| **GitHub Actions** | Repo on GitHub | Workflow file in repo, runs zcli push |
| **GitHub webhook** | Repo on GitHub | Zerops pulls code via webhook (GUI setup) |
| **GitLab webhook** | Repo on GitLab | Zerops pulls code via webhook (GUI setup) |

### GitHub Actions (recommended for GitHub repos)

1. **Create Zerops access token:**
   "Go to: https://app.zerops.io/settings/token-management → Generate a new token"

2. **Add GitHub secret:**
   "Go to: GitHub repo → Settings → Secrets and variables → Actions
    → New repository secret
    → Name: `ZEROPS_TOKEN`, Value: the token you just created"

3. **Create workflow file:**
   ```bash
   ssh {devHostname} "mkdir -p /var/www/.github/workflows"
   ```
   Write `.github/workflows/deploy.yml`:
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

4. **Commit and push:**
   ```bash
   ssh {devHostname} "cd /var/www && git add -A && git commit -m 'add CI/CD workflow' && git push"
   ```

5. **Verify:** First push to main triggers deploy.
   ```
   zerops_events serviceHostname="{targetHostname}" limit=5
   ```

### GitHub/GitLab Webhook (GUI setup required)

Guide user through the Zerops GUI OAuth flow:

"Set up automatic deploy via webhook:

 1. Open: https://app.zerops.io/service-stack/{serviceId}/deploy
    (or: Zerops dashboard → project → service {targetHostname} → Deploy tab)

 2. Click **'Connect with a GitHub repository'** (or GitLab)

 3. A GitHub/GitLab authorization popup will open.
    Log in and grant Zerops access to your repositories.

    **IMPORTANT:** You need **ADMIN rights** on the repository.
    If the repo doesn't appear in the list, check your permissions
    on GitHub/GitLab.

 4. Select repository: **{owner}/{repoName}**

 5. Configure the build trigger:
    • **Push to branch** → select **'{branchName}'** (recommended)
    • Or: **New tag** (with optional regex filter like `v*`)

 6. Make sure **'Trigger automatic builds'** is checked.

 7. Click **Save**.

 Tell me when you're done — I'll verify the webhook is working."

After user confirms:
```
zerops_events serviceHostname="{targetHostname}" limit=5
```

If no build event within 2 minutes:
- Check commit message doesn't contain `[ci skip]` or `[skip ci]`
- GitHub Actions: verify workflow file is on the correct branch, check Actions tab
- Webhook: verify connection in Zerops dashboard Deploy tab
- Verify the access token / webhook authorization is valid
