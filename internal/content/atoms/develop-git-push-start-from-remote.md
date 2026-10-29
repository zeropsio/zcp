---
id: develop-git-push-start-from-remote
priority: 1
phases: [develop-active]
gitPushStates: [configured]
closeDeployModes: [auto, unset]
modes: [standard, simple, local-stage, local-only]
multiService: aggregate
title: "Start from the remote — the working copy outlives the session"
references-atoms: [develop-git-push-delivery]
---
The repo is the source of truth here, and this working copy is not it. A container's `/var/www` survives every session: it can still be sitting on a topic branch that was merged and deleted weeks ago, or on a `main` that is behind by everything anyone else has landed since. Writing code on top of that produces a diff against the wrong base, and the mistake only surfaces at the push — as a conflict, or worse, as a silent revert of someone else's work.

Sync before the first edit, not after — the command depends on whether this pair is wired to this Mate's HQ (a repository HQ gave it, whose `main` moves only by a person's merge).

Not wired to this Mate's HQ — sync by moving the working copy onto whatever the remote's default branch now is:

```
ssh <push-source-host> "cd /var/www && git fetch origin && git checkout <default-branch> && git reset --hard origin/<default-branch>"
```

Read the default branch from the remote rather than assuming `main` — `git remote show origin` names it, and a repo created before that convention may still be on `master`. Uncommitted changes in the working copy are the one thing this destroys, so look before resetting (`git status --short`). Local edits that survived a previous session are almost always leftovers from work that already landed; if they are not, commit them to a branch first and say so, rather than carrying them silently into new work.

In a Mate, read both in the dev service: `zcp git <push-source-host> remote show origin`, `zcp git <push-source-host> status --short`.

Wired to this Mate's HQ — take `main` in and start the work on a branch of your own cut from it; `main` itself is never pushed. Run every git command for it in its dev service with `zcp git <push-source-host> …`, never on the mounted checkout `/var/www/<push-source-host>`: git there and git in the dev service on one `.git` break it, and zcp answers the dev service's git for HQ and GitHub. After a command that changes files, re-read a file before editing it — the mount can show the old one for up to 20 s: <!-- axis-n-keep: a pair wired to HQ is a Mate's, always in a container -->

```
zcp git <push-source-host> fetch origin && zcp git <push-source-host> checkout main && zcp git <push-source-host> merge --ff-only origin/main && zcp git <push-source-host> checkout -b <your-branch>
```

One branch per piece of work, one change per branch: carrying on an open change means its own branch (status lists each repository's open changes by number and branch), and new work starts on a new branch cut as above. Where an HQ keeps one open change per repository, develop's start names the open one and says how to take its branch.

Keep one change open at a time on the dev service unless two pieces of work are truly independent: the person reviews and merges each, so fewer, larger, self-contained changes beat a queue of small dependent ones. Its `/var/www` is what its dev server runs and what deploys read: checking out another branch there changes what runs, and files that branch lacks disappear. To update another open change's branch — merge `main` in, a fix that needs no run — use a worktree the dev service holds below `/tmp/wt-`:

```
zcp git <push-source-host> fetch origin
zcp git <push-source-host> worktree add --detach /tmp/wt-<branch> origin/<branch>
zcp git <push-source-host> -C /tmp/wt-<branch> merge origin/main
zcp git <push-source-host> -C /tmp/wt-<branch> push origin HEAD:<branch>
zcp git <push-source-host> worktree remove /tmp/wt-<branch>
```

Nothing runs or deploys from it; remove it once pushed.

A `main` that will not fast-forward carries local commits that never landed: put them on a branch of their own (`zcp git <push-source-host> branch <name>`), then `zcp git <push-source-host> reset --keep origin/main`, and say so rather than carrying them silently into new work. On this Mate's HQ, while `origin/main` holds only HQ's initial commit, a `main` sharing no history with it is the repository's first history instead: leave it as it is, cut your branch from it, push the branch and open its change — merged, HQ makes it `main`; never merge or rebase it onto that commit.

Where the remote later refuses the branch you push, the ref is protected — on this Mate's HQ that is `main`, a tag, or a branch another open change holds: push a branch of your own and open its change with `zerops_workflow action="open-change"`, never a fresh token. The transport classifier names that case directly when it happens.
