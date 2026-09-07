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

Sync before the first edit, not after:

```
ssh <push-source-host> "cd /var/www && git fetch origin && git checkout <default-branch> && git reset --hard origin/<default-branch>"
```

Read the default branch from the remote rather than assuming `main` — `git remote show origin` names it, and a repo created before that convention may still be on `master`.

Uncommitted changes in the working copy are the one thing this destroys, so look before resetting (`git status --short`). Local edits that survived a previous session are almost always leftovers from work that already landed; if they are not, commit them to a branch first and say so, rather than carrying them silently into new work.

Where the remote later refuses the branch you push, the ref is protected: the answer is a pull request a person merges, never a fresh token. The transport classifier names that case directly when it happens.
