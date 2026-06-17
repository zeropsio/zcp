---
id: strategy-setup/container-unconfigured
atomIds: [setup-git-push-container]
description: "strategy-setup phase, in-container, GitPushState unconfigured — agent walks through GIT_TOKEN/.netrc setup."
---
Runtime containers have no user credentials, so pushes to an external git remote run under `GIT_TOKEN`. Collect three inputs, then one tool call that **verifies the token works against the remote before writing any project state**, then commit + push.

## Collect three inputs (use `AskUserQuestion` when the harness exposes it)

The `git-push-setup` walkthrough response carries `inputsRequired` for exactly these three; render them as a structured picker rather than free-text questions. The walkthrough also carries `recommendedIntegration` (default `actions` for GitHub remotes — zero manual Zerops dashboard step; `webhook` for GitLab and policy-constrained repos).

1. **Git remote URL (HTTPS only)** — `https://github.com/{owner}/{repo}.git`. Container mode authenticates via a PAT (session-env git credential helper), which requires HTTPS. SSH (`git@host:owner/repo`) remotes are rejected — use the HTTPS clone URL.
2. **GIT_TOKEN** (fine-grained PAT, secret) — single-repo scope, created/edited at <https://github.com/settings/personal-access-tokens>. Under **Repository access** select **Only select repositories** → this repo, NOT **Public repositories** (read-only — cannot push). **Push-only minimum:** `Contents: Read and write`. **Recommended Actions integration (full set):** `Contents: Read and write` + `Workflows: Read and write` (REQUIRED — pushing any file under `.github/workflows/` is rejected without it) + `Secrets: Read and write` (for `gh secret set`) + `Actions: Read` and `Checks: Read` (so `gh run list` / `gh run watch` can read the run). GitLab equivalent: `write_repository` + `api`. Single-repo blast radius — the container can only mutate this one repo. PATs require an expiration; pick the longest you're comfortable with (max 1 year).
3. **Build integration** — pick one of `actions` (recommended for GitHub remotes; agent writes workflow YAML + `gh secret set` from the terminal), `webhook` (Zerops dashboard OAuth — one manual dashboard step; fallback for GitLab / policy-constrained repos), or `none` (external CI/CD you already own).

| Host | Recommended token scope |
|---|---|
| GitHub fine-grained (git-push only) | `Contents: Read and write`. |
| GitHub fine-grained (actions integration) | `Contents: Read and write` + `Workflows: Read and write` (push `.github/workflows/`) + `Secrets: Read and write` (`gh secret set`) + `Actions: Read` and `Checks: Read` (watch the run). Create at <https://github.com/settings/personal-access-tokens>. |
| GitLab personal access | `write_repository` + `api` (covers push + webhook integration). |

## 1. Verify + configure git-push capability in one call

```
zerops_workflow action="git-push-setup" service="appdev" \
  remoteUrl="{repoUrl}" \
  gitToken="{token}"
```

Probe-first: the handler runs a write-auth probe (`git push --dry-run` to a throwaway ref against the supplied URL using the supplied token — non-mutating, but exercises git-receive-pack so a read-only/garbage token fails HERE, not at the first push; falls back to read reachability only on a repo with no commit yet) via an inline credential helper — nothing touches disk. **No project state is touched until the probe passes.** On success: the token is written as a service-scope secret on the push-source service (never echoed back; one token per push-source/repo pair, so a second pair's setup does not clobber the first), `origin` + a url-scoped credential helper are synced in the working tree's git config, and a FRESH session is verified end-to-end before `meta.GitPushState=configured` + `meta.RemoteURL` are stamped — no container restart is involved (fresh sessions read the live secret within seconds). Re-calling with the SAME remote and a NEW token ROTATES the credential through the same probe-first chain. On failure (`GIT_TOKEN_INVALID`): project state is left untouched, and the error carries the git stderr + a `failureClassification` naming the precise cause (auth rejected vs repository not found) — ask the USER for a corrected token/URL (never generate one) and re-call.

## 2. Commit + first push

```
ssh appdev "cd /var/www && git add -A && git commit -m 'initial commit'"

zerops_deploy targetService="appdev" strategy="git-push" \
  branch="main"
```

`git init` already ran at bootstrap time (`InitServiceGit`); the commit step lives outside ZCP because `zerops_deploy strategy="git-push"` refuses to push an empty working tree. The deploy call authenticates via the push source's `GIT_TOKEN` (service-scope secret, read live by the credential helper) and the stamped `origin` — no extra plumbing needed.

**Push via `zerops_deploy strategy="git-push"`** — it watches the integration build to completion (build logs + classification on failure) instead of stopping at the push receipt. A manual `git push` run ON the runtime container (`ssh appdev "cd /var/www && git push"`) also authenticates — the setup writes a url-scoped credential helper into the repo config reading the live `$GIT_TOKEN` — but skips the build watch, so prefer the deploy call. Shells OUTSIDE the container (the ZCP host, the SSHFS mount) hold no credential and still fail with "could not read Username".

The `remoteUrl` arg is optional on the deploy call (the stamped meta carries it). On failure, read `failureClassification.category` — `credential` means the token was rejected by the remote (re-call git-push-setup with a fresh PAT), `config` with a committed-code cause means there is no commit on HEAD yet.
