# CI/CD Setup: Connect Git Repository to Zerops

## How CI/CD Works

Push code to a git remote -> CI/CD deploys to Zerops service(s) automatically.
Each target service needs its own deploy configuration (GitHub Actions step or GitLab webhook connection).

The CI/CD targets section above (if present) shows your dev -> stage mapping from project configuration.
If no targets are listed, use `zerops_discover` to identify services or ask the user for target service IDs.

## Choose Your Approach

| Approach | When to use | How it works |
|----------|------------|--------------|
| **GitHub Actions** | Repo on GitHub | Workflow file in repo triggers `zeropsio/actions@main` |
| **GitLab Integration** | Repo on GitLab | Zerops GUI webhook triggers build on push/tag |

---

## Git Preparation

SSH to the dev container and assess current state:
  ssh {devHostname} "cd /var/www && git status && git remote -v && git branch"

Fill only what's missing:

**If no .gitignore** -- write one via SSH:
  Baseline: node_modules/ vendor/ .env .env.* *.log dist/ build/ .cache/
  Customize for framework: zerops_knowledge query="{runtime} gitignore"

**If no remote** (git remote -v shows nothing):
  ssh {devHostname} "cd /var/www && git remote add origin https://{token}@github.com/{owner}/{repo}.git"
  Token safety: guide user to create a personal access token. NEVER paste tokens in conversation.

**If remote already configured** -- use existing. Don't overwrite.

### Push safety hook

Block force push and branch deletion on main/master:
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

For additional protection, recommend enabling branch protection rules in repository settings.

### Push initial code

  ssh {devHostname} "cd /var/www && git add -A && git commit -m 'initial' && git push -u origin {branch}"
  Use whatever branch exists (from git branch output above).

---

## GitHub Actions Configuration

### 1. Get stage service ID

  Zerops dashboard -> stage service -> three-dot menu -> **Copy Service ID**
  Or from URL: https://app.zerops.io/service-stack/<service-id>/dashboard

### 2. Create Zerops access token

  Zerops dashboard -> [Settings -> Access Token Management](https://app.zerops.io/settings/token-management) -> Generate

### 3. Add GitHub secret

  Repository -> Settings -> Secrets and variables -> Actions -> Repository secrets
  Name: `ZEROPS_TOKEN`, Value: the generated token

### 4. Create workflow file

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
      - uses: zeropsio/actions@main
        with:
          access-token: ${{ secrets.ZEROPS_TOKEN }}
          service-id: {stageServiceId}
```

For multiple services, add one `zeropsio/actions` step per target service, each with its own `service-id`.

### 5. Commit and push

  ssh {devHostname} "cd /var/www && git add -A && git commit -m 'add CI/CD workflow' && git push"
  First push to main triggers deploy.

---

## GitLab Integration Configuration

GitLab uses Zerops GUI webhook (no CI pipeline file needed).
Zerops pulls code directly and runs the zerops.yaml build pipeline.

### 1. Connect repository in Zerops

  Zerops dashboard -> **stage service** -> Build, Deploy, Run Pipeline Settings
  -> **Connect with a GitLab repository**
  -> Authorize Zerops access (full repo access required for webhook)
  -> Select repository
  -> Set trigger: **Push to branch** -> `main`
  -> Confirm

Zerops creates a webhook. Builds trigger automatically on push.

For multiple services, connect each target service separately in the Zerops dashboard.

### 2. Trigger first build

  ssh {devHostname} "cd /var/www && git add -A && git commit -m 'trigger build' --allow-empty && git push"

---

## Verification

1. **Check build triggered** -- `zerops_events serviceHostname="{stageHostname}" limit=5`
2. **Wait for completion** -- monitor events until build FINISHED.
3. **Verify health** -- `zerops_verify serviceHostname="{stageHostname}"`

**If no build triggers:**
- Check commit message doesn't contain `[ci skip]` or `[skip ci]`
- GitHub: verify workflow file is on main branch, check Actions tab
- GitLab: verify webhook in Zerops dashboard, check trigger branch
- Both: verify access token is valid

**If build fails:**
- `zerops_logs serviceHostname="{stageHostname}" severity=error`
- Deploy creates NEW container -- local files from dev are NOT carried over

**Present to user:** stage URL, repo URL, explain: push to {branch} -> automatic deploy.
