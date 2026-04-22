---
id: strategy-push-git-push-local
priority: 2
phases: [strategy-setup]
strategies: [push-git]
environments: [local]
title: "push-git push setup — local env (user's git)"
---

# Push path — local env

Local env uses the user's own git credentials (SSH keys, Keychain,
credential manager). **No `GIT_TOKEN`, no `.netrc`.** ZCP doesn't manage
credentials on the local path; it just orchestrates `git push`.

## 1. Confirm the repo + origin

```
git -C <your-project-dir> rev-parse HEAD         # must have a commit
git -C <your-project-dir> remote get-url origin  # should match the intended repo
```

If there's no origin yet and you know the URL, add it:

```
git -C <your-project-dir> remote add origin <url>
```

Or pass `remoteUrl=<url>` to `zerops_deploy strategy=git-push` on the
first push and ZCP will add it for you (only when no origin is set —
it refuses to silently rewrite an existing mismatched origin).

## 2. First push

Commit, then push via `zerops_deploy`:

```
git -C <your-project-dir> add -A
git -C <your-project-dir> commit -m "your message"

zerops_deploy targetService="{targetHostname}" strategy="git-push"
```

`zerops_deploy` runs `git push origin <branch>` with
`GIT_TERMINAL_PROMPT=0` so a passphrase-protected key without an agent
fails fast instead of hanging. If it fails with an auth error, check
your local git credentials (`ssh-add -l` for SSH agent keys,
`git credential fill` for the credential manager).

## What's different from container

- ZCP does NOT set up `GIT_TOKEN` on the Zerops project. If you
  previously had `GIT_TOKEN` set for the container's benefit, it stays
  out of the way — local push reads nothing from there.
- ZCP does NOT run `git init`, `git config`, or alter your repo beyond
  what `git remote add` does on first push.
- The committed state that gets pushed is what's in the branch's HEAD.
  Uncommitted changes in the working tree are WARNED about, not pushed.

The trigger setup (webhook or actions) in the paired trigger atom
handles what Zerops does after the push lands.
