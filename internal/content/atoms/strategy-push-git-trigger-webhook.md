---
id: strategy-push-git-trigger-webhook
priority: 3
phases: [strategy-setup]
strategies: [push-git]
triggers: [webhook]
title: "push-git trigger — Zerops webhook (dashboard walkthrough)"
---

# Trigger — Zerops webhook

Zerops owns the trigger through an OAuth integration with
GitHub/GitLab. No in-repo YAML, no secrets in the user's repo.

## GUI walkthrough (dashboard)

Guide the user step-by-step:

> 1. Open **https://app.zerops.io/service-stack/{serviceId}/deploy**
>    (or: dashboard → project → service **{targetHostname}** → **Deploy**
>    tab).
> 2. Click **"Connect with a GitHub repository"** (or GitLab equivalent).
> 3. OAuth popup → log in, grant access.
>    **ADMIN rights on the repo are required** — Zerops creates the
>    webhook under your account.
> 4. Select repository **{owner}/{repoName}**.
> 5. Configure build trigger:
>    - **Push to branch** → enter `{branchName}` (typically `main`), or
>    - **New tag** with an optional regex filter (e.g. `v*`).
> 6. Check **"Trigger automatic builds"**.
> 7. Click **Save**.

`{serviceId}` comes from `zerops_discover service="{targetHostname}"`.

## Verify

After the user confirms the dashboard steps, probe for build activity:

```
zerops_events serviceHostname="{targetHostname}" limit=5
```

Look for `stack.build` in RUNNING or FINISHED state. If absent:

| Problem | Fix |
|---|---|
| OAuth revoked | Dashboard → Deploy tab → Re-authorize |
| Wrong branch | Check the trigger rule matches the branch you push to |
| `[ci skip]` / `[skip ci]` in last commit | Remove marker, push again |

## Report

```
push-git + webhook configured for {targetHostname}.
Every push to {branchName} triggers a Zerops build automatically.

Iterate:
  zerops_deploy targetService="{targetHostname}" strategy="git-push"
```

(The next push — whether via `zerops_deploy strategy=git-push` or the
user's own `git push` — will fire the webhook.)
