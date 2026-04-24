---
id: strategy-push-git-push-container
priority: 2
phases: [strategy-setup]
strategies: [push-git]
environments: [container]
title: "push-git push setup — container env (GIT_TOKEN + .netrc)"
references-fields: [ops.DeployResult.Status, ops.DeployResult.Warnings, platform.APIError.Code]
---

# Push path — container env

The container has no user credentials, so pushes to the external git
remote run under `GIT_TOKEN`.

## 1. Set `GIT_TOKEN` as a project env var

Token scopes:

| Host | Minimum scope |
|---|---|
| GitHub fine-grained | `Contents: Read and write` (push-only); add `Secrets` + `Workflows` if pairing with the Actions trigger atom |
| GitLab personal access | `write_repository` (push-only); add `api` if pairing with the Webhook trigger atom |

```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

## 2. Commit + first push

```
ssh {targetHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"

zerops_deploy targetService="{targetHostname}" strategy="git-push" \
  remoteUrl="{repoUrl}" branch="main"
```

The response's `status` confirms the push. If `platform.APIError.code`
is `PREREQUISITE_MISSING: requires committed code`, the container's
`/var/www` has no commit — run the ssh commit step again and retry.

Do not run `git init`, `.netrc` configuration, or `git remote add`
manually — the deploy tool owns the git-push shape.

## Ongoing pushes

Subsequent pushes omit `remoteUrl`:

```
ssh {targetHostname} "cd /var/www && git add -A && git commit -m '...'"
zerops_deploy targetService="{targetHostname}" strategy="git-push"
```

The trigger setup (webhook or actions) in the paired trigger atom
handles what Zerops does after the push lands.
