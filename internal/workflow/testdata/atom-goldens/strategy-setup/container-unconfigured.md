---
id: strategy-setup/container-unconfigured
atomIds: [setup-git-push-container]
description: "strategy-setup phase, in-container, GitPushState unconfigured — agent walks through GIT_TOKEN/.netrc setup."
---
<!-- UNREVIEWED -->

The runtime container has no user credentials, so pushes to an external git remote run under `GIT_TOKEN`. Procedure: set the token, mark the capability configured (stamps `GitPushState=configured` + cached `RemoteURL`), then commit and run the first push so deploy and build-integration actions stop blocking on the prereq chain.

## 1. Set `GIT_TOKEN` as a project env var

**Default to a fine-grained PAT scoped ONLY to the single `{owner}/{repo}` you intend to push to.** Single-repo blast radius — the container can only mutate this one repository, can't reach anything else in your account/org. GitHub fine-grained PATs require an expiration; pick the longest you're comfortable with (max 1 year). Token scopes (push-only is the minimum; add scopes for the build-integration you plan to wire):

| Host | Minimum scope |
|---|---|
| GitHub fine-grained | `Contents: Read and write`. Add `Secrets` + `Workflows` if you plan `build-integration=actions`. |
| GitLab personal access | `write_repository`. Add `api` if you plan `build-integration=webhook`. |

```
zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
```

## 2. Stamp git-push capability BEFORE the first push

`zerops_deploy strategy=git-push` refuses with `PREREQUISITE_MISSING` until `GitPushState=configured` is stamped on the service meta — the setup call is a pre-flight handshake, not a post-deploy confirmation:

```
zerops_workflow action="git-push-setup" service="appdev" \
  remoteUrl="{repoUrl}"
```

This validates the URL shape and stamps `GitPushState=configured` plus `RemoteURL`. It does NOT push anything yet — the actual transmission happens in the next step.

## 3. Commit + first push

```
ssh appdev "cd /var/www && git add -A && git commit -m 'initial commit'"

zerops_deploy targetService="appdev" strategy="git-push" \
  branch="main"
```

The response's `status` confirms the push. On failure, read `failureClassification` first — `category=credential` indicates a missing or rejected `GIT_TOKEN`; `category=config` with a committed-code cause means `/var/www` has no commit yet (re-run the ssh commit step above). The committed-code gate is a hard pre-flight: `zerops_deploy strategy="git-push"` refuses to push an empty working tree because there is nothing to commit. The handler DOES configure `.netrc` from `GIT_TOKEN` and runs `git remote add` against the stamped `RemoteURL` for you — those are not separate manual steps — but `git init` plus the first `git add -A && git commit` are agent-side and must precede the deploy call. The `remoteUrl` arg is optional after step 2 (handler reads it from the stamped meta); pass it on the deploy call only when you want to override the stamped value.
