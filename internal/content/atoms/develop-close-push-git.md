---
id: develop-close-push-git
priority: 7
phases: [develop-active]
strategies: [push-git]
title: "Close task — push-git strategy"
---

### Closing the task

When code changes are complete, ask the user: **push code only, or set up
full CI/CD** (automatic deploy on every push)?

#### Option A — Push code to remote

**Prerequisites (one-time setup):**

- `GIT_TOKEN` project env var — a GitHub fine-grained token with
  `Contents: Read and write`, or a GitLab token with `write_repository`.
- `.netrc` on the dev container for git auth:

  ```
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
    'umask 077 && echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc'
  ```

**Steps:**

1. Commit: `ssh {hostname} "cd /var/www && git add -A && git commit -m 'description'"`
2. Push: `zerops_deploy targetService="{hostname}" strategy="git-push"`

Repeat per dev service if you have more than one.

#### Option B — Full CI/CD

```
zerops_workflow action="start" workflow="cicd"
```

This workflow provisions a GitHub Action that runs `zcli push` on every
remote push — no manual deploy required afterwards.
