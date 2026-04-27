---
id: strategy-push-git-push-local
priority: 2
phases: [strategy-setup]
strategies: [push-git]
environments: [local]
title: "push-git push setup (user's git)"
references-fields: [ops.DeployResult.Status, ops.DeployResult.Warnings, ops.DeployResult.Message]
---

# Push path (user's git)

Local env uses the user's own git credentials (SSH keys, Keychain,
credential manager). **No `GIT_TOKEN`, no `.netrc`.** ZCP does not
manage credentials on the local path; it orchestrates `git push`.

## 1. Confirm the repo + origin

```
git -C <your-project-dir> rev-parse HEAD          # must have a commit
git -C <your-project-dir> remote get-url origin   # should match the intended repo
```

Add an origin if missing (`git remote add origin <url>`) or pass
`remoteUrl=<url>` on the first deploy — `zerops_deploy` refuses to silently
rewrite a mismatched existing origin.

## 2. First push

```
git -C <your-project-dir> add -A
git -C <your-project-dir> commit -m "your message"

zerops_deploy targetService="{targetHostname}" strategy="git-push"
```

A passphrase-protected key without an agent fails fast — the response's
`status` is non-success and `warnings` identifies the auth problem
(`ssh-add -l` for SSH agents, `git credential fill` for the credential
manager).

## What's different from container

- Local push-git uses local credentials only; project `GIT_TOKEN`
  does not apply.
- ZCP never runs `git init` or `git config` on your repo (only
  `git remote add` on first push).
- Pushed state is the branch HEAD; uncommitted working-tree changes
  are WARNED via `warnings[]` but not pushed.

The trigger setup (webhook or actions) in the paired trigger atom
handles what Zerops does after the push lands.
