---
id: develop-close-push-git
priority: 7
phases: [develop-active]
deployStates: [deployed]
strategies: [push-git]
title: "Close task — push-git strategy"
---

### Closing the task

When code changes are complete, ask the user: **push code only, or set up
full CI/CD** (automatic deploy on every push)?

#### Option A — Push code to remote

One-time prerequisites:

- `GIT_TOKEN` project env var — GitHub fine-grained token with
  `Contents: Read and write`, or GitLab token with `write_repository`.
- `.netrc` on the dev container for git auth:

  ```
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
    'umask 077 && echo "machine github.com login oauth2 password $GIT_TOKEN" > ~/.netrc'
  ```

Then commit the code on the dev container and push via
`zerops_deploy strategy="git-push"`. Repeat per dev service if you
have more than one.

#### Option B — Full CI/CD

```
zerops_workflow action="start" workflow="cicd"
```

Provisions a GitHub Action that deploys to Zerops on every remote push.
