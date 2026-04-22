---
id: develop-close-push-git-local
priority: 7
phases: [develop-active]
deployStates: [deployed]
strategies: [push-git]
environments: [local]
title: "Close task — push-git strategy (local)"
---

### Closing the task — local + push-git

The dev surface is your working directory; committed code pushes out
via your own git credentials. ZCP invokes git — no `GIT_TOKEN` on the
Zerops project, no `.netrc`.

```
git -C <your-project-dir> add -A
git -C <your-project-dir> commit -m "{your-description}"
zerops_deploy targetService="{hostname}" strategy="git-push"
```

The tool's pre-flight refuses without committed code AND without an
origin. If push-git isn't configured yet (missing trigger, no origin),
go through the central deploy-config action:

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-git"}
```

That returns the trigger intro atom + the env-specific push atom (which
uses your local git, not a container token).
