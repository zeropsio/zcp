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

Wired to this Mate's HQ — take `main` in and start the work on a branch of your own cut from it; `main` itself is never pushed. Run it in your own shell on the mounted checkout (`/var/www/<push-source-host>`, a code-volume pair's folder below it), never over ssh: whether the checkout's origin is HQ or GitHub, git asks `zcp hq git-credential`, which only your own shell has: <!-- axis-n-keep: a pair wired to HQ is a Mate's, always in a container -->

```
cd /var/www/<push-source-host> && git fetch origin && git checkout main && git merge --ff-only origin/main && git checkout -b <your-branch>
```

A `main` that will not fast-forward carries local commits that never landed: put them on a branch of their own (`git branch <name>`), then `git reset --keep origin/main`, and say so rather than carrying them silently into new work.

Where the remote later refuses the branch you push, the ref is protected — on this Mate's HQ that is `main`, a tag, or a branch another open change holds: push a branch of your own and open its change with `zerops_workflow action="open-change"`, never a fresh token. The transport classifier names that case directly when it happens.
