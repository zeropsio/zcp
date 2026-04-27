---
id: strategy-push-git-push-container
priority: 2
phases: [strategy-setup]
strategies: [push-git]
environments: [container]
title: "push-git push setup (GIT_TOKEN + .netrc)"
references-fields: [ops.DeployResult.Status, ops.DeployResult.Warnings, ops.DeployResult.FailureClassification]
---

# Push path (GIT_TOKEN + .netrc)

The runtime container has no user credentials, so pushes to the external git
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

The response's `status` confirms the push. On failure, read
`failureClassification` first — it carries the matched `category`
(`credential` for missing/rejected `GIT_TOKEN`, `config` for missing
commits) plus a one-line `suggestedAction`. If category is `config`
and the cause names committed code, `/var/www` has no commit on the
runtime container — run the ssh commit step again and retry.

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
