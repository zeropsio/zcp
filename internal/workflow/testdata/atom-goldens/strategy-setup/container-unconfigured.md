---
id: strategy-setup/container-unconfigured
atomIds: [setup-git-push-container]
description: "strategy-setup phase, in-container, GitPushState unconfigured — agent walks through GIT_TOKEN/.netrc setup."
---
The runtime container has no user credentials, so pushes to an external git remote run under `GIT_TOKEN`. Three inputs to collect, then three commands to run.

## Collect three inputs (use `AskUserQuestion` when the harness exposes it)

The `git-push-setup` walkthrough response carries `inputsRequired` for exactly these three; render them as a structured picker rather than free-text questions. The walkthrough also carries `recommendedIntegration` (default `actions` for GitHub remotes — zero manual Zerops dashboard step; `webhook` for GitLab and policy-constrained repos).

1. **Git remote URL** — `https://github.com/{owner}/{repo}.git` (HTTPS) or `git@github.com:{owner}/{repo}.git` (scp-form SSH).
2. **GIT_TOKEN** (fine-grained PAT, secret) — single-repo scope. **Default token shape** (covers the recommended Actions integration as well as the push-only minimum): GitHub fine-grained PAT scoped ONLY to `{owner}/{repo}` with `Contents: Read and write` + `Secrets: Read and write` + `Workflows: Read and write`. GitLab equivalent: `write_repository` + `api`. Single-repo blast radius — the container can only mutate this one repo. PATs require an expiration; pick the longest you're comfortable with (max 1 year).
3. **Build integration** — pick one of `actions` (recommended for GitHub remotes; agent writes workflow YAML + `gh secret set` from the terminal), `webhook` (Zerops dashboard OAuth — one manual dashboard step; fallback for GitLab / policy-constrained repos), or `none` (external CI/CD you already own).

| Host | Recommended token scope |
|---|---|
| GitHub fine-grained | `Contents: Read and write` + `Secrets: Read and write` + `Workflows: Read and write` (covers push + actions integration). |
| GitLab personal access | `write_repository` + `api` (covers push + webhook integration). |

## 1. Set `GIT_TOKEN` as a project env var

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

The response's `status` confirms the push. On failure, read `failureClassification` first — `category=credential` indicates a missing or rejected `GIT_TOKEN`; `category=config` with a committed-code cause means `/var/www` has no commit yet (re-run the ssh commit step above). The committed-code gate is a hard pre-flight: `zerops_deploy strategy="git-push"` refuses to push an empty working tree because there is nothing to commit. `.netrc` from `GIT_TOKEN` and `git remote add` against the stamped `RemoteURL` are wired by the deploy call itself — not separate manual steps — but `git init` plus the first `git add -A && git commit` are agent-side and must precede the deploy call. The `remoteUrl` arg is optional after step 2 (the stamped meta carries it); pass it on the deploy call only when you want to override the stamped value.
