---
id: develop-push-git-deploy-local
priority: 2
phases: [develop-active]
deployStates: [deployed]
strategies: [push-git]
environments: [local]
title: "Push-git strategy — deploy via git push"
references-atoms: [strategy-push-git-push-local, develop-platform-rules-local]
---

### Push-Git Deploy Strategy

Local push-git uses the user's own git credentials (SSH keys, Keychain,
credential manager). `GIT_TOKEN` and `.netrc` are container-only and do
not apply here. Setup details (origin, first push, auth troubleshooting):
`strategy-push-git-push-local`.

**Each deploy:**

```
git -C <your-project-dir> add -A
git -C <your-project-dir> commit -m "{what changed}"
zerops_deploy targetService="{hostname}" strategy="git-push"
```

Pass `remoteUrl="{url}"` only on first push if `origin` isn't set —
`zerops_deploy` refuses to silently rewrite a mismatched existing origin.
A dirty tree warns via `warnings[]` but does not block; uncommitted
changes are NOT pushed.

CI/CD trigger (webhook or Actions) lands the build automatically once
configured via the strategy setup atom — push itself doesn't change.
Without CI/CD, cross-deploy to stage manually:
`zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"`.

**Configure/re-configure push-git:** `zerops_workflow action="strategy" strategies={"{hostname}":"push-git"}`
**Switch strategy:** `zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}`
