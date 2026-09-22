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

Sync before the first edit, not after — the command depends on whether this pair is wired to the account's own Gitea (a repository the broker gave it; its branch is never `main`, which is protected there).

Not wired to the account's own Gitea — sync by moving the working copy onto whatever the remote's default branch now is:

```
ssh <push-source-host> "cd /var/www && git fetch origin && git checkout <default-branch> && git reset --hard origin/<default-branch>"
```

Read the default branch from the remote rather than assuming `main` — `git remote show origin` names it, and a repo created before that convention may still be on `master`. Uncommitted changes in the working copy are the one thing this destroys, so look before resetting (`git status --short`). Local edits that survived a previous session are almost always leftovers from work that already landed; if they are not, commit them to a branch first and say so, rather than carrying them silently into new work.

Wired to the account's own Gitea — there is no command to run here, on purpose: the working copy stays on this Mate's own branch always, and neither checking it out to the base, resetting it, nor merging the base in by hand is the sync. A plain `git merge origin/<base>` run by hand can fail on a false conflict — the base can carry a squash of THIS Mate's own earlier work, which shares no history with the branch it came from, so an ordinary merge reads it as two histories that both add the same files even though nothing really collides. When a pull request of this Mate's own is recorded as merged, zcp folds that landing in for you: the moment a pass learns of the merge, and again before whatever push follows it needs a landing absorbed — a delivery's own commit+push, or an ordinary `strategy="git-push"`. Usually that keeps the false conflict from ever reaching you or the pull request it opens; an old container git, or a landing zcp cannot prove lossless, can still leave it unabsorbed, and the ordinary step can then hit that same shape for real.

Either way, every conflict zcp reports here — from the absorb step itself, or the ordinary step behind an unabsorbed landing — names the exact commands in its own message: never a plain reset, and never the plain `git fetch origin && git merge origin/<base>` on its own, which would only repeat the same conflict. Run what the message says, in the checkout it names, then push or deploy again — that is what resolves it.

Where the remote later refuses the branch you push, the ref is protected: the answer is a pull request a person merges, never a fresh token. The transport classifier names that case directly when it happens.
