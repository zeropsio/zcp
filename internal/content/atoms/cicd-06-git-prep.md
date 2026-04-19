---
id: cicd-06-git-prep
priority: 2
phases: [cicd-active]
title: "CI/CD — Git preparation"
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
