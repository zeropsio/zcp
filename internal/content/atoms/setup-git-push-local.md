---
id: setup-git-push-local
priority: 2
phases: [strategy-setup]
gitPushStates: [unconfigured, broken, unknown]
environments: [local]
title: "Configure git-push capability from the local machine"
references-fields: [ops.DeployResult.Status, ops.DeployResult.Warnings, ops.DeployResult.FailureClassification]
coverageExempt: "local-mode git-push setup — strategy-setup/container-unconfigured is the canonical scenario; local variant covered by Phase 5 quarterly live-eval"
---
On a local workstation, ZCP delegates auth to your existing git setup — SSH keys, macOS Keychain, or the system credential helper your local git already uses. ZCP never reads or writes credentials. Walk through a first push, then mark the capability configured.

## 1. Confirm git knows who to push as

A working `git push` outside ZCP confirms credentials are wired. If `git push` prompts for a password every time or fails on auth, fix that first (SSH key in agent, credential helper installed, or PAT in keychain).

## 2. Make sure origin matches the remote you want

```
git -C {workingDir} remote -v
```

If `origin` is empty, pass `remoteUrl=` on the first push and ZCP adds it. If `origin` exists with a different URL, ZCP refuses rather than silently rewrite — fix `origin` manually first.

## 3. Stamp git-push capability BEFORE the first push

`zerops_deploy strategy=git-push` refuses with `PREREQUISITE_MISSING` until `GitPushState=configured` is stamped on the service meta — the setup call is a pre-flight handshake, not a post-deploy confirmation:

```
zerops_workflow action="git-push-setup" service="{hostname}" \
  remoteUrl="{repoUrl}"
```

This validates the URL shape and stamps `GitPushState=configured` plus `RemoteURL`. It does NOT push anything yet — the actual transmission happens in the next step.

## 4. Commit + first push

```
git -C {workingDir} add -A && git -C {workingDir} commit -m "initial commit"

zerops_deploy targetService="{hostname}" strategy="git-push" \
  branch="main"
```

The response's `status` confirms the push. On `failureClassification.category=network` (transport-layer), check VPN / proxy / network reachability of the remote. On `category=credential`, fix git auth on this workstation; ZCP can't help — it never sees the credential. The `remoteUrl` arg is optional after step 3 (handler reads it from the stamped meta); pass it on the deploy call only when you want to override the stamped value.
