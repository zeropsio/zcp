---
id: setup-git-push-container
priority: 2
phases: [strategy-setup]
gitPushStates: [unconfigured, broken]
environments: [container]
title: "Configure git-push capability on the container"
references-fields: [ops.DeployResult.Status, ops.DeployResult.Warnings, ops.DeployResult.FailureClassification]
---
Runtime containers have no user credentials, so pushes to an external git remote run under `GIT_TOKEN`. Collect three inputs, then one tool call that **verifies the token works against the remote before writing any project state**, then commit + push.

## Collect three inputs (use `AskUserQuestion` when the harness exposes it)

The `git-push-setup` walkthrough response carries `inputsRequired` for exactly these three; render them as a structured picker rather than free-text questions. The walkthrough also carries `recommendedIntegration` (default `actions` for GitHub remotes — zero manual Zerops dashboard step; `webhook` for GitLab and policy-constrained repos).

1. **Git remote URL (HTTPS only)** — `https://github.com/{owner}/{repo}.git`. Container mode authenticates via `.netrc` + PAT, which requires HTTPS. SSH (`git@host:owner/repo`) remotes are rejected — use the HTTPS clone URL.
2. **GIT_TOKEN** (fine-grained PAT, secret) — single-repo scope. **Default token shape** (covers the recommended Actions integration as well as the push-only minimum): GitHub fine-grained PAT scoped ONLY to `{owner}/{repo}` with `Contents: Read and write` + `Secrets: Read and write` + `Workflows: Read and write`. GitLab equivalent: `write_repository` + `api`. Single-repo blast radius — the container can only mutate this one repo. PATs require an expiration; pick the longest you're comfortable with (max 1 year). <!-- axis-m-keep -->
3. **Build integration** — pick one of `actions` (recommended for GitHub remotes; agent writes workflow YAML + `gh secret set` from the terminal), `webhook` (Zerops dashboard OAuth — one manual dashboard step; fallback for GitLab / policy-constrained repos), or `none` (external CI/CD you already own).

| Host | Recommended token scope |
|---|---|
| GitHub fine-grained | `Contents: Read and write` + `Secrets: Read and write` + `Workflows: Read and write` (covers push + actions integration). |
| GitLab personal access | `write_repository` + `api` (covers push + webhook integration). |

## 1. Verify + configure git-push capability in one call

```
zerops_workflow action="git-push-setup" service="{hostname}" \
  remoteUrl="{repoUrl}" \
  gitToken="{token}"
```

Probe-first: the handler runs `git ls-remote` against the supplied URL using the supplied token (transient `.netrc`, trap-cleaned). **No project state is touched until the probe passes.** On success: token is written to project env as sensitive (never echoed back), `origin` is synced in the working tree's git config, the runtime is restarted so `$GIT_TOKEN` is live in shell, and `meta.GitPushState=configured` + `meta.RemoteURL` are stamped. On failure (`GIT_TOKEN_INVALID`): project state is left untouched — fix the token or URL and re-call.

## 2. Commit + first push

```
ssh {hostname} "cd /var/www && git add -A && git commit -m 'initial commit'"

zerops_deploy targetService="{hostname}" strategy="git-push" \
  branch="main"
```

`git init` already ran at bootstrap time (`InitServiceGit`); the commit step lives outside ZCP because `zerops_deploy strategy="git-push"` refuses to push an empty working tree. The deploy call uses the project-level `GIT_TOKEN` and the stamped `origin` — no extra plumbing needed. The `remoteUrl` arg is optional on the deploy call (the stamped meta carries it). On failure, read `failureClassification.category` — `credential` means the token was rejected by the remote (re-call git-push-setup with a fresh PAT), `config` with a committed-code cause means there is no commit on HEAD yet.
