---
id: cicd-05-git-setup
priority: 2
phases: [cicd-active, export-active]
title: "CI/CD — git authentication and repository preparation"
---

## Git Authentication

### GIT_TOKEN — Project-Level Env Var

For pushing code from a Zerops container to GitHub/GitLab, set a project-
level token:

"I need a GitHub fine-grained token to push code.

 **For push-only (Option A):**
 GitHub → Settings → Developer settings → Fine-grained tokens → select repo
 Permissions: **Contents: Read and write**

 **For full CI/CD (Option B):**
 Same path, but three permissions:
 - **Contents: Read and write** (push code)
 - **Secrets: Read and write** (set ZEROPS_TOKEN secret)
 - **Workflows: Read and write** (create .github/workflows/deploy.yml)

 **GitLab alternative:** User Settings → Access Tokens → Scope: **write_repository**

 Paste the token here — I'll store it as a project env var."

After user provides token:
```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

### .netrc for authentication

Before any git push from a container, create .netrc (token NOT in URL, NOT
in command args):

GitHub:
```bash
ssh {devHostname} 'echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

GitLab:
```bash
ssh {devHostname} 'echo "machine gitlab.com login oauth2 password $GIT_TOKEN" > ~/.netrc && chmod 600 ~/.netrc'
```

**.netrc is lost on deploy** (new container). Recreate before each push
session. The GIT_TOKEN env var persists across deploys.

## Git Preparation

SSH to the dev container and assess current state:
```bash
ssh {devHostname} "cd /var/www && git remote -v 2>/dev/null && git status 2>/dev/null && git branch 2>/dev/null"
```

**If no .gitignore** — write one via SSH:
```bash
ssh {devHostname} "cd /var/www && echo 'node_modules/\nvendor/\n.env\n.env.*\n*.log\ndist/\nbuild/\n.cache/' > .gitignore"
```
Customize for framework: `zerops_knowledge query="{runtime} gitignore"`.

**If no git initialized:**
```bash
ssh {devHostname} "cd /var/www && git init -q -b main && git config user.email 'deploy@zerops.io' && git config user.name 'ZCP'"
```

**If no remote** (git remote -v shows nothing):
```bash
ssh {devHostname} "cd /var/www && git remote add origin {repoUrl}"
```

**If remote already configured** — use existing. Don't overwrite unless the
user requests it.

### Push safety hook

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

### Push initial code

Commit first, then push (the tool handles .netrc auth + remote setup):
```bash
ssh {devHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"
```
```
zerops_deploy targetService="{devHostname}" strategy="git-push" remoteUrl="{repoUrl}"
```

If push fails with an authentication error:
1. Verify GIT_TOKEN is set: `zerops_discover service="{devHostname}" includeEnvs=true` — look for GIT_TOKEN.
2. Retry the push: `zerops_deploy targetService="{devHostname}" strategy="git-push"`.
