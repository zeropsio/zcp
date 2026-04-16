# CI/CD Setup: Connect Git Repository to Zerops

## What Do You Need?

Ask the user first:
"Do you want to **just push code** to a remote repository, or set up **full CI/CD** (push triggers automatic deploy to Zerops)?"

### Option A: Just push code to remote

**GitHub fine-grained token permissions: Contents: Read and write** (that's all)
- GitHub → Settings → Developer settings → Fine-grained tokens → select repo → Permissions: **Contents: Read and write**
- GitLab alternative: User Settings → Access Tokens → Scope: **write_repository**

That's all. Skip to **Git Authentication** section below.

### Option B: Full CI/CD (push → automatic deploy)

**GitHub fine-grained token permissions: Contents: Read and write + Actions secrets: Read and write + Workflows: Read and write**

**Requirements — gather ALL of these before starting:**
1. **Git push token with CI/CD permissions** — needs Contents + Secrets + Workflows (three permissions above) for pushing code AND creating the workflow file + setting secrets
2. **Zerops deploy token** — for CI/CD to deploy back to Zerops
   - Use the existing ZCP API key (ask user: "Can I use the existing API key as the deploy token? It has full project access. For a scoped token, create one at https://app.zerops.io/settings/token-management")
   - Or user creates a dedicated token at https://app.zerops.io/settings/token-management
3. **GitHub repo secret `ZEROPS_TOKEN`** — store the deploy token as a secret
   - Via `gh` CLI: `gh secret set ZEROPS_TOKEN --repo {owner}/{repo} --body "{zeropsToken}"`
   - Or manually: repo **Settings** → **Secrets and variables** → **Actions** → **New repository secret** → Name: `ZEROPS_TOKEN`, Value: the deploy token
4. **GitHub Actions permissions** — the repo must allow workflows to run
   - **Settings** → **Actions** → **General** → **Actions permissions**: "Allow all actions" (or at minimum allow actions from the repository)
   - **Settings** → **Actions** → **General** → **Workflow permissions**: "Read and write permissions"
5. **Service ID** of the deploy target — get via `zerops_discover service="{targetHostname}"`

Verify all prerequisites before generating the workflow file. This prevents the "push workflow → CI fails → fix permissions → push again" loop.

---

## Choose Your Approach

| Approach | When to use | How it works |
|----------|------------|--------------|
| **GitHub Actions (auto)** | Repo on GitHub + `gh` CLI available | Auto-set secret + generate workflow via `gh` |
| **GitHub Actions (manual)** | Repo on GitHub | User manually adds secret + workflow file |
| **GitHub webhook** | Repo on GitHub (alternative) | Zerops GUI webhook triggers build on push/tag |
| **GitLab webhook** | Repo on GitLab | Zerops GUI webhook triggers build on push/tag |

**Recommendation:** GitHub Actions (auto) when `gh` CLI is installed — fastest path, zero browser visits. Otherwise GitHub Actions (manual). GitLab webhook for GitLab repos.

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

---

## Git Authentication

### GIT_TOKEN — Project-Level Env Var

For pushing code from a Zerops container to GitHub/GitLab, set a project-level token:

"I need a GitHub fine-grained token to push code.

 **For push-only (Option A):**
 GitHub → Settings → Developer settings → Fine-grained tokens → select repo
 Permissions: **Contents: Read and write**

 **For full CI/CD (Option B):**
 Same path, but three permissions:
 - **Contents: Read and write** (push code)
 - **Actions secrets: Read and write** (set ZEROPS_TOKEN secret)
 - **Workflows: Read and write** (create .github/workflows/deploy.yml)

 **GitLab alternative:** User Settings → Access Tokens → Scope: **write_repository**

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

Commit first, then push (tool handles .netrc auth, remote setup):
```bash
ssh {devHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"
```
```
zerops_deploy targetService="{devHostname}" strategy="git-push" remoteUrl="{repoUrl}"
```

If push fails with authentication error:
1. Verify GIT_TOKEN is set: `zerops_discover service="{devHostname}" includeEnvs=true` — look for GIT_TOKEN
2. Retry the push: `zerops_deploy targetService="{devHostname}" strategy="git-push"`

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
zerops_events serviceHostname="{targetHostname}" limit=5
```

**Check for build triggered** — look for `stack.build` process in RUNNING or FINISHED state.

**If no build triggers:**
- Check commit message doesn't contain `[ci skip]` or `[skip ci]`
- GitHub Actions: verify workflow file is on the correct branch, check GitHub Actions tab
- Webhook: verify connection in Zerops dashboard → Deploy tab → check integration status
- Verify access token is valid (not expired)
- Verify the push was to the monitored branch
- Verify GitHub Actions permissions: repo Settings → Actions → General → Workflow permissions must be "Read and write"

**If build fails:**
```
zerops_logs serviceHostname="{targetHostname}" severity=error
```
Deploy creates a NEW container — local files from dev are NOT carried over. Only `deployFiles` content survives.

**If build succeeds:**
```
zerops_verify serviceHostname="{targetHostname}"
```

Present to user: stage URL, repo URL, explain: "Push to {branch} → automatic deploy to {targetHostname}."

---

## Error Recovery

| Problem | Solution |
|---------|---------|
| Push rejected (auth) | Recreate .netrc, verify GIT_TOKEN env var |
| Push rejected (non-fast-forward) | `git pull --rebase` then push again |
| zcli install fails in CI | Check network access, verify `curl` is available on runner |
| `zcli: command not found` | Ensure install step has `echo "$HOME/.local/bin" >> $GITHUB_PATH` |
| `Cannot find corresponding setup` | Add `--setup {name}` to the zcli push command (e.g. `--setup prod`) |
| `ZEROPS_TOKEN` not available | Verify secret name matches exactly, check workflow permissions are Read and write |
| Webhook not triggering | Check Zerops dashboard → Deploy → integration status, re-authorize if needed |
| Actions not triggering | Verify `.github/workflows/` is on the correct branch, check Actions tab for errors |
| Build timeout | Builds have 60-minute hard limit. Check build logs for slow steps. |
| Wrong branch deploying | Verify trigger config: branch name must match exactly |
| `[ci skip]` in commit message | Remove `[ci skip]` / `[skip ci]` from commit message, push again |
