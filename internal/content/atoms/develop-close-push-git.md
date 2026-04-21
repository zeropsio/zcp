---
id: develop-close-push-git
priority: 7
phases: [develop-active]
deployStates: [deployed]
strategies: [push-git]
title: "Close task — push-git strategy"
---

### Closing the task

If push-git is already set up (GIT_TOKEN present, remote configured),
commit on the dev container and push:

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && git add -A && git commit -m "{your-description}"'
zerops_deploy targetService="{hostname}" strategy="git-push"
```

If push-git isn't fully set up yet (first time, or you want to add
CI/CD), the central deploy-config action returns the full setup flow
(Option A push-only vs Option B full CI/CD, tokens, optional GitHub
Actions or webhook, first push):

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-git"}
```
