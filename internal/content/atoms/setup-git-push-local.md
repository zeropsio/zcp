---
id: setup-git-push-local
priority: 2
phases: [strategy-setup]
gitPushStates: [unconfigured, broken, unknown]
environments: [local]
title: "Configure git-push capability from the local machine"
references-fields: [ops.DeployResult.Status, ops.DeployResult.Warnings, ops.DeployResult.FailureClassification]
---
On a local workstation, ZCP delegates auth to your existing git setup — SSH keys, macOS Keychain, or the system credential helper your local git already uses. ZCP never reads or writes credentials. Walk through a first push, then mark the capability configured.

## 1. Confirm git knows who to push as

A working `git push` outside ZCP confirms credentials are wired. If `git push` prompts for a password every time or fails on auth, fix that first (SSH key in agent, credential helper installed, or PAT in keychain).

## 2. Make sure origin matches the remote you want

```
git -C {workingDir} remote -v
```

If `origin` is empty, pass `remoteUrl=` on the first push and ZCP adds it. If `origin` exists with a different URL, ZCP refuses rather than silently rewrite — fix `origin` manually first.

## 3. Commit + first push

```
git -C {workingDir} add -A && git -C {workingDir} commit -m "initial commit"

zerops_deploy targetService="{hostname}" strategy="git-push" \
  remoteUrl="{repoUrl}" branch="main"
```

The response's `status` confirms the push. On `failureClassification.category=network` (transport-layer), check VPN / proxy / network reachability of the remote. On `category=credential`, fix git auth on this workstation; ZCP can't help — it never sees the credential.

## 4. Confirm setup

After the first push lands:

```
zerops_workflow action="git-push-setup" service="{hostname}" \
  remoteUrl="{repoUrl}"
```

This stamps `GitPushState=configured` plus `RemoteURL`. Subsequent pushes via `zerops_deploy strategy=git-push` skip this walkthrough, and `action=build-integration` stops blocking on the prereq chain.
