# CI/CD Setup: Connect Git Repository to Zerops

## How CI/CD Works

Push code to a git remote → CI/CD deploys to Zerops service(s) automatically.
Each target service needs its own deploy configuration (GitHub Actions step or GitLab webhook connection).

The CI/CD targets section above (if present) shows your dev → stage mapping from project configuration.
If no targets are listed, use `zerops_discover` to identify services or ask the user for target service IDs.

## Choose Your Approach

| Approach | When to use | How it works |
|----------|------------|--------------|
| **GitHub Actions** | Repo on GitHub | Workflow file in repo triggers `zeropsio/actions@main` |
| **GitHub webhook** | Repo on GitHub (alternative) | Zerops GUI webhook triggers build on push/tag |
| **GitLab webhook** | Repo on GitLab | Zerops GUI webhook triggers build on push/tag |

**Recommendation:** GitHub Actions for GitHub repos (config lives in repo, versioned). GitLab webhook for GitLab repos (only option — no Actions equivalent).

---

## Git Authentication

### GIT_TOKEN — Project-Level Env Var

For pushing code from a Zerops container to GitHub/GitLab, set a project-level token:

"I need a GitHub/GitLab token to push code.

 **For GitHub:**
 1. GitHub → Settings → Developer settings → Fine-grained tokens
 2. Select the target repository
 3. Permissions: **Contents → Read and write**
 4. Generate token

 **For GitLab:**
 1. GitLab → User Settings → Access Tokens
 2. Scope: **write_repository**
 3. Generate token

 Paste the token here — I'll store it as a project env var."

After user provides token:
```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

### .netrc for Authentication

Before any git push from a container, create .netrc (token NOT in URL, NOT in command args):

GitHub:
```bash
ssh {hostname} 'echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

GitLab:
```bash
ssh {hostname} 'echo "machine gitlab.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

**.netrc is lost on deploy** (new container). Recreate before each push session. The GIT_TOKEN env var persists across deploys.

---

## Git Preparation

SSH to the dev container and assess current state:
```bash
ssh {devHostname} "cd /var/www && git remote -v 2>/dev/null && git status 2>/dev/null && git branch 2>/dev/null"
```

Fill only what's missing:

**If no .gitignore** — write one via SSH:
```bash
ssh {devHostname} "cd /var/www && echo 'node_modules/\nvendor/\n.env\n.env.*\n*.log\ndist/\nbuild/\n.cache/' > .gitignore"
```
Customize for framework: `zerops_knowledge query="{runtime} gitignore"`

**If no git initialized:**
```bash
ssh {devHostname} "cd /var/www && git init -q -b main && git config user.email 'deploy@zerops.io' && git config user.name 'ZCP'"
```

**If no remote** (git remote -v shows nothing):
```bash
ssh {devHostname} "cd /var/www && git remote add origin {repoUrl}"
```

**If remote already configured** — use existing. Don't overwrite unless user requests it.

### Push Safety Hook

Block force push and branch deletion on main/master:
```bash
ssh {devHostname} 'mkdir -p /var/www/.git/hooks && cat > /var/www/.git/hooks/pre-push << '\''HOOK'\''
#!/bin/sh
while read local_ref local_sha remote_ref remote_sha; do
  branch="${remote_ref#refs/heads/}"
  case "$branch" in main|master)
    zero="0000000000000000000000000000000000000000"
    if [ "$local_sha" = "$zero" ]; then
      echo "BLOCKED: Deleting $branch denied." >&2; exit 1
    fi
    if [ "$remote_sha" != "$zero" ] && ! git merge-base --is-ancestor "$remote_sha" "$local_sha" 2>/dev/null; then
      echo "BLOCKED: Force push to $branch denied." >&2; exit 1
    fi
  esac
done
HOOK
chmod +x /var/www/.git/hooks/pre-push'
```

### Push Initial Code

```bash
ssh {devHostname} "cd /var/www && git add -A && git commit -m 'initial' && git push -u origin {branch}"
```

If push fails with authentication error:
1. Verify GIT_TOKEN is set: `zerops_discover service="{devHostname}" includeEnvs=true` — look for GIT_TOKEN
2. Verify .netrc exists: `ssh {devHostname} "test -f ~/.netrc && echo OK || echo MISSING"`
3. Recreate .netrc if missing (see above)

---

## GitHub Actions Configuration

### 1. Get service ID for deploy target

From `zerops_discover`:
```
zerops_discover service="{stageHostname}"
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
 → Value: the Zerops token from step 2"

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
      - uses: zeropsio/actions@main
        with:
          access-token: ${{ secrets.ZEROPS_TOKEN }}
          service-id: {stageServiceId}
```

For multiple services, add one `zeropsio/actions` step per target service, each with its own `service-id`.

### 5. Commit and push

```bash
ssh {devHostname} "cd /var/www && git add -A && git commit -m 'add CI/CD workflow' && git push"
```

First push to main triggers deploy.

---

## GitHub Webhook Configuration (GUI)

Alternative to Actions — Zerops pulls code directly via webhook.

Guide user through the Zerops GUI:

"Set up automatic deploy from GitHub via webhook:

 1. Open: **https://app.zerops.io/service-stack/{serviceId}/deploy**
    (or: Zerops dashboard → project → service **{hostname}** → **Deploy** tab)

 2. Find **'Build, Deploy, Run Pipeline Settings'**
    Click **'Connect with a GitHub repository'**

 3. A GitHub authorization popup will open — log in and grant access.

    **IMPORTANT:** You need **ADMIN rights** on the repository.
    Zerops creates a webhook which requires admin permissions.
    If the repo doesn't appear in the list, check your GitHub permissions.

 4. Select repository: **{owner}/{repoName}**

 5. Configure the build trigger:
    • **Push to branch** → select **'{branchName}'** (most common)
    • Or: **New tag** (with optional regex filter like `v*`)

 6. Make sure **'Trigger automatic builds'** is checked.

 7. Click **Save**.

 Tell me when you're done — I'll verify the webhook."

---

## GitLab Webhook Configuration (GUI)

GitLab uses the same GUI flow with GitLab OAuth instead of GitHub.

"Set up automatic deploy from GitLab:

 1. Open: **https://app.zerops.io/service-stack/{serviceId}/deploy**

 2. Click **'Connect with a GitLab repository'**

 3. GitLab authorization popup — log in and grant access.
    **Requires ADMIN rights** on the repository.

 4. Select repository: **{owner}/{repoName}**

 5. Configure trigger:
    • **Push to branch** → select **'{branchName}'**
    • Or: **New tag** (optional regex)

 6. Check **'Trigger automatic builds'** → **Save**.

 Tell me when done — I'll verify."

---

## Verification

After any CI/CD setup:

```
zerops_events serviceHostname="{stageHostname}" limit=5
```

**Check for build triggered** — look for `stack.build` process in RUNNING or FINISHED state.

**If no build triggers:**
- Check commit message doesn't contain `[ci skip]` or `[skip ci]`
- GitHub Actions: verify workflow file is on the correct branch, check GitHub Actions tab
- Webhook: verify connection in Zerops dashboard → Deploy tab → check integration status
- Verify access token is valid (not expired)
- Verify the push was to the monitored branch

**If build fails:**
```
zerops_logs serviceHostname="{stageHostname}" severity=error
```
Deploy creates a NEW container — local files from dev are NOT carried over. Only `deployFiles` content survives.

**If build succeeds:**
```
zerops_verify serviceHostname="{stageHostname}"
```

Present to user: stage URL, repo URL, explain: "Push to {branch} → automatic deploy to {stageHostname}."

---

## Error Recovery

| Problem | Solution |
|---------|---------|
| Push rejected (auth) | Recreate .netrc, verify GIT_TOKEN env var |
| Push rejected (non-fast-forward) | `git pull --rebase` then push again |
| Webhook not triggering | Check Zerops dashboard → Deploy → integration status, re-authorize if needed |
| Actions not triggering | Verify `.github/workflows/` is on the correct branch, check Actions tab for errors |
| Build timeout | Builds have 60-minute hard limit. Check build logs for slow steps. |
| Wrong branch deploying | Verify trigger config: branch name must match exactly |
| `[ci skip]` in commit message | Remove `[ci skip]` / `[skip ci]` from commit message, push again |
