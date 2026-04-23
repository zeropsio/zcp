---
id: strategy-push-git-push-container
priority: 2
phases: [strategy-setup]
strategies: [push-git]
environments: [container]
title: "push-git push setup — container env (GIT_TOKEN + .netrc)"
---

# Push path — container env

The container has no user credentials, so pushes to the external git
remote run under `GIT_TOKEN` via a trap-cleaned `.netrc`. `zerops_deploy`
wires all of this automatically; the setup is:

## 1. Set `GIT_TOKEN` as a project env var

Ask the user for a git access token with these scopes:

| Host | Minimum scope |
|---|---|
| GitHub fine-grained | `Contents: Read and write` (push-only trigger); add `Secrets` + `Workflows` if pairing with the Actions trigger atom |
| GitLab personal access | `write_repository` (push-only trigger); add `api` if pairing with the Webhook trigger atom |

Then:

```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

## 2. Commit + first push

```
ssh {targetHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"

zerops_deploy targetService="{targetHostname}" strategy="git-push" \
  remoteUrl="{repoUrl}" branch="main"
```

`zerops_deploy` handles `.netrc` provisioning, `git remote add`, and
cleanup automatically — do NOT run those by hand. The tool's pre-flight
refuses when there's no committed code; make sure the commit above
lands before the push.

## Ongoing pushes

After the first push, subsequent pushes don't need `remoteUrl`:

```
ssh {targetHostname} "cd /var/www && git add -A && git commit -m '...'"
zerops_deploy targetService="{targetHostname}" strategy="git-push"
```

The trigger setup (webhook or actions) in the paired trigger atom
handles what Zerops does after the push lands.
