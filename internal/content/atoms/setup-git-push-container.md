---
id: setup-git-push-container
priority: 2
phases: [strategy-setup]
gitPushStates: [unconfigured, broken, unknown]
environments: [container]
title: "Configure git-push capability on the container"
references-fields: [ops.DeployResult.Status, ops.DeployResult.Warnings, ops.DeployResult.FailureClassification]
---
The runtime container has no user credentials, so pushes to an external git remote run under `GIT_TOKEN`. Set the token, walk through a first push, then mark the capability configured so deploy and build-integration actions stop blocking on the prereq chain.

## 1. Set `GIT_TOKEN` as a project env var

Token scopes (push-only is the minimum; add scopes for the build-integration you plan to wire):

| Host | Minimum scope |
|---|---|
| GitHub fine-grained | `Contents: Read and write`. Add `Secrets` + `Workflows` if you plan `build-integration=actions`. |
| GitLab personal access | `write_repository`. Add `api` if you plan `build-integration=webhook`. |

```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

## 2. Commit + first push

```
ssh {hostname} "cd /var/www && git add -A && git commit -m 'initial commit'"

zerops_deploy targetService="{hostname}" strategy="git-push" \
  remoteUrl="{repoUrl}" branch="main"
```

The response's `status` confirms the push. On failure, read `failureClassification` first — `category=credential` indicates a missing or rejected `GIT_TOKEN`; `category=config` with a committed-code cause means `/var/www` has no commit yet (re-run the ssh commit step). `zerops_deploy strategy="git-push"` handles `git init`, `.netrc` configuration, and `git remote add` internally — these are not separate manual steps.

## 3. Confirm setup

After the first push lands:

```
zerops_workflow action="git-push-setup" service="{hostname}" \
  remoteUrl="{repoUrl}"
```

This stamps `GitPushState=configured` plus `RemoteURL`. Now `zerops_deploy strategy=git-push` deploys without re-running through this atom, and `action=build-integration` stops blocking on the prereq chain.
