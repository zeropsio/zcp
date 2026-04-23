---
id: strategy-push-git-intro
priority: 1
phases: [strategy-setup]
strategies: [push-git]
triggers: [unset]
title: "push-git setup — pick your downstream trigger"
---

# Configure push-git for `{targetHostname}`

ZCP orchestrates two distinct concerns in a push-git setup: **pushing
code** to a git remote, and **what happens at that remote** after the
push. The trigger choice here answers only the second — the first lives
in the env-specific push atoms that follow.

## Two trigger options

| Trigger | What it means | When to pick it |
|---|---|---|
| `webhook` | Zerops OAuths with GitHub/GitLab; pushes to the remote fire a webhook, Zerops pulls + builds. No in-repo YAML. | Simple setup, single service, branch-based. |
| `actions` | `.github/workflows/deploy.yml` in the repo runs `zcli push`. The user's CI owns the build trigger. | Monorepos, custom branch routing, multi-service builds, existing Actions infrastructure. |

> **You can't pick "neither" as a push-git trigger.** A push-git service
> without a downstream trigger is functionally `manual` — the git push
> succeeds but nothing builds. If that's the intent, set
> `strategies={"{targetHostname}":"manual"}` instead.

## Confirm the repo URL first

Before picking a trigger, confirm the git remote exists. In container env:

```
ssh {targetHostname} "cd /var/www && git remote get-url origin 2>/dev/null"
```

In local env:

```
git -C <your-project-dir> remote get-url origin
```

Empty output → ask the user for a repo URL and create the remote (empty
repo, no README, no `.gitignore` — the first push populates it).

## Next step

Ask the user which trigger they want, then call:

```
zerops_workflow action="strategy" \
  strategies={"{targetHostname}":"push-git"} \
  trigger="webhook"   # or "actions"
```

The follow-up response carries the trigger-specific setup flow plus the
env-specific push instructions.
