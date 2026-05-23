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
On a local workstation, ZCP delegates auth to your existing git setup — SSH keys, macOS Keychain, or the system credential helper your local git already uses. ZCP never reads or writes credentials. Token entry is NOT collected — local git already holds the user's credentials.

## Collect inputs (use `AskUserQuestion` when the harness exposes it)

The `git-push-setup` walkthrough response carries `inputsRequired` + `recommendedIntegration`; render them as a structured picker, not free-text questions. The inputs on local mode are: (1) **remote URL** (HTTPS or SSH form — whichever your local git can authenticate against), (2) **build integration** choice (`actions` recommended for GitHub remotes — zero manual Zerops dashboard step; `webhook` for GitLab and policy-constrained repos; `none` for external CI/CD).

## 1. Confirm git knows who to push as

Run a `git ls-remote {repoUrl}` outside ZCP. If it succeeds, credentials are wired. If `git ls-remote` prompts for a password or fails on auth, fix that first (SSH key in agent, credential helper installed, or PAT in keychain) before calling the setup tool.

## 2. Verify + configure git-push capability in one call

```
zerops_workflow action="git-push-setup" service="{hostname}" \
  remoteUrl="{repoUrl}"
```

Probe-first: the handler runs `git ls-remote` from the workspace using your local credentials. **No project state is touched until the probe passes.** On success: `origin` is synced in the workspace's git config, `meta.GitPushState=configured` + `meta.RemoteURL` are stamped. On failure (`GIT_TOKEN_INVALID`): project state is left untouched — fix your local git auth and re-call.

## 3. Commit + first push

```
git -C {workingDir} add -A && git -C {workingDir} commit -m "initial commit"

zerops_deploy targetService="{hostname}" strategy="git-push" \
  branch="main"
```

The response's `status` confirms the push. On `failureClassification.category=network` check VPN / proxy / network reachability. On `category=credential`, fix git auth locally; ZCP can't help — it never sees the credential. The `remoteUrl` arg is optional after step 2 (stamped meta carries it).
